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

	now := time.Now()
	twoDaysAgo := now.Add(-48 * time.Hour)

	// Parent updated 2 days ago, child updated just now → >24h gap triggers staleness
	_ = store.Put(ctx, &parchment.Artifact{
		ID: "parent1", Title: "campaign",
		Labels:    []string{"kind:effort.campaign"},
		UpdatedAt: twoDaysAgo,
	})
	_ = store.Put(ctx, &parchment.Artifact{
		ID: "child1", Title: "goal under campaign",
		Labels:    []string{"kind:effort.goal"},
		UpdatedAt: now,
	})
	store.AddEdge(ctx, parchment.Edge{From: "parent1", Relation: "parent_of", To: "child1"}) //nolint:errcheck // test setup

	parentArt, _ := store.Get(ctx, "parent1")
	stale := service.NeighborStaleness(ctx, store, parentArt)
	if len(stale) == 0 {
		t.Fatal("expected child to appear as stale neighbor after >24h gap")
	}
	if stale[0].ID != "child1" {
		t.Errorf("stale neighbor ID = %q, want %q", stale[0].ID, "child1")
	}
}

func TestNeighborStaleness_IgnoresCoEditingChurn(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	store := svc.Proto.Store()

	now := time.Now()
	// Both updated within an hour of each other — below 24h threshold
	_ = store.Put(ctx, &parchment.Artifact{
		ID: "churnA", Title: "artifact A",
		Labels:    []string{"kind:effort.goal"},
		UpdatedAt: now.Add(-30 * time.Minute),
	})
	_ = store.Put(ctx, &parchment.Artifact{
		ID: "churnB", Title: "artifact B",
		Labels:    []string{"kind:effort.task"},
		UpdatedAt: now,
	})
	store.AddEdge(ctx, parchment.Edge{From: "churnA", Relation: "parent_of", To: "churnB"}) //nolint:errcheck // test setup

	aArt, _ := store.Get(ctx, "churnA")
	stale := service.NeighborStaleness(ctx, store, aArt)
	if len(stale) > 0 {
		t.Errorf("co-editing churn (<24h gap) should not be flagged as stale, got %d findings", len(stale))
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
