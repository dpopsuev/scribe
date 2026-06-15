//nolint:goconst // brief renders status strings inline
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

type briefInput struct {
	Scope string `json:"scope,omitempty"`
}

var opBrief = Op{
	Name: "brief",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in briefInput
		_ = json.Unmarshal(raw, &in)
		if in.Scope == "" {
			return "", fmt.Errorf("scope/project required") //nolint:err113 // agent-facing
		}

		scopeLabel := parchment.LabelPrefixScope + in.Scope
		var b strings.Builder
		fmt.Fprintf(&b, "# Project: %s\n\n", in.Scope)

		campaigns, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: []string{scopeLabel, parchment.LabelPrefixKind + "effort.campaign"},
		})
		if len(campaigns) > 0 {
			b.WriteString("## Campaigns\n")
			for _, c := range campaigns {
				status := parchment.StatusFromLabels(c.Labels)
				score := svc.Proto.CompletionScore(ctx, c)
				fmt.Fprintf(&b, "  [%s] %s (%.0f%%)\n", status, c.Title, score*100)
			}
			b.WriteString("\n")
		}

		activeGoals, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: []string{scopeLabel, parchment.LabelPrefixKind + "effort.goal", "work.active"},
		})
		if len(activeGoals) > 0 {
			b.WriteString("## Active Goals\n")
			for _, g := range activeGoals {
				score := svc.Proto.CompletionScore(ctx, g)
				fmt.Fprintf(&b, "  %s (%.0f%%) — %s\n", g.Title, score*100, g.ID)
			}
			b.WriteString("\n")
		}

		activeTasks, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: []string{scopeLabel, parchment.LabelPrefixKind + "effort.task", "work.active"},
		})
		if len(activeTasks) > 0 {
			b.WriteString("## Active Tasks\n")
			for _, t := range activeTasks {
				fmt.Fprintf(&b, "  %s — %s\n", t.Title, t.ID)
			}
			b.WriteString("\n")
		}

		since := time.Now().Add(-48 * time.Hour)
		recent, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels:       []string{scopeLabel},
			UpdatedAfter: since.Format(time.RFC3339),
		})
		if len(recent) > 10 {
			recent = recent[:10]
		}
		if len(recent) > 0 {
			b.WriteString("## Recent (48h)\n")
			for _, a := range recent {
				kind := a.Label(parchment.LabelPrefixKind)
				fmt.Fprintf(&b, "  %s  %s  %s\n", a.UpdatedAt.Format("01-02 15:04"), kind, a.Title)
			}
			b.WriteString("\n")
		}

		return b.String(), nil
	},
}
