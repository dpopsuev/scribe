package service

import (
	"context"
	"encoding/json"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

type synthesizeInput struct {
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Sources []string `json:"sources,omitempty"`
	Scope   string   `json:"scope,omitempty"`
	Labels  []string `json:"labels,omitempty"`
}

var opSynthesize = Op{
	Name: "synthesize",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in synthesizeInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Title == "" || in.Body == "" {
			return "", fmt.Errorf("title and body are required") //nolint:err113 // agent-facing
		}

		labels := append([]string{"kind:knowledge.note"}, in.Labels...)
		if in.Scope != "" {
			labels = append(labels, parchment.LabelPrefixScope+in.Scope)
		}

		ci := parchment.CreateInput{
			Title:    in.Title,
			Labels:   labels,
			Sections: []parchment.Section{{Name: "body", Text: in.Body}},
		}
		art, err := svc.Proto.CreateArtifact(ctx, ci)
		if err != nil {
			return "", fmt.Errorf("create synthesis note: %w", err)
		}

		for _, src := range in.Sources {
			if _, err := svc.Proto.GetArtifact(ctx, src); err != nil {
				continue
			}
			_ = svc.Proto.AddEdgeSource(ctx, art.ID, parchment.RelCites, src, "synthesize")
		}

		out := fmt.Sprintf("created %s (%s)", art.ID, art.Title)
		if len(in.Sources) > 0 {
			out += fmt.Sprintf(", cited %d source(s)", len(in.Sources))
		}
		return out, nil
	},
}
