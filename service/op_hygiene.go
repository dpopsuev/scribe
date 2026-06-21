//nolint:goconst // hygiene checks reference status strings inline
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

type hygieneInput struct {
	Scope       string `json:"scope,omitempty"`
	IncludeCode bool   `json:"include_code,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Format      string `json:"format,omitempty"`
}

// SuggestedFix is a machine-parseable repair action for a hygiene finding.
type SuggestedFix struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params"`
}

// HygieneFinding is a single issue found during hygiene analysis, annotated
// with impact, confidence, and an optional auto-fix.
type HygieneFinding struct {
	Severity     string        `json:"severity"`
	Category     string        `json:"category"`
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	Detail       string        `json:"detail"`
	Fix          string        `json:"fix,omitempty"`
	Impact       string        `json:"impact"`
	Confidence   string        `json:"confidence"`
	SafeAutofix  bool          `json:"safe_autofix"`
	Owner        string        `json:"owner,omitempty"`
	Score        int           `json:"score"`
	SuggestedFix *SuggestedFix `json:"suggested_fix,omitempty"`
}

// HygieneOutput is the JSON payload returned by hygiene when format=full.
type HygieneOutput struct {
	Total    int              `json:"total"`
	Summary  map[string]int   `json:"summary"`
	Findings []HygieneFinding `json:"findings"`
	Pruned   int              `json:"pruned"`
}

func isCodeKind(kind string) bool {
	return strings.HasPrefix(kind, "code.")
}

func hygieneScore(impact, confidence string) int {
	iw := map[string]int{"high": 3, "medium": 2, "low": 1}
	cw := map[string]int{"certain": 3, "likely": 2, "guess": 1}
	return iw[impact] * cw[confidence]
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 0
	case "planning":
		return 1
	case "content":
		return 2
	case "index":
		return 3
	default:
		return 4
	}
}

func ownerFromProvenance(art *parchment.Artifact) string {
	if art == nil || art.Extra == nil {
		return ""
	}
	prov, ok := art.Extra["provenance"].(map[string]any)
	if !ok {
		return ""
	}
	sid, _ := prov["session_id"].(string)
	return sid
}

// reviewedAfterNeighbors returns true if Extra["hygiene_reviewed_at"]
// is set and is after the artifact's own UpdatedAt. When an agent
// reviews staleness findings and determines they are acceptable, it
// sets this timestamp via update(extra={"hygiene_reviewed_at": "..."}).
func reviewedAfterNeighbors(art *parchment.Artifact) bool {
	if art == nil || art.Extra == nil {
		return false
	}
	ts, ok := art.Extra["hygiene_reviewed_at"].(string)
	if !ok || ts == "" {
		return false
	}
	reviewed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return reviewed.After(art.UpdatedAt)
}

