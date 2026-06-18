package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type lensCreateInput struct {
	Title    string         `json:"title"`
	Scope    string         `json:"scope,omitempty"`
	Anchor   []string       `json:"anchor,omitempty"`
	AnchorOr []string       `json:"anchor_or,omitempty"`
	Traverse []traverseRule `json:"traverse,omitempty"`
	Exclude  []string       `json:"exclude,omitempty"`
	Include  []string       `json:"include,omitempty"`
	MaxDepth int            `json:"max_depth,omitempty"`
	ScoreBy  string         `json:"score_by,omitempty"`
}

var opLensCreate = Op{
	Name: "lens_create",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in lensCreateInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Title == "" {
			return "", fmt.Errorf("title is required") //nolint:err113 // user-facing validation
		}
		if len(in.Anchor) == 0 && len(in.AnchorOr) == 0 {
			return "", fmt.Errorf("at least one anchor or anchor_or label is required") //nolint:err113 // user-facing validation
		}

		extra := map[string]any{}
		if len(in.Anchor) > 0 {
			extra["lens_anchor"] = in.Anchor
		}
		if len(in.AnchorOr) > 0 {
			extra["lens_anchor_or"] = in.AnchorOr
		}
		if len(in.Exclude) > 0 {
			extra["lens_exclude"] = in.Exclude
		}
		if len(in.Include) > 0 {
			extra["lens_include"] = in.Include
		}
		if in.ScoreBy != "" {
			extra["lens_score_by"] = in.ScoreBy
		}
		if in.MaxDepth > 0 {
			extra["lens_max_depth"] = in.MaxDepth
		}
		if len(in.Traverse) > 0 {
			rules := make([]map[string]any, len(in.Traverse))
			for i, r := range in.Traverse {
				rule := map[string]any{"relation": r.Relation}
				if r.Direction != "" {
					rule["direction"] = r.Direction
				}
				if r.MaxDepth > 0 {
					rule["max_depth"] = r.MaxDepth
				}
				if r.Weight > 0 {
					rule["weight"] = r.Weight
				}
				rules[i] = rule
			}
			extra["lens_traverse"] = rules
		}

		labels := []string{parchment.LabelPrefixKind + "knowledge.context"}
		if in.Scope != "" {
			labels = append(labels, parchment.LabelPrefixScope+in.Scope)
		}

		ci := parchment.CreateInput{
			Title:  in.Title,
			Labels: labels,
			Extra:  extra,
		}
		art, err := svc.Proto.CreateArtifact(ctx, ci)
		if err != nil {
			return "", err
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Created lens: %s\n", art.ID)
		fmt.Fprintf(&b, "  title:   %s\n", art.Title)
		if len(in.Anchor) > 0 {
			fmt.Fprintf(&b, "  anchor:  %s\n", strings.Join(in.Anchor, ", "))
		}
		if len(in.Traverse) > 0 {
			for _, r := range in.Traverse {
				dir := r.Direction
				if dir == "" {
					dir = "outgoing"
				}
				fmt.Fprintf(&b, "  traverse: %s %s depth=%d\n", r.Relation, dir, r.MaxDepth)
			}
		}
		return b.String(), nil
	},
}

var opLensList = Op{
	Name: "lens_list",
	Run: func(ctx context.Context, svc *Service, _ json.RawMessage) (string, error) {
		arts, err := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: []string{parchment.LabelPrefixKind + "knowledge.context"},
		})
		if err != nil {
			return "", err
		}

		var lenses []*parchment.Artifact
		for _, a := range arts {
			if a.Extra == nil {
				continue
			}
			if _, ok := a.Extra["lens_anchor"]; ok {
				lenses = append(lenses, a)
			} else if _, ok := a.Extra["lens_anchor_or"]; ok {
				lenses = append(lenses, a)
			}
		}

		if len(lenses) == 0 {
			return "No stored lenses.\n", nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "%-40s  %s\n", "ID", "Title")
		fmt.Fprintf(&b, "%s\n", strings.Repeat("─", 60))
		for _, l := range lenses {
			fmt.Fprintf(&b, "%-40s  %s\n", truncate(l.ID, 38), l.Title)
		}
		return b.String(), nil
	},
}

func init() {
	Registry = append(Registry, opLensCreate, opLensList)
}
