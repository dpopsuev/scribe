package mcp

// recall.go — knowledge(action=recall) implementation.
//
// Multi-pass FTS over knowledge artifacts ranked by:
//   BM25 score (from FTS5) × recency weight × kind weight
//
// Kind weight: evergreen note > fleeting note > concept > source > context > journal
// Recency weight: linear decay from 1.0 (today) to 0.5 (90+ days ago)
//
// Only knowledge kinds are searched — work artifacts (task, spec, bug, goal)
// are excluded. They're tracking, not memory.

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// knowledgeKindSet is the set of kinds that constitute agent memory.
var knowledgeKindSet = map[string]bool{
	parchment.KindNote:    true,
	parchment.KindJournal: true,
	parchment.KindSource:  true,
	parchment.KindConcept: true,
	parchment.KindContext: true,
}

// kindWeight returns the relevance multiplier for a knowledge kind + status.
// Evergreen notes are the most authoritative memory; journals the least.
func kindWeight(kind, status string) float64 {
	switch kind {
	case parchment.KindNote:
		if status == parchment.StatusEvergreen {
			return 2.0
		}
		return 1.0 // fleeting
	case parchment.KindConcept:
		return 1.4
	case parchment.KindSource:
		return 1.2
	case parchment.KindContext:
		return 1.1
	case parchment.KindJournal:
		return 0.8
	default:
		return 1.0
	}
}

// recencyWeight returns a [0.5, 1.0] multiplier decaying over 90 days.
func recencyWeight(updatedAt time.Time) float64 {
	if updatedAt.IsZero() {
		return 0.5
	}
	days := time.Since(updatedAt).Hours() / 24
	if days <= 0 {
		return 1.0
	}
	// Exponential decay: 1.0 at day 0, ~0.5 at day 90
	return 0.5 + 0.5*math.Exp(-days/130)
}

type recallResult struct {
	art   *parchment.Artifact
	score float64
}

func (h *handler) handleRecall(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	if in.Query == "" {
		return text("query is required for recall — describe what you want to remember"), nil, nil
	}

	scope := in.Scope
	if scope == "" && len(h.homeScopes) > 0 {
		scope = h.homeScopes[0]
	}

	// Multi-pass FTS: collect candidates from all passes, deduplicate, score.
	seen := make(map[string]bool)
	var candidates []*parchment.Artifact

	passes := buildFTSPasses(in.Query)
	for _, q := range passes {
		li := parchment.ListInput{Scope: scope}
		arts, err := h.proto.SearchArtifacts(ctx, q, li)
		if err != nil {
			continue
		}
		for _, a := range arts {
			if seen[a.ID] {
				continue
			}
			if !knowledgeKindSet[a.Kind] {
				continue
			}
			seen[a.ID] = true
			candidates = append(candidates, a)
		}
		if len(candidates) >= 20 {
			break
		}
	}

	if len(candidates) == 0 {
		return text(fmt.Sprintf("no memory found for %q — try capture or ingest to build up the vault", in.Query)), nil, nil
	}

	// Score and rank.
	queryTerms := strings.Fields(strings.ToLower(in.Query))
	var results []recallResult
	for _, a := range candidates {
		bm25 := termOverlap(a, queryTerms) // proxy for BM25 relevance
		score := bm25 * kindWeight(a.Kind, a.Status) * recencyWeight(a.UpdatedAt)
		results = append(results, recallResult{a, score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	limit := 5
	if len(results) < limit {
		limit = len(results)
	}
	results = results[:limit]

	var b strings.Builder
	fmt.Fprintf(&b, "Recall: %q\n\n", in.Query)
	for _, r := range results {
		fmt.Fprintf(&b, "[%s|%s] %s  %s\n", r.art.Kind, r.art.Status, r.art.ID, r.art.Title)
		if excerpt := extractExcerpt(r.art, queryTerms); excerpt != "" {
			fmt.Fprintf(&b, "  %s\n", excerpt)
		}
	}

	return text(b.String()), nil, nil
}

// buildFTSPasses generates FTS5 query strings from most to least strict.
// SQLite FTS5 MATCH syntax: quoted phrase, then all terms, then any term.
func buildFTSPasses(query string) []string {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil
	}
	words := strings.Fields(q)

	var passes []string
	// Pass 1: exact phrase
	passes = append(passes, `"`+strings.ReplaceAll(q, `"`, "")+`"`)
	// Pass 2: all terms (implicit AND in FTS5)
	if len(words) > 1 {
		passes = append(passes, strings.Join(words, " "))
	}
	// Pass 3: any significant term (OR via separate queries merged upstream)
	for _, w := range words {
		if len(w) >= 4 { // skip short stop words
			passes = append(passes, w)
		}
	}
	return passes
}

// termOverlap returns a simple relevance score: fraction of query terms
// that appear in the artifact's searchable text.
func termOverlap(art *parchment.Artifact, terms []string) float64 {
	if len(terms) == 0 {
		return 1.0
	}
	haystack := strings.ToLower(art.Title + " " + art.Goal)
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

// extractExcerpt returns a short snippet from the artifact most relevant to
// the query terms, truncated to ~120 chars.
func extractExcerpt(art *parchment.Artifact, terms []string) string {
	// Prefer section text over title/goal.
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
	if art.Goal != "" && len(art.Goal) <= 120 {
		return art.Goal
	}
	return ""
}
