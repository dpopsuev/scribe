package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type analyzeInput struct {
	Mode       string `json:"mode"`
	ID         string `json:"id,omitempty"`
	From       string `json:"from,omitempty"`
	To         string `json:"to,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Sort       string `json:"sort,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	MinShared  int    `json:"min_shared,omitempty"`
	MaxDepth   int    `json:"max_depth,omitempty"`
	Iterations int    `json:"iterations,omitempty"`
}

var opAnalyze = Op{
	Name: "analyze",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in analyzeInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Limit <= 0 {
			in.Limit = 20
		}

		switch in.Mode {
		case "fan":
			return runFanAnalysis(ctx, svc, &in)
		case "co_citation":
			return runOverlapAnalysis(ctx, svc, &in, parchment.Incoming, "co-citation analysis")
		case "coupling":
			return runOverlapAnalysis(ctx, svc, &in, parchment.Outgoing, "bibliographic coupling")
		case "paths":
			return runPathAnalysis(ctx, svc, &in)
		case "pagerank":
			return runPageRankAnalysis(ctx, svc, &in)
		case "lens":
			return runLensAnalysis(ctx, svc, raw)
		default:
			return "", fmt.Errorf("unknown analyze mode %q; valid: fan, co_citation, coupling, paths, pagerank, lens", in.Mode) //nolint:err113 // user-facing input validation
		}
	},
}

func runFanAnalysis(ctx context.Context, svc *Service, in *analyzeInput) (string, error) {
	labels := scopeLabels(in.Scope)
	arts, err := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: labels})
	if err != nil {
		return "", err
	}

	scores := make([]FanScore, 0, len(arts))
	for _, a := range arts {
		fi, _ := FanIn(ctx, svc.Proto.Store(), a.ID)
		fo, _ := FanOut(ctx, svc.Proto.Store(), a.ID)
		scores = append(scores, FanScore{
			ID: a.ID, Title: a.Title,
			Kind:  a.Label(parchment.LabelPrefixKind),
			FanIn: fi, FanOut: fo,
		})
	}

	switch in.Sort {
	case "fan_out":
		sort.Slice(scores, func(i, j int) bool { return scores[i].FanOut > scores[j].FanOut })
	default:
		sort.Slice(scores, func(i, j int) bool { return scores[i].FanIn > scores[j].FanIn })
	}

	if len(scores) > in.Limit {
		scores = scores[:in.Limit]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "fan analysis: %d artifacts (top %d)\n\n", len(arts), len(scores))
	fmt.Fprintf(&b, "%-40s %-20s %6s %7s\n", "ID", "Kind", "FanIn", "FanOut")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("─", 75))
	for _, s := range scores {
		fmt.Fprintf(&b, "%-40s %-20s %6d %7d\n", truncate(s.Title, 38), s.Kind, s.FanIn, s.FanOut)
	}
	return b.String(), nil
}

func runOverlapAnalysis(ctx context.Context, svc *Service, in *analyzeInput, dir parchment.Direction, header string) (string, error) {
	if in.ID == "" {
		return "", fmt.Errorf("%s requires id", in.Mode) //nolint:err113 // user-facing input validation
	}
	if in.MinShared <= 0 {
		in.MinShared = 1
	}
	results, err := FindCoCitations(ctx, svc.Proto.Store(), in.ID, dir, in.MinShared, in.Limit)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s for %s (min_shared=%d)\n\n", header, in.ID, in.MinShared)
	if len(results) == 0 {
		fmt.Fprintf(&b, "No results found.\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "%-40s %-20s %7s\n", "Title", "Kind", "Overlap")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("─", 69))
	for _, r := range results {
		fmt.Fprintf(&b, "%-40s %-20s %7d\n", truncate(r.Title, 38), r.Kind, r.Overlap)
	}
	return b.String(), nil
}

func runPathAnalysis(ctx context.Context, svc *Service, in *analyzeInput) (string, error) {
	if in.From == "" || in.To == "" {
		return "", fmt.Errorf("paths requires from and to") //nolint:err113 // user-facing input validation
	}
	if in.MaxDepth <= 0 {
		in.MaxDepth = 5
	}
	path, err := ShortestPath(ctx, svc.Proto.Store(), in.From, in.To, in.MaxDepth)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	if path == nil {
		fmt.Fprintf(&b, "no path from %s to %s (max_depth=%d)\n", in.From, in.To, in.MaxDepth)
		return b.String(), nil
	}
	fmt.Fprintf(&b, "shortest path (%d hops):\n\n", len(path))
	for i, e := range path {
		if i == 0 {
			fmt.Fprintf(&b, "  %s\n", artTitle(ctx, svc, e.From))
		}
		fmt.Fprintf(&b, "    ──%s──▶\n  %s\n", e.Relation, artTitle(ctx, svc, e.To))
	}
	return b.String(), nil
}

func runPageRankAnalysis(ctx context.Context, svc *Service, in *analyzeInput) (string, error) {
	labels := scopeLabels(in.Scope)
	iterations := in.Iterations
	if iterations <= 0 {
		iterations = 20
	}
	results, err := ComputePageRank(ctx, svc.Proto.Store(), labels, iterations, 0.85)
	if err != nil {
		return "", err
	}
	if len(results) > in.Limit {
		results = results[:in.Limit]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "pagerank (%d iterations, damping=0.85)\n\n", iterations)
	if len(results) == 0 {
		fmt.Fprintf(&b, "No artifacts in scope.\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "%-40s %-20s %10s\n", "Title", "Kind", "Score")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("─", 72))
	for _, r := range results {
		fmt.Fprintf(&b, "%-40s %-20s %10.6f\n", truncate(r.Title, 38), r.Kind, r.Score)
	}
	return b.String(), nil
}

func scopeLabels(scope string) []string {
	if scope == "" {
		return nil
	}
	return []string{parchment.LabelPrefixScope + scope}
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}

func artTitle(ctx context.Context, svc *Service, id string) string {
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return id
	}
	return art.Title
}
