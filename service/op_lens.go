package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type lensInput struct {
	Anchor    []string       `json:"anchor,omitempty"`
	AnchorOr  []string       `json:"anchor_or,omitempty"`
	AnchorIDs []string       `json:"anchor_ids,omitempty"`
	Traverse  []traverseRule `json:"traverse,omitempty"`
	Exclude   []string       `json:"exclude,omitempty"`
	Include   []string       `json:"include,omitempty"`
	MaxDepth  int            `json:"max_depth,omitempty"`
	Limit     int            `json:"limit,omitempty"`
	ScoreBy   string         `json:"score_by,omitempty"`
	ContextID string         `json:"context_id,omitempty"`
}

type traverseRule struct {
	Relation  string  `json:"relation,omitempty"`
	Direction string  `json:"direction,omitempty"`
	MaxDepth  int     `json:"max_depth,omitempty"`
	Weight    float64 `json:"weight,omitempty"`
}

func runLensAnalysis(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
	var in lensInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", err
	}

	var result *parchment.LensResult
	var err error

	if in.ContextID != "" {
		result, err = svc.Proto.ApplyLensFromArtifact(ctx, in.ContextID)
	} else {
		spec := parchment.LensSpec{
			Anchor:    in.Anchor,
			AnchorOr:  in.AnchorOr,
			AnchorIDs: in.AnchorIDs,
			Exclude:   in.Exclude,
			Include:   in.Include,
			MaxDepth:  in.MaxDepth,
			Limit:     in.Limit,
			ScoreBy:   in.ScoreBy,
		}
		for _, r := range in.Traverse {
			spec.Traverse = append(spec.Traverse, parchment.TraversalRule{
				Relation:  r.Relation,
				Direction: r.Direction,
				MaxDepth:  r.MaxDepth,
				Weight:    r.Weight,
			})
		}
		result, err = svc.Proto.ApplyLens(ctx, spec)
	}
	if err != nil {
		return "", err
	}

	return formatLensResult(result), nil
}

func formatLensResult(r *parchment.LensResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "lens projection: %d artifacts, %d seeds, %d edges\n",
		len(r.Entries), r.Stats.SeedCount, len(r.Edges))
	if r.Stats.ExcludedCount > 0 {
		fmt.Fprintf(&b, "  excluded: %d", r.Stats.ExcludedCount)
	}
	if r.Stats.MaxDepthHit {
		b.WriteString("  (depth limit reached)")
	}
	b.WriteString("\n\n")

	if len(r.Entries) == 0 {
		b.WriteString("No artifacts matched.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "%-40s %-20s %5s %10s  %s\n", "Title", "Kind", "Depth", "Score", "Via")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("─", 85))
	for _, e := range r.Entries {
		kind := parchment.LabelValue(e.Labels, parchment.LabelPrefixKind)
		fmt.Fprintf(&b, "%-40s %-20s %5d %10.4f  %s\n",
			truncate(e.Title, 38), kind, e.Depth, e.Score, e.Via)
	}
	return b.String()
}
