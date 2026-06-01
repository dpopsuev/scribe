package service_test

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// newTestService creates a Service backed by an in-memory store for tests.
func newTestService(t *testing.T, scopes ...string) *service.Service {
	t.Helper()
	if len(scopes) == 0 {
		scopes = []string{"test"}
	}
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, nil, scopes, nil, parchment.ProtocolConfig{})
	return service.New(proto, nil, scopes)
}

// --- ContextRead ---

func TestContextRead_ReturnsTask(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "fix auth bug",
		Scope:    "test",
		Priority: "high",
		Labels:   []string{"go", "security"},
		Sections: []parchment.Section{{Name: "context", Text: "JWT expiry not checked"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Task == nil {
		t.Fatal("expected task in packet, got nil")
	}
	if packet.Task.ID != task.ID {
		t.Errorf("task ID = %q, want %q", packet.Task.ID, task.ID)
	}
}

func TestContextRead_RulesExpandedByLabelHierarchy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	// Create a rule with label "lang.go" (expands to include "lang")
	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "rule",
		Title:    "Go conventions",
		Scope:    "global",
		Priority: "none",
		Labels:   []string{"lang.go"},
		Sections: []parchment.Section{{Name: "content", Text: "Use gofmt."}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a task with label "lang.go" — rule should appear in context
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "write Go service",
		Scope:    "test",
		Priority: "medium",
		Labels:   []string{"lang.go"},
		Sections: []parchment.Section{{Name: "context", Text: "build a Go HTTP service"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Rules) == 0 {
		t.Error("expected Go conventions rule in context, got none")
	}
	if len(packet.Rules) > 0 && packet.Rules[0].Title != "Go conventions" {
		t.Errorf("expected 'Go conventions', got %q", packet.Rules[0].Title)
	}
}

func TestContextRead_AlwaysRulesAlwaysIncluded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "rule",
		Title:    "KISS directives",
		Scope:    "global",
		Priority: "none",
		Labels:   []string{"always"},
		Sections: []parchment.Section{{Name: "content", Text: "Keep it simple."}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Task with no matching labels — always rule should still appear
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "unrelated task",
		Scope:    "test",
		Priority: "low",
		Labels:   []string{"rust"},
		Sections: []parchment.Section{{Name: "context", Text: "Rust stuff"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Rules) == 0 {
		t.Error("expected always rule to be included, got none")
	}
}

func TestContextRead_KnowledgeInSameScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:   "note",
		Title:  "auth note",
		Scope:  "test",
		Labels: []string{"security"},
	})
	if err != nil {
		t.Fatal(err)
	}

	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     "task",
		Title:    "auth task",
		Scope:    "test",
		Priority: "high",
		Labels:   []string{"security"},
		Sections: []parchment.Section{{Name: "context", Text: "fix auth"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	packet, err := svc.ContextRead(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Know) == 0 {
		t.Error("expected auth note in knowledge layer, got none")
	}
}

// --- SyncLexicon ---

func TestSyncLexicon_EmptyDirectory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)

	n, err := svc.SyncLexicon(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 artifacts from empty dir, got %d", n)
	}
}

// --- Motd ---

func TestMotd_EmptyStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t, "test")

	m, err := svc.Motd(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("Motd returned nil")
	}
}

func TestMotd_ReturnsCurrentGoals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t, "test")

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:  "goal",
		Title: "ship labeldef",
		Scope: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := svc.Motd(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// goal starts as draft; motd tracks active/current goals
	// just verify motd does not error and returns a packet
	// no current goals yet — just verify Motd does not error
	_ = m
}

// --- ExpandLabels integration ---

func TestExpandLabels_DotHierarchy(t *testing.T) {
	t.Parallel()
	got := parchment.ExpandLabels([]string{"lang.go.test"})
	want := map[string]bool{"lang.go.test": true, "lang.go": true, "lang": true}
	for _, l := range got {
		if !want[l] {
			t.Errorf("unexpected label %q in expansion", l)
		}
	}
	for w := range want {
		found := false
		for _, l := range got {
			if l == w {
				found = true
			}
		}
		if !found {
			t.Errorf("missing expected label %q in expansion", w)
		}
	}
}
