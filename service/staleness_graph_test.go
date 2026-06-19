package service_test

import (
	"context"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestNeighborStaleness_DetectsChangedNeighbor(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	store := svc.Proto.Store()

	parent, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "campaign",
		Labels: []string{"kind:effort.campaign"},
	})
	child, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "goal under campaign",
		Labels: []string{"kind:effort.goal"},
	})
	store.AddEdge(ctx, parchment.Edge{From: parent.ID, Relation: "parent_of", To: child.ID}) //nolint:errcheck // test setup

	// Touch parent to lock its UpdatedAt, then update child after.
	time.Sleep(15 * time.Millisecond)
	svc.Proto.SetField(ctx, []string{parent.ID}, "title", "campaign v2") //nolint:errcheck // test setup
	time.Sleep(15 * time.Millisecond)
	svc.Proto.SetField(ctx, []string{child.ID}, "title", "updated goal") //nolint:errcheck // test setup

	parentArt, _ := store.Get(ctx, parent.ID)
	stale := service.NeighborStaleness(ctx, store, parentArt)
	if len(stale) == 0 {
		t.Fatal("expected child to appear as stale neighbor after update")
	}
	if stale[0].ID != child.ID {
		t.Errorf("stale neighbor ID = %q, want %q", stale[0].ID, child.ID)
	}
}

func TestNeighborStaleness_NoFalsePositives(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	store := svc.Proto.Store()

	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "artifact A",
		Labels: []string{"kind:knowledge.note"},
	})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "artifact B",
		Labels: []string{"kind:knowledge.note"},
	})
	store.AddEdge(ctx, parchment.Edge{From: a.ID, Relation: "relates_to", To: b.ID}) //nolint:errcheck // test setup

	// Update A after B — A should not show B as stale
	time.Sleep(10 * time.Millisecond)
	svc.Proto.SetField(ctx, []string{a.ID}, "title", "updated A") //nolint:errcheck // test setup

	aArt, _ := store.Get(ctx, a.ID)
	stale := service.NeighborStaleness(ctx, store, aArt)
	for _, s := range stale {
		if s.ID == b.ID {
			t.Errorf("B should not be stale — it hasn't changed since A was updated")
		}
	}
}

func TestFormatStalenessHint_EmptyWhenNoStale(t *testing.T) {
	t.Parallel()
	hint := service.FormatStalenessHint(nil)
	if hint != "" {
		t.Errorf("expected empty hint, got %q", hint)
	}
}

func TestFormatStalenessHint_RendersNeighbors(t *testing.T) {
	t.Parallel()
	stale := []service.StaleNeighbor{
		{ID: "abc-123", Title: "changed task", Relation: "parent_of", UpdatedAt: time.Now()},
	}
	hint := service.FormatStalenessHint(stale)
	if hint == "" {
		t.Fatal("expected non-empty hint")
	}
	if !containsAll(hint, "Stale references", "abc-123", "changed task", "parent_of") {
		t.Errorf("hint missing expected content: %s", hint)
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
