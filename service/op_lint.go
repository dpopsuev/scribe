package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type lintInput struct {
	ID    string `json:"id,omitempty"`
	Scope string `json:"scope,omitempty"`
}

type lintFinding struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Detail   string `json:"detail"`
}

var opLint = Op{
	Name: "lint",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in lintInput
		_ = json.Unmarshal(raw, &in)

		var arts []*parchment.Artifact

		if in.ID != "" {
			root, err := svc.Proto.GetArtifact(ctx, in.ID)
			if err != nil {
				return "", err
			}
			arts = append(arts, root)
			_ = svc.Proto.Store().Walk(ctx, in.ID, parchment.RelParentOf, parchment.Outgoing, 0, func(_ int, e parchment.Edge) bool {
				if child, err := svc.Proto.Store().Get(ctx, e.To); err == nil {
					arts = append(arts, child)
				}
				return true
			})
		} else {
			schemaKinds := []string{
				parchment.LabelPrefixKind + "edge_type_definition",
				parchment.LabelPrefixKind + "label_definition",
				parchment.LabelPrefixKind + "relationship",
				parchment.LabelPrefixKind + "support.rule",
				parchment.LabelPrefixKind + "support.config",
				parchment.LabelPrefixKind + "support.template",
			}
			li := parchment.ListInput{ExcludeLabels: schemaKinds}
			if in.Scope != "" {
				li.Labels = []string{parchment.LabelPrefixScope + in.Scope}
			}
			var err error
			arts, err = svc.Proto.ListArtifacts(ctx, li)
			if err != nil {
				return "", err
			}
		}

		var findings []lintFinding

		for _, art := range arts {
			kind := art.Label(parchment.LabelPrefixKind)

			outEdges, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Outgoing)
			inEdges, _ := svc.Proto.Store().Neighbors(ctx, art.ID, "", parchment.Incoming)
			if len(outEdges) == 0 && len(inEdges) == 0 {
				findings = append(findings, lintFinding{
					ID: art.ID, Title: art.Title, Kind: kind,
					Category: "orphan",
					Detail:   "no edges (incoming or outgoing)",
				})
			}

			score := svc.Proto.CompletionScore(ctx, art)
			if score < 1.0 && score > 0 {
				shouldSections := svc.Proto.ShouldSections(kind)
				if len(shouldSections) > 0 {
					var missing []string
					existing := make(map[string]bool)
					for _, s := range art.Sections {
						if strings.TrimSpace(s.Text) != "" {
							existing[s.Name] = true
						}
					}
					for _, name := range shouldSections {
						if !existing[name] {
							missing = append(missing, name)
						}
					}
					if len(missing) > 0 {
						findings = append(findings, lintFinding{
							ID: art.ID, Title: art.Title, Kind: kind,
							Category: "incomplete",
							Detail:   fmt.Sprintf("missing sections: %s", strings.Join(missing, ", ")),
						})
					}
				}
			}
		}

		if len(findings) == 0 {
			return "lint: no issues found", nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "lint: %d issues found\n\n", len(findings))
		for _, f := range findings {
			fmt.Fprintf(&b, "[%s] %s — %s (%s)\n", f.Category, f.ID, f.Detail, f.Title)
		}
		return b.String(), nil
	},
}
