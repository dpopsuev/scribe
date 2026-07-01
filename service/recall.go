package service

// recall.go — Recall business logic.
// Multi-pass FTS over knowledge artifacts ranked by kind weight × recency decay.

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const (
	kindCampaign = "effort.campaign"
	kindGoal     = "effort.goal"
)

// recallableKinds are always recallable regardless of status.
var recallableKinds = map[string]bool{
	"knowledge.note":    true,
	"knowledge.journal": true,
	"knowledge.source":  true,
	"knowledge.concept": true,
	"knowledge.context": true,
	"agent.memory":      true,
	kindCampaign:        true,
	kindGoal:            true,
	kindTask:            true,
	"intent.bug":        true,
	"intent.spec":       true,
	"intent.need":       true,
	"intent.decision":   true,
}

// IsRecallable returns true when an artifact should be included in recall.
func IsRecallable(art *parchment.Artifact, proto *parchment.Protocol) bool {
	kind := art.Label(parchment.LabelPrefixKind)
	if proto.IsRecallable(kind) {
		return true
	}
	if recallableKinds[kind] {
		return true
	}
	return proto.IsTerminal(parchment.StatusFromLabels(art.Labels))
}

// KindWeight returns the relevance multiplier for a kind + status combination.
const defaultRecallWeight = 0.7 //nolint:mnd // baseline for unknown kinds

// KindWeight returns the recall ranking weight for a kind.
// Uses trait-driven RecallWeight when available, falls back to hardcoded map.
func KindWeight(kind, status string, proto *parchment.Protocol) float64 {
	if w := proto.RecallWeight(kind); w > 0 {
		if kind == "knowledge.note" && status == "note.evergreen" {
			return w * 2 //nolint:mnd // evergreen notes get 2x boost
		}
		return w
	}
	return defaultRecallWeight
}

// RecencyWeight returns a [0.5, 1.0] multiplier decaying over 90 days.
func RecencyWeight(updatedAt time.Time) float64 {
	if updatedAt.IsZero() {
		return 0.5
	}
	days := time.Since(updatedAt).Hours() / 24
	if days <= 0 {
		return 1.0
	}
	return 0.5 + 0.5*math.Exp(-days/130)
}

// RecallResult holds a scored artifact for sorting.
type RecallResult struct {
	Art   *parchment.Artifact
	Score float64
}

// Recall runs multi-pass FTS over knowledge artifacts and returns top-N ranked results.
func (s *Service) Recall(ctx context.Context, query, scope string, top int) ([]RecallResult, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required") //nolint:err113 // agent-facing, inline message is the contract
	}
	if scope == "" && len(s.HomeScopes) > 0 {
		scope = s.HomeScopes[0]
	}

	seen := make(map[string]bool)
	var candidates []*parchment.Artifact
	for _, q := range BuildFTSPasses(query) {
		recallLabels := []string{}
		if scope != "" {
			recallLabels = append(recallLabels, parchment.LabelPrefixScope+scope)
		}
		arts, err := s.Proto.SearchArtifacts(ctx, q, parchment.ListInput{Labels: recallLabels})
		if err != nil {
			continue
		}
		for _, a := range arts {
			if seen[a.ID] || !IsRecallable(a, s.Proto) {
				continue
			}
			seen[a.ID] = true
			candidates = append(candidates, a)
		}
		if len(candidates) >= 20 {
			break
		}
	}

	queryTerms := strings.Fields(strings.ToLower(query))
	var results []RecallResult
	for _, a := range candidates {
		bm25 := TermOverlap(a, queryTerms)
		score := bm25 * KindWeight(a.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(a.Labels), s.Proto) * RecencyWeight(a.UpdatedAt)
		fanIn, _ := FanIn(ctx, s.Proto.Store(), a.ID)
		score *= 1.0 + math.Log1p(float64(fanIn))*0.1
		results = append(results, RecallResult{a, score})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	if top <= 0 {
		top = 5
	}
	if len(results) < top {
		top = len(results)
	}
	return results[:top], nil
}

// BuildFTSPasses generates FTS5 query strings from most to least strict.
func BuildFTSPasses(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	words := strings.Fields(q)
	var passes []string
	passes = append(passes, `"`+strings.ReplaceAll(q, `"`, "")+`"`)
	if len(words) > 1 {
		passes = append(passes, strings.Join(words, " "))
	}
	for _, w := range words {
		if len(w) >= 4 {
			passes = append(passes, w)
		}
	}
	return passes
}

// TermOverlap returns the fraction of query terms that appear in the artifact text.
func TermOverlap(art *parchment.Artifact, terms []string) float64 {
	if len(terms) == 0 {
		return 1.0
	}
	haystack := strings.ToLower(art.Title + " " + art.Goal())
	for _, sec := range art.Sections {
		haystack += " " + strings.ToLower(sec.Text)
	}
	hits := 0
	for _, t := range terms {
		if strings.Contains(haystack, t) {
			hits++
		}
	}
	return float64(hits) / float64(len(terms))
}

// ExtractExcerpt returns a short snippet most relevant to query terms.
func ExtractExcerpt(art *parchment.Artifact, terms []string) string {
	for _, sec := range art.Sections {
		lower := strings.ToLower(sec.Text)
		for _, t := range terms {
			idx := strings.Index(lower, t)
			if idx < 0 {
				continue
			}
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + 80
			if end > len(sec.Text) {
				end = len(sec.Text)
			}
			excerpt := strings.TrimSpace(sec.Text[start:end])
			if len(excerpt) > 120 {
				excerpt = excerpt[:117] + "…"
			}
			return excerpt
		}
	}
	if goal := art.Goal(); goal != "" && len(goal) <= 120 {
		return goal
	}
	return ""
}
