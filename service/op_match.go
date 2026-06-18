package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type matchInput struct {
	File   string   `json:"file"`
	Labels []string `json:"labels,omitempty"`
	Top    int      `json:"top,omitempty"`
}

var opMatchRules = Op{
	Name: "match_rules",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in matchInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.File == "" && len(in.Labels) == 0 {
			return "", fmt.Errorf("file or labels required") //nolint:err113 // agent-facing hint
		}
		if in.Top <= 0 {
			in.Top = 10
		}
		results, err := svc.MatchRules(ctx, in.File, in.Labels)
		if err != nil {
			return "", err
		}
		if len(results) > in.Top {
			results = results[:in.Top]
		}
		if len(results) == 0 {
			return "no matching rules", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d rules matched:\n\n", len(results))
		for _, r := range results {
			fmt.Fprintf(&b, "## %s (score: %.1f)\n", r.Art.Title, r.Score)
			for _, sec := range r.Art.Sections {
				if sec.Name == "body" {
					fmt.Fprintf(&b, "%s\n\n", sec.Text)
					break
				}
			}
		}
		return b.String(), nil
	},
}

func init() {
	Registry = append(Registry, opMatchRules)
}
