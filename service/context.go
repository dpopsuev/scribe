package service

import (
	"context"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

const ruleTokenBudget = 4000

// CodePointers carries file and symbol references from a task's ComponentMap.
// The agent uses these to load code via its file reading tools — Parchment
// stores the routing information, not the content.
type CodePointers struct {
	Files   []string `json:"files,omitempty"`
	Symbols []string `json:"symbols,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

// RuleEntry is a resolved rule artifact ready for injection into agent context.
type RuleEntry struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Labels   []string `json:"labels,omitempty"`
	Priority int      `json:"priority"`
	Body     string   `json:"body"`
}

// ContextPacket is the four-worlds context assembled for a single task.
// Returned by ContextRead and consumed at agent session start.
type ContextPacket struct {
	Task  *parchment.Artifact   `json:"task"`
	Know  []*parchment.Artifact `json:"know,omitempty"`
	Code  CodePointers          `json:"code"`
	Rules []RuleEntry           `json:"rules,omitempty"`
}

// ContextRead assembles the working context for a task in one call.
//
// Iteration 2 — label expansion via label_parents taxonomy:
//
//	task  — the task artifact itself
//	know  — notes and concepts in the same scope matching task labels
//	code  — file and symbol pointers from task.Components (agent loads them)
//	rules — kind=rule artifacts matching expanded task labels, sorted by priority
func (s *Service) ContextRead(ctx context.Context, taskID string) (*ContextPacket, error) {
	task, err := s.Proto.GetArtifact(ctx, taskID)
	if err != nil {
		return nil, err
	}

	var know []*parchment.Artifact
	if task.Scope != "" && len(task.Labels) > 0 {
		all, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{
			Scope:  task.Scope,
			Labels: task.Labels,
		})
		for _, art := range all {
			if art.Kind == parchment.KindNote || art.Kind == parchment.KindConcept {
				know = append(know, art)
			}
		}
	}

	code := CodePointers{
		Files:   task.Components.Files,
		Symbols: task.Components.Symbols,
	}
	if len(code.Files) > 0 || len(code.Symbols) > 0 {
		code.Hint = "load via lector.read or file reader at these paths"
	}

	rules := s.resolveRules(ctx, task.Labels)

	return &ContextPacket{
		Task:  task,
		Know:  know,
		Code:  code,
		Rules: rules,
	}, nil
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
		Kind:     "rule",
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
