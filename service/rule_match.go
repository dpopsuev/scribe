package service

import (
	"context"
	"path/filepath"
	"sort"

	parchment "github.com/dpopsuev/parchment"
)

// MatchRules returns support.rule artifacts whose labels or globs match
// the given file path. This is the core of contextual rule resolution —
// "given this file, which rules apply?"
func (s *Service) MatchRules(ctx context.Context, filePath string, extraLabels []string) ([]RecallResult, error) {
	rules, err := s.Proto.Store().List(ctx, parchment.Filter{
		Labels: []string{parchment.LabelPrefixKind + "support.rule"},
	})
	if err != nil {
		return nil, err
	}

	var results []RecallResult
	for _, rule := range rules {
		score := matchScore(rule, filePath, extraLabels)
		if score <= 0 {
			continue
		}
		results = append(results, RecallResult{Art: rule, Score: score})
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results, nil
}

func matchScore(rule *parchment.Artifact, filePath string, extraLabels []string) float64 {
	score := 0.0

	// Check always_apply
	if alwaysApply, ok := rule.Extra["always_apply"].(bool); ok && alwaysApply {
		score += 10.0
	}

	// Check glob match
	if globs, ok := rule.Extra["globs"].([]any); ok {
		for _, g := range globs {
			pattern, ok := g.(string)
			if !ok {
				continue
			}
			if matched, _ := filepath.Match(pattern, filePath); matched {
				score += 5.0
				break
			}
			if matched, _ := filepath.Match(pattern, filepath.Base(filePath)); matched {
				score += 5.0
				break
			}
		}
	}

	// Check label overlap — rule labels vs requested labels
	if len(extraLabels) > 0 {
		ruleLabels := make(map[string]bool, len(rule.Labels))
		for _, l := range rule.Labels {
			ruleLabels[l] = true
		}
		for _, want := range extraLabels {
			if ruleLabels[want] {
				score += 2.0
			}
		}
	}

	if score <= 0 {
		return 0
	}
	// Priority boost — only applied when something already matched
	if p, ok := rule.Extra["priority"].(float64); ok {
		score += p / 100.0
	}

	return score
}
