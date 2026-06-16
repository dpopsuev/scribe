package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type retentionInput struct {
	Scope      string `json:"scope,omitempty"`
	Kind       string `json:"kind,omitempty"`
	MinAgeDays int    `json:"min_age_days,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

var opRetention = Op{
	Name: "retention",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in retentionInput
		_ = json.Unmarshal(raw, &in)
		if in.Limit <= 0 {
			in.Limit = 50
		}

		policy := parchment.EvictionPolicy{
			Scope:      in.Scope,
			MinAgeDays: in.MinAgeDays,
		}
		if in.Kind != "" {
			policy.Kinds = []string{in.Kind}
		}

		candidates, err := svc.Proto.DetectEvictionCandidates(ctx, policy)
		if err != nil {
			return "", err
		}

		groups := map[parchment.EvictionLabel][]parchment.EvictionCandidate{}
		for _, c := range candidates {
			groups[c.Label] = append(groups[c.Label], c)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "retention analysis: %d candidates\n", len(candidates))
		if in.Scope != "" {
			fmt.Fprintf(&b, "scope: %s\n", in.Scope)
		}

		for _, label := range []parchment.EvictionLabel{
			parchment.EvictionLabelOrphaned,
			parchment.EvictionLabelStale,
			parchment.EvictionLabelCandidate,
		} {
			items := groups[label]
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&b, "\n## %s (%d)\n\n", label, len(items))
			fmt.Fprintf(&b, "%-40s %-18s %6s %6s %6s %6s  %s\n",
				"Title", "Kind", "Access", "Struct", "Qual", "Recen", "Reason")
			fmt.Fprintf(&b, "%s\n", strings.Repeat("─", 100))
			shown := 0
			for _, c := range items {
				if shown >= in.Limit {
					fmt.Fprintf(&b, "  ... and %d more\n", len(items)-shown)
					break
				}
				t := c.Tensor
				fmt.Fprintf(&b, "%-40s %-18s %6.2f %6.2f %6.2f %6.2f  %s\n",
					truncate(c.Artifact.Title, 38),
					c.Artifact.Label(parchment.LabelPrefixKind),
					t.AccessHeat, t.StructuralHeat, t.QualityScore, t.Recency,
					c.Reason)
				shown++
			}
		}

		if len(candidates) == 0 {
			fmt.Fprintf(&b, "\nAll artifacts are evergreen or active — nothing to review.\n")
		}
		return b.String(), nil
	},
}
