//nolint:goconst // lint action/status literals
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type lintInput struct {
	ID    string   `json:"id,omitempty"`
	IDs   []string `json:"ids,omitempty"`
	Scope string   `json:"scope,omitempty"`
}

type lintFinding struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Category string `json:"category"`
	Detail   string `json:"detail"`
}

type lintResult struct {
	Action   string        `json:"action"`
	Status   string        `json:"status"`
	Count    int           `json:"count"`
	Findings []lintFinding `json:"findings"`
}

var opLint = Op{
	Name:       "lint",
	Structured: runLintStructured,
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		r, err := runLintStructured(ctx, svc, raw)
		return r.Text, err
	},
}

func runLintStructured(ctx context.Context, svc *Service, raw json.RawMessage) (Result, error) {
	var in lintInput
	_ = json.Unmarshal(raw, &in)

	arts, err := collectLintArtifacts(ctx, svc, in)
	if err != nil {
		return Result{}, err
	}

	var findings []lintFinding
	for _, art := range arts {
		kind := art.Label(parchment.LabelPrefixKind)
		if isCodeKind(kind) {
			continue
		}
		if isIntentionalOrphan(art) {
			continue
		}

		outEdges, _ := svc.Proto.Neighbors(ctx, art.ID, "", parchment.Outgoing)
		inEdges, _ := svc.Proto.Neighbors(ctx, art.ID, "", parchment.Incoming)
		if len(outEdges) == 0 && len(inEdges) == 0 {
			findings = append(findings, lintFinding{
				ID: art.ID, Title: art.Title, Kind: kind,
				Category: "orphan",
				Detail:   "no edges (incoming or outgoing)",
			})
		}

		score := contentCompleteness(svc, art)
		if score < 1.0 && score > 0 {
			if missing := missingSections(svc.Proto.ShouldSections(kind), art.Sections); len(missing) > 0 {
				findings = append(findings, lintFinding{
					ID: art.ID, Title: art.Title, Kind: kind,
					Category: "incomplete",
					Detail:   fmt.Sprintf("missing sections: %s", strings.Join(missing, ", ")),
				})
			}
		}
	}

	lr := lintResult{Action: "lint", Status: "ok", Count: len(findings), Findings: findings}
	if len(findings) == 0 {
		return Result{Text: "lint: no issues found", Data: lr}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "lint: %d issues found\n\n", len(findings))
	for _, f := range findings {
		fmt.Fprintf(&b, "[%s] %s — %s (%s)\n", f.Category, f.ID, f.Detail, f.Title)
	}
	return Result{Text: b.String(), Data: lr}, nil
}

func collectLintArtifacts(ctx context.Context, svc *Service, in lintInput) ([]*parchment.Artifact, error) {
	ids := resolveIDs(in.IDs, in.ID)
	if len(ids) > 0 {
		var arts []*parchment.Artifact
		for _, id := range ids {
			root, err := svc.Proto.GetArtifact(ctx, id)
			if err != nil {
				return nil, err
			}
			arts = append(arts, root)
			_ = svc.Proto.Walk(ctx, id, parchment.RelParentOf, parchment.Outgoing, 0, func(_ int, e parchment.Edge) bool {
				if child, err := svc.Proto.Get(ctx, e.To); err == nil {
					arts = append(arts, child)
				}
				return true
			})
		}
		return arts, nil
	}

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
	return svc.Proto.ListArtifacts(ctx, li)
}

func missingSections(should []string, sections []parchment.Section) []string {
	if len(should) == 0 {
		return nil
	}
	existing := make(map[string]bool, len(sections))
	for _, s := range sections {
		if strings.TrimSpace(s.Text) != "" {
			existing[s.Name] = true
		}
	}
	var missing []string
	for _, name := range should {
		if !existing[name] {
			missing = append(missing, name)
		}
	}
	return missing
}
