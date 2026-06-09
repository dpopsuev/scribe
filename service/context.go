package service

import (
	"context"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

const ruleTokenBudget = 4000

// RuleEntry is a resolved rule artifact ready for injection into agent context.
type RuleEntry struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Labels   []string `json:"labels,omitempty"`
	Priority int      `json:"priority"`
	Body     string   `json:"body"`
}

// KindHint carries agent guidance from the kind_definition artifact in _schema.
type KindHint struct {
	Kind         string `json:"kind"`
	WhenToCreate string `json:"when_to_create,omitempty"`
	AgentNote    string `json:"agent_note,omitempty"`
}

// ContextPacket is the four-worlds context assembled for a single task.
// Returned by ContextRead and consumed at agent session start.
type ContextPacket struct {
	Task      *parchment.Artifact   `json:"task"`
	Know      []*parchment.Artifact `json:"know,omitempty"`
	Rules     []RuleEntry           `json:"rules,omitempty"`
	KindHints []KindHint            `json:"kind_hints,omitempty"` // guidance from _schema for the task's kind
}

// ContextRead assembles the four-worlds context for a task: task artifact,
// knowledge layer (notes/concepts in scope), code pointers, and ranked rules.
func (s *Service) ContextRead(ctx context.Context, taskID string) (*ContextPacket, error) {
	task, err := s.Proto.GetArtifact(ctx, taskID)
	if err != nil {
		return nil, err
	}

	var know []*parchment.Artifact
	userLabels := userDefinedLabels(task.Labels)
	if task.Label(parchment.LabelPrefixScope) != "" && len(userLabels) > 0 {
		all, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{
			Scope:  task.Label(parchment.LabelPrefixScope),
			Labels: userLabels,
		})
		for _, art := range all {
			if art.Label(parchment.LabelPrefixKind) == parchment.KindNote || art.Label(parchment.LabelPrefixKind) == parchment.KindConcept {
				know = append(know, art)
			}
		}
	}

	rules := s.resolveRules(ctx, task.Labels)
	kindHints := s.resolveKindHints(ctx, task.Label(parchment.LabelPrefixKind))

	return &ContextPacket{
		Task:      task,
		Know:      know,
		Rules:     rules,
		KindHints: kindHints,
	}, nil
}

// resolveKindHints fetches agent guidance from the kind_definition artifact in _schema.
// Returns hints for the given kind and its family siblings so the agent understands
// when to create each kind without external documentation.
func (s *Service) resolveKindHints(ctx context.Context, kind string) []KindHint {
	// Fetch the definition for the task's kind plus common related kinds.
	kindsToFetch := []string{kind, "task", "goal", "spec", "bug", "need"}
	seen := make(map[string]bool)
	var hints []KindHint
	for _, k := range kindsToFetch {
		if seen[k] {
			continue
		}
		seen[k] = true
		art, err := s.Proto.GetArtifact(ctx, "DEF-"+k)
		if err != nil {
			continue
		}
		hint := KindHint{Kind: k}
		for _, sec := range art.Sections {
			switch sec.Name {
			case "when_to_create":
				hint.WhenToCreate = sec.Text
			case "agent_note":
				hint.AgentNote = sec.Text
			}
		}
		if hint.WhenToCreate != "" || hint.AgentNote != "" {
			hints = append(hints, hint)
		}
	}
	return hints
}

func (s *Service) resolveRules(ctx context.Context, signalLabels []string) []RuleEntry {
	signals := make([]string, 0, len(signalLabels)+1)
	signals = append(signals, "always")
	for _, l := range signalLabels {
		if !strings.HasPrefix(l, "world:") && !strings.HasPrefix(l, "source:") {
			signals = append(signals, l)
		}
	}

	expanded := parchment.ExpandLabels(signals)

	// no scope filter — rules are global, not tied to homeScopes
	arts, _ := s.Proto.Store().List(ctx, parchment.Filter{
		Labels:   []string{"rule"},
		LabelsOr: expanded,
	})

	sort.Slice(arts, func(i, j int) bool {
		pi, _ := arts[i].Extra["priority"].(float64)
		pj, _ := arts[j].Extra["priority"].(float64)
		return pi > pj
	})

	var out []RuleEntry
	used := 0
	for _, art := range arts {
		body := ""
		for _, sec := range art.Sections {
			if sec.Name == "content" {
				body = sec.Text
				break
			}
		}
		tokens := len(body) / 4
		if tokens == 0 && body != "" {
			tokens = 1
		}
		if ruleTokenBudget > 0 && used+tokens > ruleTokenBudget {
			break
		}
		used += tokens
		priority, _ := art.Extra["priority"].(float64)
		out = append(out, RuleEntry{
			ID:       art.ID,
			Title:    art.Title,
			Labels:   art.Labels,
			Priority: int(priority),
			Body:     body,
		})
	}
	return out
}

// userDefinedLabels strips system-managed label prefixes so knowledge lookups
// only match on user-defined labels, not structural metadata.
func userDefinedLabels(labels []string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if strings.HasPrefix(l, "kind:") ||
			strings.HasPrefix(l, "status:") ||
			strings.HasPrefix(l, "scope:") ||
			strings.HasPrefix(l, "priority:") ||
			strings.HasPrefix(l, "sprint:") ||
			strings.HasPrefix(l, "compliance:") {
			continue
		}
		out = append(out, l)
	}
	return out
}
