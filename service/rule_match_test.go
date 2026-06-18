package service_test

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func setupRuleService(t *testing.T) *service.Service {
	t.Helper()
	svc := newTestService(t, "test")
	ctx := context.Background()

	type ruleSpec struct {
		title       string
		labels      []string
		body        string
		globs       []any
		priority    float64
		alwaysApply bool
	}
	for _, rs := range []ruleSpec{
		{
			title:    "Effective Go",
			labels:   []string{"kind:support.rule", "go", "best-practices"},
			body:     "Use named types. Never pass raw string.",
			globs:    []any{"*.go"},
			priority: 25,
		},
		{
			title:       "OWASP Top 10",
			labels:      []string{"kind:support.rule", "security", "owasp"},
			body:        "Validate all input. Sanitize output.",
			priority:    50,
			alwaysApply: true,
		},
		{
			title:    "TDD Practices",
			labels:   []string{"kind:support.rule", "testing", "tdd"},
			body:     "Red green refactor. Test behavior not implementation.",
			globs:    []any{"*_test.go", "*_test.ts"},
			priority: 30,
		},
	} {
		extra := map[string]any{"priority": rs.priority, "always_apply": rs.alwaysApply}
		if rs.globs != nil {
			extra["globs"] = rs.globs
		}
		art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Title:    rs.title,
			Labels:   rs.labels,
			Sections: []parchment.Section{{Name: "body", Text: rs.body}},
			Extra:    extra,
		})
		if err != nil {
			t.Fatalf("create %s: %v", rs.title, err)
		}
		_ = art
	}
	return svc
}

func TestMatchRules_ByGlob(t *testing.T) {
	svc := setupRuleService(t)
	results, err := svc.MatchRules(context.Background(), "service/handler.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.Art.Title == "Effective Go" {
			found = true
		}
	}
	if !found {
		t.Error("expected Effective Go to match *.go file")
	}
}

func TestMatchRules_AlwaysApply(t *testing.T) {
	svc := setupRuleService(t)
	results, err := svc.MatchRules(context.Background(), "readme.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.Art.Title == "OWASP Top 10" {
			found = true
			if r.Score < 10 {
				t.Errorf("always_apply rule should score >= 10, got %.1f", r.Score)
			}
		}
	}
	if !found {
		t.Error("expected always_apply OWASP Top 10 to match any file")
	}
}

func TestMatchRules_ByLabel(t *testing.T) {
	svc := setupRuleService(t)
	results, err := svc.MatchRules(context.Background(), "", []string{"testing"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.Art.Title == "TDD Practices" {
			found = true
		}
	}
	if !found {
		t.Error("expected TDD Practices to match label 'testing'")
	}
}

func TestMatchRules_TestFile(t *testing.T) {
	svc := setupRuleService(t)
	results, err := svc.MatchRules(context.Background(), "auth_test.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Should match: Effective Go (*.go), TDD Practices (*_test.go), OWASP Top 10 (always_apply)
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.Art.Title] = true
	}
	for _, want := range []string{"Effective Go", "TDD Practices", "OWASP Top 10"} {
		if !ids[want] {
			t.Errorf("expected %s to match test file, got: %v", want, ids)
		}
	}
}

func TestMatchRules_NoMatch(t *testing.T) {
	svc := setupRuleService(t)
	results, err := svc.MatchRules(context.Background(), "image.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Only always_apply rules should match
	for _, r := range results {
		alwaysApply, _ := r.Art.Extra["always_apply"].(bool)
		if !alwaysApply {
			t.Errorf("non-always_apply rule %s matched image.png", r.Art.ID)
		}
	}
}
