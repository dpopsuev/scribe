package server

import (
	"sort"
	"strings"
	"unicode"
)

// TriageResult is the output of intent matching against registered tools.
type TriageResult struct {
	Category   string      `json:"category"`
	Confidence float64     `json:"confidence"`
	Tools      []ToolMatch `json:"tools"`
}

// ToolMatch represents a ranked tool recommendation.
type ToolMatch struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
	Reason string         `json:"reason"`
}

// TriageRegistry scores and ranks tools by intent matching.
type TriageRegistry struct {
	tools []ToolMeta
}

// NewTriageRegistry creates an empty triage registry.
func NewTriageRegistry() *TriageRegistry {
	return &TriageRegistry{}
}

// Register adds a tool to the triage registry. Keywords are lowered for matching.
func (r *TriageRegistry) Register(meta ToolMeta) {
	lower := make([]string, len(meta.Keywords))
	for i, k := range meta.Keywords {
		lower[i] = strings.ToLower(k)
	}
	meta.Keywords = lower
	r.tools = append(r.tools, meta)
}

// List returns all registered tools.
func (r *TriageRegistry) List() []ToolMeta {
	out := make([]ToolMeta, len(r.tools))
	copy(out, r.tools)
	return out
}

// ByCategory returns tools matching the given category.
func (r *TriageRegistry) ByCategory(cat string) []ToolMeta {
	var out []ToolMeta
	for _, t := range r.tools {
		for _, c := range t.Categories {
			if c == cat {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// Triage matches an intent string against registered tools and returns
// ranked recommendations. Uses Jaccard similarity with prefix matching.
func (r *TriageRegistry) Triage(intent, path string) TriageResult {
	if intent == "" || len(r.tools) == 0 {
		return TriageResult{Category: "general", Tools: []ToolMatch{}}
	}

	tokens := tokenize(intent)

	// Score each tool and aggregate by category.
	type scored struct {
		meta  ToolMeta
		score float64
	}
	all := make([]scored, 0, len(r.tools))
	catScore := make(map[string]float64)

	for _, t := range r.tools {
		s := jaccardScore(tokens, t.Keywords)
		all = append(all, scored{meta: t, score: s})
		for _, cat := range t.Categories {
			catScore[cat] += s
		}
	}

	// Pick best category.
	bestCat := ""
	bestScore := 0.0
	for cat, s := range catScore {
		if s > bestScore || (s == bestScore && cat < bestCat) {
			bestCat = cat
			bestScore = s
		}
	}

	if bestCat == "" {
		return TriageResult{Category: "general", Tools: []ToolMatch{}}
	}

	// Filter to tools in best category, sort by priority then score.
	var matches []scored
	for _, s := range all {
		for _, c := range s.meta.Categories {
			if c == bestCat {
				matches = append(matches, s)
				break
			}
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].meta.Priority != matches[j].meta.Priority {
			return matches[i].meta.Priority < matches[j].meta.Priority
		}
		return matches[i].score > matches[j].score
	})

	// Build result.
	confidence := bestScore
	if len(catScore) > 0 {
		confidence = bestScore / float64(len(catScore))
	}
	if confidence > 1.0 {
		confidence = 1.0
	}

	tools := make([]ToolMatch, 0, len(matches))
	for _, m := range matches {
		params := make(map[string]any)
		for k, v := range m.meta.DefaultArgs {
			params[k] = v
		}
		if path != "" {
			params["path"] = path
		}

		reason := m.meta.Description
		if r, ok := m.meta.Rationale[bestCat]; ok {
			reason = r
		}

		tools = append(tools, ToolMatch{
			Name:   m.meta.Name,
			Params: params,
			Reason: reason,
		})
	}

	return TriageResult{
		Category:   bestCat,
		Confidence: confidence,
		Tools:      tools,
	}
}

// stop words filtered during tokenization.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"in": true, "on": true, "at": true, "to": true, "for": true,
	"of": true, "and": true, "or": true, "my": true, "me": true,
	"i": true, "it": true, "do": true, "does": true, "this": true,
	"that": true, "with": true, "from": true, "can": true, "how": true,
}

// tokenize splits intent into lowercase tokens, stripping stop words.
func tokenize(intent string) []string {
	words := strings.FieldsFunc(strings.ToLower(intent), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var tokens []string
	for _, w := range words {
		if !stopWords[w] && len(w) > 1 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}

// jaccardScore computes Jaccard similarity with bidirectional prefix matching.
func jaccardScore(intentTokens, keywords []string) float64 {
	if len(intentTokens) == 0 || len(keywords) == 0 {
		return 0
	}

	intersection := 0
	for _, t := range intentTokens {
		for _, k := range keywords {
			if t == k || strings.HasPrefix(t, k) || strings.HasPrefix(k, t) {
				intersection++
				break
			}
		}
	}

	// Union = unique tokens from both sets.
	seen := make(map[string]bool)
	for _, t := range intentTokens {
		seen[t] = true
	}
	for _, k := range keywords {
		seen[k] = true
	}

	if len(seen) == 0 {
		return 0
	}

	return float64(intersection) / float64(len(seen))
}
