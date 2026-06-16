package service_test

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func setupGraphStore(t *testing.T) parchment.Store {
	t.Helper()
	store := parchment.NewMemoryStore()
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "a", Title: "Alpha", Labels: []string{"kind:knowledge.note", "project:test"}},
		{ID: "b", Title: "Beta", Labels: []string{"kind:knowledge.note", "project:test"}},
		{ID: "c", Title: "Charlie", Labels: []string{"kind:knowledge.note", "project:test"}},
		{ID: "d", Title: "Delta", Labels: []string{"kind:effort.task", "project:test"}},
	} {
		if err := store.Put(ctx, a); err != nil {
			t.Fatalf("put %s: %v", a.ID, err)
		}
	}

	// Edges: a→b, a→c, d→b  (b has fan-in=2, a has fan-out=2)
	for _, e := range []parchment.Edge{
		{From: "a", To: "b", Relation: "cites"},
		{From: "a", To: "c", Relation: "cites"},
		{From: "d", To: "b", Relation: "depends_on"},
	} {
		if err := store.AddEdge(ctx, e); err != nil {
			t.Fatalf("edge %s→%s: %v", e.From, e.To, err)
		}
	}
	return store
}

func TestFanIn(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	tests := []struct {
		id   string
		want int
	}{
		{"b", 2}, // a→b, d→b
		{"c", 1}, // a→c
		{"a", 0},
		{"d", 0},
	}
	for _, tt := range tests {
		got, err := service.FanIn(ctx, store, tt.id)
		if err != nil {
			t.Fatalf("FanIn(%s): %v", tt.id, err)
		}
		if got != tt.want {
			t.Errorf("FanIn(%s) = %d, want %d", tt.id, got, tt.want)
		}
	}
}

func TestFanOut(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	tests := []struct {
		id   string
		want int
	}{
		{"a", 2}, // a→b, a→c
		{"d", 1}, // d→b
		{"b", 0},
		{"c", 0},
	}
	for _, tt := range tests {
		got, err := service.FanOut(ctx, store, tt.id)
		if err != nil {
			t.Fatalf("FanOut(%s): %v", tt.id, err)
		}
		if got != tt.want {
			t.Errorf("FanOut(%s) = %d, want %d", tt.id, got, tt.want)
		}
	}
}

func TestCommonNeighbors_Incoming(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	// b and c both have incoming from a. b also has incoming from d.
	common, err := service.CommonNeighbors(ctx, store, "b", "c", parchment.Incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(common) != 1 || common[0] != "a" {
		t.Errorf("CommonNeighbors(b, c, Incoming) = %v, want [a]", common)
	}
}

func TestCommonNeighbors_NoOverlap(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	common, err := service.CommonNeighbors(ctx, store, "a", "d", parchment.Incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(common) != 0 {
		t.Errorf("CommonNeighbors(a, d, Incoming) = %v, want []", common)
	}
}

func TestShortestPath_Direct(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	path, err := service.ShortestPath(ctx, store, "a", "b", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 1 {
		t.Fatalf("ShortestPath(a→b) length = %d, want 1", len(path))
	}
	if path[0].From != "a" || path[0].To != "b" {
		t.Errorf("path[0] = %s→%s, want a→b", path[0].From, path[0].To)
	}
}

func TestShortestPath_TwoHops(t *testing.T) {
	t.Parallel()
	store := parchment.NewMemoryStore()
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "x", Title: "X"}, {ID: "y", Title: "Y"}, {ID: "z", Title: "Z"},
	} {
		_ = store.Put(ctx, a)
	}
	_ = store.AddEdge(ctx, parchment.Edge{From: "x", To: "y", Relation: "r"})
	_ = store.AddEdge(ctx, parchment.Edge{From: "y", To: "z", Relation: "r"})

	path, err := service.ShortestPath(ctx, store, "x", "z", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 2 {
		t.Fatalf("ShortestPath(x→z) length = %d, want 2", len(path))
	}
}

func TestShortestPath_NoPath(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	path, err := service.ShortestPath(ctx, store, "c", "d", 5)
	if err != nil {
		t.Fatal(err)
	}
	if path != nil {
		t.Errorf("ShortestPath(c→d) = %v, want nil", path)
	}
}

func TestShortestPath_MaxDepth(t *testing.T) {
	t.Parallel()
	store := parchment.NewMemoryStore()
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "x", Title: "X"}, {ID: "y", Title: "Y"}, {ID: "z", Title: "Z"},
	} {
		_ = store.Put(ctx, a)
	}
	_ = store.AddEdge(ctx, parchment.Edge{From: "x", To: "y", Relation: "r"})
	_ = store.AddEdge(ctx, parchment.Edge{From: "y", To: "z", Relation: "r"})

	path, _ := service.ShortestPath(ctx, store, "x", "z", 1)
	if path != nil {
		t.Errorf("ShortestPath(x→z, maxDepth=1) should be nil, got %v", path)
	}
}

func TestShortestPath_CycleSafe(t *testing.T) {
	t.Parallel()
	store := parchment.NewMemoryStore()
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "x", Title: "X"}, {ID: "y", Title: "Y"},
	} {
		_ = store.Put(ctx, a)
	}
	_ = store.AddEdge(ctx, parchment.Edge{From: "x", To: "y", Relation: "r"})
	_ = store.AddEdge(ctx, parchment.Edge{From: "y", To: "x", Relation: "r"})

	path, _ := service.ShortestPath(ctx, store, "x", "nonexistent", 10)
	if path != nil {
		t.Errorf("expected nil for unreachable target in cyclic graph")
	}
}

func TestComputePageRank_Basic(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	results, err := service.ComputePageRank(ctx, store, []string{"project:test"}, 20, 0.85)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("PageRank returned %d results, want 4", len(results))
	}
	// b has 2 inbound (from a and d), should rank highest
	if results[0].ID != "b" {
		t.Errorf("top PageRank = %s, want b (highest fan-in)", results[0].ID)
	}
}

func TestComputePageRank_SingleNode(t *testing.T) {
	t.Parallel()
	store := parchment.NewMemoryStore()
	ctx := context.Background()
	_ = store.Put(ctx, &parchment.Artifact{ID: "solo", Title: "Solo", Labels: []string{"project:test"}})

	results, err := service.ComputePageRank(ctx, store, []string{"project:test"}, 20, 0.85)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0].Score < 0.99 {
		t.Errorf("single node score = %f, want ~1.0", results[0].Score)
	}
}

func TestComputePageRank_Empty(t *testing.T) {
	t.Parallel()
	store := parchment.NewMemoryStore()
	ctx := context.Background()

	results, err := service.ComputePageRank(ctx, store, []string{"project:none"}, 20, 0.85)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil for empty scope, got %v", results)
	}
}

func TestFindCoCitations(t *testing.T) {
	t.Parallel()
	store := setupGraphStore(t)
	ctx := context.Background()

	// a cites b and c. d depends_on b.
	// Co-citations of b (incoming): a and d both point to b.
	// Looking for artifacts that share incoming neighbors with b:
	// - c also has incoming from a → overlap(b,c)=1 (shared source: a)
	results, err := service.FindCoCitations(ctx, store, "b", parchment.Incoming, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected co-citation results for b")
	}
	found := false
	for _, r := range results {
		if r.ID == "c" {
			found = true
			if r.Overlap != 1 {
				t.Errorf("c overlap = %d, want 1", r.Overlap)
			}
		}
	}
	if !found {
		t.Errorf("expected c in co-citation results for b, got %v", results)
	}
}
