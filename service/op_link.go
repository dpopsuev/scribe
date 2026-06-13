package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type edgeInput struct {
	From     string `json:"from"`
	Relation string `json:"relation"`
	To       string `json:"to"`
}

type linkInput struct {
	ID        string      `json:"id"`
	Relation  string      `json:"relation"`
	Targets   []string    `json:"targets,omitempty"`
	Target    string      `json:"target,omitempty"`
	OldTarget string      `json:"old_target,omitempty"` // edge to replace when mode=replace
	Mode      string      `json:"mode,omitempty"`
	Weight    float64     `json:"weight,omitempty"`
	Edges     []edgeInput `json:"edges,omitempty"`
}

func execEdgeOp(ctx context.Context, svc *Service, in linkInput, unlink bool) (string, error) {
	verb := "linked"
	if unlink {
		verb = "unlinked"
	}
	callLink := func(ctx context.Context, from, rel string, targets []string) ([]parchment.Result, error) {
		if unlink {
			return svc.Proto.UnlinkArtifacts(ctx, from, rel, targets)
		}
		return svc.Proto.LinkArtifacts(ctx, from, rel, targets, in.Weight)
	}
	if len(in.Edges) > 0 {
		var lines []string
		for _, e := range in.Edges {
			results, err := callLink(ctx, e.From, e.Relation, []string{e.To})
			if err != nil {
				lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, err))
				continue
			}
			for _, r := range results {
				if r.OK {
					lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, e.From, e.Relation, e.To))
				} else {
					lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, r.Error))
				}
			}
		}
		return strings.Join(lines, "\n"), nil
	}
	if in.ID == "" || len(in.Targets) == 0 || in.Relation == "" {
		return "", fmt.Errorf("id, relation, and targets required") //nolint:err113 // user-facing hint
	}
	results, err := callLink(ctx, in.ID, in.Relation, in.Targets)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, in.ID, in.Relation, r.ID))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return strings.Join(lines, "\n"), nil
}

var opLink = Op{
	Name: "link",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in linkInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Mode == "remove" || in.Mode == "unlink" {
			return execEdgeOp(ctx, svc, in, true)
		}
		if in.OldTarget != "" {
			if in.ID == "" || in.Relation == "" || in.Target == "" {
				return "", fmt.Errorf("id, relation, replace_from, and target required") //nolint:err113 // user-facing hint
			}
			if _, err := svc.Proto.UnlinkArtifacts(ctx, in.ID, in.Relation, []string{in.OldTarget}); err != nil {
				return "", fmt.Errorf("unlink old: %w", err)
			}
			if _, err := svc.Proto.LinkArtifacts(ctx, in.ID, in.Relation, []string{in.Target}, in.Weight); err != nil {
				return "", fmt.Errorf("link new: %w", err)
			}
			return fmt.Sprintf("replaced %s -[%s]-> %s with %s", in.ID, in.Relation, in.OldTarget, in.Target), nil
		}
		return execEdgeOp(ctx, svc, in, false)
	},
}
