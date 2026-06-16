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
	Scope string `json:"scope,omitempty"`
}

type hygieneFinding struct {
	Category string `json:"category"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
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
					Category: "zombie_campaign", ID: c.ID, Title: c.Title,
					Detail: "active campaign with zero active goals — park or activate a goal",
				})
			}
		}

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
					Category: "stale_active", ID: t.ID, Title: t.Title,
					Detail: fmt.Sprintf("active for %d days with no updates", int(now.Sub(t.UpdatedAt).Hours()/24)),
				})
			}
		}

		allArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: labels})
		for _, art := range allArts {
			out, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Outgoing)
			in, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Incoming)
			kind := art.Label(parchment.LabelPrefixKind)
			if len(out) == 0 && len(in) == 0 && kind != "" &&
				kind != "knowledge.concept" && kind != "support.config" {
				findings = append(findings, hygieneFinding{
					Category: "orphan", ID: art.ID, Title: art.Title,
					Detail: "no edges — not connected to any other artifact",
				})
			}
		}

		// Knowledge health: stale, incomplete, and legacy knowledge artifacts.
		knowledgeArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels:     labels,
			KindPrefix: "knowledge",
		})
		for _, art := range knowledgeArts {
			status := parchment.StatusFromLabels(art.Labels)

			if !art.UpdatedAt.IsZero() && now.Sub(art.UpdatedAt) > 90*24*time.Hour &&
				status != "note.evergreen" {
				findings = append(findings, hygieneFinding{
					Category: "stale_knowledge", ID: art.ID, Title: art.Title,
					Detail: fmt.Sprintf("not updated in %d days — may be outdated",
						int(now.Sub(art.UpdatedAt).Hours()/24)),
				})
			}

			shouldSections := svc.Proto.ShouldSections(art.Label(parchment.LabelPrefixKind))
			if len(shouldSections) > 0 {
				existing := make(map[string]bool, len(art.Sections))
				for _, s := range art.Sections {
					existing[s.Name] = true
				}
				var missing []string
				for _, s := range shouldSections {
					if !existing[s] {
						missing = append(missing, s)
					}
				}
				if len(missing) > 0 {
					findings = append(findings, hygieneFinding{
						Category: "incomplete_knowledge", ID: art.ID, Title: art.Title,
						Detail: fmt.Sprintf("missing sections: %s", strings.Join(missing, ", ")),
					})
				}
			}

			inEdges, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Incoming)
			if len(inEdges) > 0 {
				allReferrersTerminal := true
				for _, e := range inEdges {
					ref, err := svc.Proto.GetArtifact(ctx, e.From)
					if err != nil {
						allReferrersTerminal = false
						break
					}
					if !svc.Proto.IsTerminal(parchment.StatusFromLabels(ref.Labels)) {
						allReferrersTerminal = false
						break
					}
				}
				if allReferrersTerminal {
					findings = append(findings, hygieneFinding{
						Category: "legacy_knowledge", ID: art.ID, Title: art.Title,
						Detail: fmt.Sprintf("all %d referencing artifacts are completed/archived", len(inEdges)),
					})
				}
			}
		}

		if len(findings) == 0 {
			scope := in.Scope
			if scope == "" {
				scope = "all scopes"
			}
			return fmt.Sprintf("hygiene: %s is clean — no issues found", scope), nil
		}

		groups := map[string][]hygieneFinding{}
		for _, f := range findings {
			groups[f.Category] = append(groups[f.Category], f)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "hygiene: %d issues found\n", len(findings))
		for cat, items := range groups {
			fmt.Fprintf(&b, "\n## %s (%d)\n", cat, len(items))
			for _, f := range items {
				fmt.Fprintf(&b, "  %s  %s\n    %s\n", f.ID, f.Title, f.Detail)
			}
		}
		return b.String(), nil
	},
}
