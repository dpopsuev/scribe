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

type edgeOp func(ctx context.Context, from, rel string, targets []string) ([]parchment.Result, error)

func execEdgeOp(ctx context.Context, svc *Service, in linkInput, unlink bool) (string, error) {
	verb := "linked"
	if unlink {
		verb = "unlinked"
	}
	apply := linkFunc(svc, unlink, in.Weight)

	if len(in.Edges) > 0 {
		return execBulkEdges(ctx, apply, in.Edges, verb)
	}
	if in.ID == "" || len(in.Targets) == 0 || in.Relation == "" {
		return "", fmt.Errorf("id, relation, and targets required") //nolint:err113 // user-facing hint
	}
	return execSingleEdge(ctx, apply, in.ID, in.Relation, in.Targets, verb)
}

func linkFunc(svc *Service, unlink bool, weight float64) edgeOp {
	if unlink {
		return svc.Proto.UnlinkArtifacts
	}
	return func(ctx context.Context, from, rel string, targets []string) ([]parchment.Result, error) {
		return svc.Proto.LinkArtifacts(ctx, from, rel, targets, weight)
	}
}

func execBulkEdges(ctx context.Context, apply edgeOp, edges []edgeInput, verb string) (string, error) {
	var lines []string
	for _, e := range edges {
		results, err := apply(ctx, e.From, e.Relation, []string{e.To})
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, err))
			continue
		}
		lines = append(lines, formatEdgeResults(results, e.From, e.Relation, e.To, verb)...)
	}
	return strings.Join(lines, "\n"), nil
}

func execSingleEdge(ctx context.Context, apply edgeOp, id, relation string, targets []string, verb string) (string, error) {
	results, err := apply(ctx, id, relation, targets)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, id, relation, r.ID))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return strings.Join(lines, "\n"), nil
}

func formatEdgeResults(results []parchment.Result, from, rel, to, verb string) []string {
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, from, rel, to))
		} else {
			lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", from, rel, to, r.Error))
		}
	}
	return lines
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
