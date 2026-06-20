//nolint:goconst // hygiene checks reference status strings inline
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

type hygieneInput struct {
	Scope       string `json:"scope,omitempty"`
	IncludeCode bool   `json:"include_code,omitempty"`
	Severity    string `json:"severity,omitempty"`
}

type hygieneFinding struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix,omitempty"`
}

func isCodeKind(kind string) bool {
	return strings.HasPrefix(kind, "code.") || kind == "knowledge.source"
}

var opHygiene = Op{
	Name: "hygiene",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in hygieneInput
		_ = json.Unmarshal(raw, &in)

		var findings []hygieneFinding

		labels := []string{}
		if in.Scope != "" {
			labels = append(labels, parchment.LabelPrefixScope+in.Scope)
		}

		// ── Critical: zombie campaigns ──
		campaigns, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: append(labels, labelCampaign),
		})
		for _, c := range campaigns {
			status := parchment.StatusFromLabels(c.Labels)
			if status != labelStatusActive {
				continue
			}
			children, _ := svc.Proto.Store().Neighbors(ctx, c.ID, parchment.RelParentOf, parchment.Outgoing)
			activeGoals := 0
			for _, e := range children {
				goal, _ := svc.Proto.GetArtifact(ctx, e.To)
				if goal != nil && parchment.StatusFromLabels(goal.Labels) == labelStatusActive {
					activeGoals++
				}
			}
			if activeGoals == 0 {
				findings = append(findings, hygieneFinding{
					Severity: "critical",
					Category: "zombie_campaign", ID: c.ID, Title: c.Title,
					Detail: "active campaign with zero active goals",
					Fix:    fmt.Sprintf("set(id=%q, field=status, value=work.draft)", c.ID),
				})
			}
		}

		// ── Critical: lifecycle mismatch ──
		effortArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: labels, KindPrefix: "effort",
		})
		for _, art := range effortArts {
			status := parchment.StatusFromLabels(art.Labels)
			if strings.HasPrefix(status, "note.") || strings.HasPrefix(status, "decision.") || strings.HasPrefix(status, "inv.") {
				findings = append(findings, hygieneFinding{
					Severity: "critical",
					Category: "lifecycle_mismatch", ID: art.ID, Title: art.Title,
					Detail: fmt.Sprintf("effort artifact has invalid status %q", status),
					Fix:    fmt.Sprintf("set(id=%q, field=status, value=work.draft, force=true)", art.ID),
				})
			}
		}

		// ── Planning: stale active tasks ──
		tasks, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: append(labels, labelTask),
		})
		now := time.Now()
		for _, t := range tasks {
			status := parchment.StatusFromLabels(t.Labels)
			if status != labelStatusActive {
				continue
			}
			if !t.UpdatedAt.IsZero() && now.Sub(t.UpdatedAt) > 14*24*time.Hour {
				findings = append(findings, hygieneFinding{
					Severity: "planning",
					Category: "stale_active", ID: t.ID, Title: t.Title,
					Detail: fmt.Sprintf("active for %d days with no updates", int(now.Sub(t.UpdatedAt).Hours()/24)),
					Fix:    fmt.Sprintf("set(id=%q, field=status, value=work.blocked)", t.ID),
				})
			}
		}

		// ── Planning/Index: orphans ──
		allArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: labels})
		for _, art := range allArts {
			kind := art.Label(parchment.LabelPrefixKind)
			if kind == "" || kind == "knowledge.concept" || kind == "support.config" {
				continue
			}
			status := parchment.StatusFromLabels(art.Labels)
			if status == "status:archived" || status == "status:retired" {
				continue
			}
			if isCodeKind(kind) && !in.IncludeCode {
				continue
			}
			outE, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Outgoing)
			inE, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Incoming)
			if len(outE) == 0 && len(inE) == 0 {
				sev := "planning"
				if isCodeKind(kind) {
					sev = "index"
				}
				findings = append(findings, hygieneFinding{
					Severity: sev,
					Category: "orphan", ID: art.ID, Title: art.Title,
					Detail: fmt.Sprintf("no edges — kind=%s", kind),
					Fix:    fmt.Sprintf("delete(id=%q)", art.ID),
				})
			}
		}

		// ── Content: incomplete knowledge ──
		knowledgeArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: labels, KindPrefix: "knowledge",
		})
		for _, art := range knowledgeArts {
			mustSections := svc.Proto.MustSections(art.Label(parchment.LabelPrefixKind))
			if len(mustSections) == 0 {
				continue
			}
			existing := make(map[string]bool, len(art.Sections))
			for _, s := range art.Sections {
				existing[s.Name] = true
			}
			var missing []string
			for _, s := range mustSections {
				if !existing[s] {
					missing = append(missing, s)
				}
			}
			if len(missing) > 0 {
				findings = append(findings, hygieneFinding{
					Severity: "content",
					Category: "incomplete_knowledge", ID: art.ID, Title: art.Title,
					Detail: fmt.Sprintf("missing required sections: %s", strings.Join(missing, ", ")),
					Fix:    fmt.Sprintf("update(id=%q, sections=[{name:%q, text:\"...\"}])", art.ID, missing[0]),
				})
			}
		}

		// ── Index: stale references (only when include_code or non-code) ──
		for _, art := range allArts {
			kind := art.Label(parchment.LabelPrefixKind)
			if isCodeKind(kind) && !in.IncludeCode {
				continue
			}
			status := parchment.StatusFromLabels(art.Labels)
			if status != labelStatusActive {
				continue
			}
			staleN := NeighborStaleness(ctx, svc.Proto.Store(), art)
			if len(staleN) > 3 {
				staleN = staleN[:3]
			}
			if len(staleN) > 0 {
				ids := make([]string, len(staleN))
				for i, s := range staleN {
					ids[i] = s.ID
				}
				sev := "planning"
				if isCodeKind(kind) {
					sev = "index"
				}
				findings = append(findings, hygieneFinding{
					Severity: sev,
					Category: "stale_references", ID: art.ID, Title: art.Title,
					Detail: fmt.Sprintf("%d neighbor(s) changed: %s", len(staleN), strings.Join(ids, ", ")),
				})
			}
		}

		// ── Revision pruning ──
		revisionsPruned := 0
		for _, art := range allArts {
			n, _ := svc.Proto.Store().PruneRevisions(ctx, art.ID, 20)
			revisionsPruned += n
		}
		if c, ok := svc.Proto.Store().(parchment.Compactor); ok && revisionsPruned > 0 {
			_ = c.IncrementalVacuum(ctx)
		}

		// ── Filter by severity ──
		if in.Severity != "" {
			var filtered []hygieneFinding
			for _, f := range findings {
				if f.Severity == in.Severity {
					filtered = append(filtered, f)
				}
			}
			findings = filtered
		}

		if len(findings) == 0 {
			scope := in.Scope
			if scope == "" {
				scope = "all scopes"
			}
			msg := fmt.Sprintf("hygiene: %s is clean — no issues found", scope)
			if revisionsPruned > 0 {
				msg += fmt.Sprintf(" (pruned %d old revisions)", revisionsPruned)
			}
			return msg, nil
		}

		// ── Output: grouped by severity (critical first) ──
		severityGroups := map[string][]hygieneFinding{}
		for _, f := range findings {
			severityGroups[f.Severity] = append(severityGroups[f.Severity], f)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "hygiene: %d issues found", len(findings))
		for sev, items := range severityGroups {
			fmt.Fprintf(&b, " | %s:%d", sev, len(items))
		}
		b.WriteString("\n")
		if revisionsPruned > 0 {
			fmt.Fprintf(&b, "(pruned %d old revisions)\n", revisionsPruned)
		}

		for _, sev := range []string{"critical", "planning", "content", "index"} {
			items := severityGroups[sev]
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n## %s (%d)\n", strings.ToUpper(sev), len(items))
			for _, f := range items {
				fmt.Fprintf(&b, "  [%s] %s  %s\n", f.Category, f.ID, f.Title)
				fmt.Fprintf(&b, "    %s\n", f.Detail)
				if f.Fix != "" {
					fmt.Fprintf(&b, "    fix: %s\n", f.Fix)
				}
			}
		}
		return b.String(), nil
	},
}
