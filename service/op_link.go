//nolint:goconst // mutation action/status literals
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
	OldTarget string      `json:"old_target,omitempty"`
	Mode      string      `json:"mode,omitempty"`
	Weight    float64     `json:"weight,omitempty"`
	Edges     []edgeInput `json:"edges,omitempty"`
}

type edgeOp func(ctx context.Context, from, rel string, targets []string) ([]parchment.Result, error)

var opLink = Op{
	Name:       "link",
	Structured: runLinkStructured,
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		r, err := runLinkStructured(ctx, svc, raw)
		return r.Text, err
	},
}

func runLinkStructured(ctx context.Context, svc *Service, raw json.RawMessage) (Result, error) {
	var in linkInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{}, err
	}
	if in.Mode == "remove" || in.Mode == "unlink" {
		return execEdgeOpStructured(ctx, svc, in, true)
	}
	if in.OldTarget != "" {
		if in.ID == "" || in.Relation == "" || in.Target == "" {
			return Result{}, fmt.Errorf("id, relation, replace_from, and target required") //nolint:err113 // user-facing hint
		}
		oldEdges := ExpandGovernedBy(in.ID, in.Relation, []string{in.OldTarget})
		newEdges := ExpandGovernedBy(in.ID, in.Relation, []string{in.Target})
		for _, e := range oldEdges {
			if _, err := svc.Proto.UnlinkArtifacts(ctx, e.From, e.Relation, []string{e.To}); err != nil {
				return Result{}, fmt.Errorf("unlink old: %w", err)
			}
		}
		for i := range newEdges {
			newEdges[i].Weight = in.Weight
			if _, err := svc.Proto.LinkArtifacts(ctx, newEdges[i].From, newEdges[i].Relation, []string{newEdges[i].To}, in.Weight); err != nil {
				return Result{}, fmt.Errorf("link new: %w", err)
			}
		}
		mr := MutationResult{Action: "link", Status: "ok", Count: len(newEdges), Edges: newEdges}
		text := fmt.Sprintf("replaced %s -[%s]-> %s with %s", in.ID, in.Relation, in.OldTarget, in.Target)
		return Result{Text: text, Data: mr}, nil
	}
	return execEdgeOpStructured(ctx, svc, in, false)
}

func execEdgeOpStructured(ctx context.Context, svc *Service, in linkInput, unlink bool) (Result, error) {
	verb := "linked"
	action := "link"
	if unlink {
		verb = "unlinked"
		action = "unlink"
	}
	apply := linkFunc(svc, unlink, in.Weight)

	var edges []EdgeRef
	var lines []string
	var warnings []string

	var planned []EdgeRef
	if len(in.Edges) > 0 {
		for _, e := range in.Edges {
			planned = append(planned, ExpandGovernedBy(e.From, e.Relation, []string{e.To})...)
		}
	} else {
		if in.ID == "" || len(in.Targets) == 0 || in.Relation == "" {
			return Result{}, fmt.Errorf("id, relation, and targets required") //nolint:err113 // user-facing hint
		}
		planned = ExpandGovernedBy(in.ID, in.Relation, in.Targets)
	}
	for _, e := range planned {
		results, err := apply(ctx, e.From, e.Relation, []string{e.To})
		if err != nil {
			return Result{}, enrichLinkError(ctx, svc, e.From, e.Relation, err)
		}
		for _, r := range results {
			if r.OK {
				lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, e.From, e.Relation, e.To))
				edges = append(edges, EdgeRef{From: e.From, Relation: e.Relation, To: e.To, Weight: in.Weight})
			} else {
				lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, r.Error))
				warnings = append(warnings, r.Error)
			}
		}
	}
	mr := MutationResult{Action: action, Status: "ok", Edges: edges, Count: len(edges), Warnings: warnings}
	return Result{Text: strings.Join(lines, "\n"), Data: mr}, nil
}

func linkFunc(svc *Service, unlink bool, weight float64) edgeOp {
	if unlink {
		return svc.Proto.UnlinkArtifacts
	}
	return func(ctx context.Context, from, rel string, targets []string) ([]parchment.Result, error) {
		return svc.Proto.LinkArtifacts(ctx, from, rel, targets, weight)
	}
}

func enrichLinkError(ctx context.Context, svc *Service, from, relation string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if relation != parchment.RelParentOf && !strings.Contains(msg, "parent_of") {
		return err
	}
	src, serr := svc.Proto.GetArtifact(ctx, from)
	if serr != nil {
		return err
	}
	kind := src.Label(parchment.LabelPrefixKind)
	rels := svc.Proto.ValidRelationsFor(kind)
	var alts []string
	for _, r := range rels {
		switch r.Relation {
		case parchment.RelMentions, parchment.RelRelatesTo:
			alts = append(alts, r.Relation)
		}
	}
	if len(alts) == 0 {
		alts = []string{parchment.RelMentions, parchment.RelRelatesTo}
	}
	return fmt.Errorf("%w. Valid alternatives: %s, or use knowledge.context as container", //nolint:err113 // agent-facing
		err, strings.Join(uniqueStrings(alts), ", "))
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
