package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opTriage)
}

type triageCampaignSummary struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	ActiveGoals int    `json:"active_goals"`
	DraftGoals  int    `json:"draft_goals"`
	TotalGoals  int    `json:"total_goals"`
}

type triageOutput struct {
	ActiveCampaigns     []triageCampaignSummary `json:"active_campaigns"`
	CancelledWithActive []triageCampaignSummary `json:"canceled_with_active,omitempty"`
	StaleWork           int                     `json:"stale_work"`
	OrphanEffort        int                     `json:"orphan_effort"`
	LifecycleMismatch   int                     `json:"lifecycle_mismatch"`
	RecentlyTouched     int                     `json:"recently_touched"`
}

func summarizeCampaigns(ctx context.Context, svc *Service, labels []string) (active, canceled []triageCampaignSummary) {
	campaigns, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels: append(labels, labelCampaign),
	})
	for _, c := range campaigns {
		status := parchment.StatusFromLabels(c.Labels)
		children, _ := svc.Proto.Neighbors(ctx, c.ID, parchment.RelParentOf, parchment.Outgoing)

		summary := triageCampaignSummary{
			ID: c.ID, Title: c.Title, Status: status, TotalGoals: len(children),
		}
		for _, e := range children {
			goal, _ := svc.Proto.GetArtifact(ctx, e.To)
			if goal == nil {
				continue
			}
			gs := parchment.StatusFromLabels(goal.Labels)
			switch gs {
			case labelStatusActive:
				summary.ActiveGoals++
			case labelStatusDraft:
				summary.DraftGoals++
			}
		}

		if status == labelStatusActive || status == labelStatusDraft {
			active = append(active, summary)
		}
		if (status == "canceled" || status == "status:archived") && (summary.ActiveGoals > 0 || summary.DraftGoals > 0) {
			canceled = append(canceled, summary)
		}
	}
	return active, canceled
}

var opTriage = Op{
	Name: "triage",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in struct {
			Scope string `json:"scope,omitempty"`
		}
		_ = json.Unmarshal(raw, &in)

		labels := []string{}
		if in.Scope != "" {
			labels = append(labels, parchment.LabelPrefixScope+in.Scope)
		}

		out := triageOutput{}

		active, canceled := summarizeCampaigns(ctx, svc, labels)
		out.ActiveCampaigns = active
		out.CancelledWithActive = canceled

		now := time.Now()
		staleCutoff := now.Add(-7 * 24 * time.Hour)
		recentCutoff := now.Add(-24 * time.Hour)

		effortArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: labels, KindPrefix: "effort",
		})
		for _, art := range effortArts {
			status := parchment.StatusFromLabels(art.Labels)

			if status == labelStatusActive && !art.UpdatedAt.IsZero() && art.UpdatedAt.Before(staleCutoff) {
				out.StaleWork++
			}

			if strings.HasPrefix(status, "note.") || strings.HasPrefix(status, "inv.") {
				out.LifecycleMismatch++
			}

			incoming, _ := svc.Proto.Neighbors(ctx, art.ID, parchment.RelParentOf, parchment.Incoming)
			if len(incoming) == 0 && !svc.Proto.IsTerminal(status) {
				kind := art.Label(parchment.LabelPrefixKind)
				if kind != "effort.campaign" {
					out.OrphanEffort++
				}
			}
		}

		allArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: labels})
		for _, art := range allArts {
			if !art.UpdatedAt.IsZero() && art.UpdatedAt.After(recentCutoff) {
				out.RecentlyTouched++
			}
		}

		b, _ := json.Marshal(out)
		return string(b), nil
	},
}
