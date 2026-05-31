package service

import (
	"context"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// CodePointers carries file and symbol references from a task's ComponentMap.
// The agent uses these to load code via its file reading tools — Parchment
// stores the routing information, not the content.
type CodePointers struct {
	Files   []string `json:"files,omitempty"`
	Symbols []string `json:"symbols,omitempty"`
	Hint    string   `json:"hint,omitempty"`
}

// ContextPacket is the four-worlds context assembled for a single task.
// Returned by ContextRead and consumed at agent session start.
type ContextPacket struct {
	Task  *parchment.Artifact   `json:"task"`
	Know  []*parchment.Artifact `json:"know,omitempty"`
	Code  CodePointers          `json:"code"`
	Rules []string              `json:"rules,omitempty"`
}

// ContextRead assembles the working context for a task in one call.
//
// Iteration 1 — raw label filtering, no trait resolution:
//
//	task  — the task artifact itself
//	know  — notes and concepts in the same scope matching task labels
//	code  — file and symbol pointers from task.Components (agent loads them)
//	rules — task.Labels as a hint for lex(action=resolve, labels=[...])
func (s *Service) ContextRead(ctx context.Context, taskID string) (*ContextPacket, error) {
	task, err := s.Proto.GetArtifact(ctx, taskID)
	if err != nil {
		return nil, err
	}

	// Knowledge layer: notes and concepts in task scope matching task labels.
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

	// Code layer: file and symbol pointers from task components.
	code := CodePointers{
		Files:   task.Components.Files,
		Symbols: task.Components.Symbols,
	}
	if len(code.Files) > 0 || len(code.Symbols) > 0 {
		code.Hint = "load via lector.read or file reader at these paths"
	}

	// Constraint layer: labels as rule hint for Lex.
	var rules []string
	for _, label := range task.Labels {
		if !strings.HasPrefix(label, "world:") {
			rules = append(rules, label)
		}
	}

	return &ContextPacket{
		Task:  task,
		Know:  know,
		Code:  code,
		Rules: rules,
	}, nil
}
