package service

import (
	"context"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

const decisionLabel = "decision"
const decisionScope = "_decisions"

// RecordDecision stores a decision as kind=note, labels=[decision], title=<key>.
// The answer is stored in the goal field. scope= narrows the decision to a project.
// Calling again with the same key updates the existing decision (title match).
func (s *Service) RecordDecision(ctx context.Context, key, answer, scope string) error {
	if key == "" {
		return fmt.Errorf("key is required") //nolint:err113 // user-facing hint
	}
	sc := scope
	if sc == "" {
		sc = decisionScope
	}
	// Upsert: find existing decision with this key, update it; otherwise create.
	existing, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels:        []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixScope + sc, decisionLabel},
		TitleContains: key,
	})
	for _, art := range existing {
		if strings.EqualFold(art.Title, key) {
			_, err := s.Proto.SetField(ctx, []string{art.ID}, "goal", answer)
			return err
		}
	}
	_, err := s.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixScope + sc, decisionLabel},
		Title:  key,
		Goal:   answer,
	})
	return err
}

// CheckDecision returns the recorded answer for key, or "" if not decided.
func (s *Service) CheckDecision(ctx context.Context, key, scope string) (string, error) {
	sc := scope
	if sc == "" {
		sc = decisionScope
	}
	arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels:        []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixScope + sc, decisionLabel},
		TitleContains: key,
	})
	for _, art := range arts {
		if strings.EqualFold(art.Title, key) {
			return art.Goal, nil
		}
	}
	return "", nil
}

// ListDecisions returns all recorded decisions for a scope.
func (s *Service) ListDecisions(ctx context.Context, scope string) ([]*parchment.Artifact, error) {
	sc := scope
	if sc == "" {
		sc = decisionScope
	}
	return s.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels: []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixScope + sc, decisionLabel},
	})
}