// collectFindings runs all hygiene checks and returns annotated, scored findings.
func collectFindings(ctx context.Context, svc *Service, scope string, includeCode bool) []HygieneFinding { //nolint:gocyclo,funlen // sequential independent checks
	var findings []HygieneFinding

	labels := []string{}
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
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
			findings = append(findings, HygieneFinding{
				Severity:    "critical",
				Category:    "zombie_campaign",
				ID:          c.ID,
				Title:       c.Title,
				Detail:      "active campaign with zero active goals",
				Fix:         fmt.Sprintf("set(id=%q, field=status, value=work.draft)", c.ID),
				Impact:      "high",
				Confidence:  "certain",
				SafeAutofix: false,
				Owner:       ownerFromProvenance(c),
				SuggestedFix: &SuggestedFix{
					Action: "set",
					Params: map[string]any{"id": c.ID, "field": "status", "value": "work.draft"},
				},
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
			fix := lifecycleFixMap[status]
			if fix == "" {
				fix = labelStatusDraft
			}
			findings = append(findings, HygieneFinding{
				Severity:    "critical",
				Category:    "lifecycle_mismatch",
				ID:          art.ID,
				Title:       art.Title,
				Detail:      fmt.Sprintf("effort artifact has invalid status %q", status),
				Fix:         fmt.Sprintf("set(id=%q, field=status, value=%s, force=true)", art.ID, fix),
				Impact:      "high",
				Confidence:  "certain",
				SafeAutofix: true,
				Owner:       ownerFromProvenance(art),
				SuggestedFix: &SuggestedFix{
					Action: "set",
					Params: map[string]any{"id": art.ID, "field": "status", "value": fix, "force": true},
				},
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
			findings = append(findings, HygieneFinding{
				Severity:    "planning",
				Category:    "stale_active",
				ID:          t.ID,
				Title:       t.Title,
				Detail:      fmt.Sprintf("active for %d days with no updates", int(now.Sub(t.UpdatedAt).Hours()/24)),
				Fix:         fmt.Sprintf("set(id=%q, field=status, value=work.blocked)", t.ID),
				Impact:      "medium",
				Confidence:  "likely",
				SafeAutofix: false,
				Owner:       ownerFromProvenance(t),
				SuggestedFix: &SuggestedFix{
					Action: "set",
					Params: map[string]any{"id": t.ID, "field": "status", "value": "work.blocked"},
				},
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
		if isCodeKind(kind) && !includeCode {
			continue
		}
		outE, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Outgoing)
		inE, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Incoming)
		if len(outE) == 0 && len(inE) == 0 {
			sev := "planning"
			if isCodeKind(kind) {
				sev = "index"
			}
			findings = append(findings, HygieneFinding{
				Severity:    sev,
				Category:    "orphan",
				ID:          art.ID,
				Title:       art.Title,
				Detail:      fmt.Sprintf("no edges — kind=%s", kind),
				Fix:         fmt.Sprintf("delete(id=%q)", art.ID),
				Impact:      "low",
				Confidence:  "likely",
				SafeAutofix: false,
				Owner:       ownerFromProvenance(art),
				SuggestedFix: &SuggestedFix{
					Action: "delete",
					Params: map[string]any{"id": art.ID},
				},
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
			findings = append(findings, HygieneFinding{
				Severity:    "content",
				Category:    "incomplete_knowledge",
				ID:          art.ID,
				Title:       art.Title,
				Detail:      fmt.Sprintf("missing required sections: %s", strings.Join(missing, ", ")),
				Fix:         fmt.Sprintf("update(id=%q, sections=[{name:%q, text:\"...\"}])", art.ID, missing[0]),
				Impact:      "low",
				Confidence:  "guess",
				SafeAutofix: false,
				Owner:       ownerFromProvenance(art),
			})
		}
	}

	// ── Index: stale references ──
	if allArts == nil {
		allArts, _ = svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: labels})
	}
	for _, art := range allArts {
		kind := art.Label(parchment.LabelPrefixKind)
		if isCodeKind(kind) && !includeCode {
			continue
		}
		status := parchment.StatusFromLabels(art.Labels)
		if status != labelStatusActive {
			continue
		}
		// Skip if agent previously reviewed and dismissed staleness
		if reviewedAfterNeighbors(art) {
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
			findings = append(findings, HygieneFinding{
				Severity:   sev,
				Category:   "stale_references",
				ID:         art.ID,
				Title:      art.Title,
				Detail:     fmt.Sprintf("%d neighbor(s) changed >24h after last update: %s", len(staleN), strings.Join(ids, ", ")),
				Fix:        fmt.Sprintf("update(id=%q, extra={\"hygiene_reviewed_at\": %q})", art.ID, time.Now().UTC().Format(time.RFC3339)),
				Impact:     "medium",
				Confidence: "guess",
				Owner:      ownerFromProvenance(art),
			})
		}
	}

	// Score and sort
	for i := range findings {
		findings[i].Score = hygieneScore(findings[i].Impact, findings[i].Confidence)
	}
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Score != findings[j].Score {
			return findings[i].Score > findings[j].Score
		}
		return severityRank(findings[i].Severity) < severityRank(findings[j].Severity)
	})

	return findings
}

var opHygiene = Op{
	Name: "hygiene",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in hygieneInput
		_ = json.Unmarshal(raw, &in)

		findings := collectFindings(ctx, svc, in.Scope, in.IncludeCode)

		// ── Revision pruning ──
		var pruneLabels []string
		if in.Scope != "" {
			pruneLabels = append(pruneLabels, parchment.LabelPrefixScope+in.Scope)
		}
		pruneArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: pruneLabels})
		revisionsPruned := 0
		for _, art := range pruneArts {
			n, _ := svc.Proto.Store().PruneRevisions(ctx, art.ID, 20)
			revisionsPruned += n
		}
		if c, ok := svc.Proto.Store().(parchment.Compactor); ok && revisionsPruned > 0 {
			_ = c.IncrementalVacuum(ctx)
		}

		// ── Filter by severity ──
		if in.Severity != "" {
			var filtered []HygieneFinding
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

		// ── JSON output (format=full) ──
		if in.Format == "full" {
			out := HygieneOutput{
				Total:    len(findings),
				Summary:  map[string]int{},
				Findings: findings,
				Pruned:   revisionsPruned,
			}
			for _, f := range findings {
				out.Summary[f.Impact]++
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		}

		// ── Text output (default, backward-compatible) ──
		severityGroups := map[string][]HygieneFinding{}
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
				if f.SafeAutofix {
					fmt.Fprintf(&b, "    safe_autofix: true\n")
				}
				if f.Fix != "" {
					fmt.Fprintf(&b, "    fix: %s\n", f.Fix)
				}
			}
		}
		return b.String(), nil
	},
}
