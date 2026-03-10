package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/store"
)

func openSQLite(t *testing.T) *store.SQLiteStore {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLitePutGet(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	art := &model.Artifact{
		ID: "TASK-2026-001", Kind: "task", Scope: "mos",
		Status: "active", Title: "Test Task", Goal: "Test the store",
	}
	if err := s.Put(ctx, art); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "TASK-2026-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Test Task" {
		t.Errorf("title = %q, want %q", got.Title, "Test Task")
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at should be set")
	}
}

func TestSQLiteGetNotFound(t *testing.T) {
	s := openSQLite(t)
	_, err := s.Get(context.Background(), "NOPE")
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
}

func TestSQLiteList(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	for _, a := range []*model.Artifact{
		{ID: "TASK-2026-001", Kind: "task", Scope: "mos", Status: "active", Title: "A"},
		{ID: "TASK-2026-002", Kind: "task", Scope: "locus", Status: "draft", Title: "B"},
		{ID: "SPEC-2026-001", Kind: "specification", Scope: "mos", Status: "active", Title: "C"},
	} {
		if err := s.Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	got, _ := s.List(ctx, model.Filter{Kind: "task"})
	if len(got) != 2 {
		t.Errorf("by kind: got %d, want 2", len(got))
	}
	got, _ = s.List(ctx, model.Filter{Scope: "mos"})
	if len(got) != 2 {
		t.Errorf("by scope: got %d, want 2", len(got))
	}
	got, _ = s.List(ctx, model.Filter{Status: "draft"})
	if len(got) != 1 {
		t.Errorf("by status: got %d, want 1", len(got))
	}
	got, _ = s.List(ctx, model.Filter{})
	if len(got) != 3 {
		t.Errorf("unfiltered: got %d, want 3", len(got))
	}
}

func TestSQLiteListSprint(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "draft", Title: "A", Sprint: "SPR-1"})
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-002", Kind: "task", Status: "draft", Title: "B", Sprint: "SPR-2"})

	got, _ := s.List(ctx, model.Filter{Sprint: "SPR-1"})
	if len(got) != 1 || got[0].ID != "TASK-2026-001" {
		t.Errorf("sprint filter: got %d, want 1 (TASK-2026-001)", len(got))
	}
}

func TestSQLiteParentEdges(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "Parent"})
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-002", Kind: "task", Status: "draft", Title: "Child", Parent: "TASK-2026-001"})

	edges, _ := s.Neighbors(ctx, "TASK-2026-001", model.RelParentOf, store.Outgoing)
	if len(edges) != 1 || edges[0].To != "TASK-2026-002" {
		t.Errorf("parent outgoing edges = %v, want 1 edge to TASK-2026-002", edges)
	}

	edges, _ = s.Neighbors(ctx, "TASK-2026-002", model.RelParentOf, store.Incoming)
	if len(edges) != 1 || edges[0].From != "TASK-2026-001" {
		t.Errorf("child incoming edges = %v, want 1 edge from TASK-2026-001", edges)
	}

	children, _ := s.Children(ctx, "TASK-2026-001")
	if len(children) != 1 || children[0].ID != "TASK-2026-002" {
		t.Errorf("children = %v, want [TASK-2026-002]", children)
	}
}

func TestSQLiteDependsOnEdges(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "Dep target"})
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-002", Kind: "task", Status: "draft", Title: "Depends", DependsOn: []string{"TASK-2026-001"}})

	edges, _ := s.Neighbors(ctx, "TASK-2026-002", model.RelDependsOn, store.Outgoing)
	if len(edges) != 1 || edges[0].To != "TASK-2026-001" {
		t.Errorf("depends_on edges = %v, want 1 edge to TASK-2026-001", edges)
	}
}

func TestSQLiteEdgeReconcileOnUpdate(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "P1"})
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-002", Kind: "task", Status: "active", Title: "P2"})
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-003", Kind: "task", Status: "draft", Title: "Child", Parent: "TASK-2026-001"})

	art, _ := s.Get(ctx, "TASK-2026-003")
	art.Parent = "TASK-2026-002"
	s.Put(ctx, art)

	edges, _ := s.Neighbors(ctx, "TASK-2026-001", model.RelParentOf, store.Outgoing)
	if len(edges) != 0 {
		t.Errorf("old parent should have 0 children, got %d", len(edges))
	}
	edges, _ = s.Neighbors(ctx, "TASK-2026-002", model.RelParentOf, store.Outgoing)
	if len(edges) != 1 {
		t.Errorf("new parent should have 1 child, got %d", len(edges))
	}
}

func TestSQLiteDelete(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "Parent"})
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-002", Kind: "task", Status: "draft", Title: "Child", Parent: "TASK-2026-001"})

	if err := s.Delete(ctx, "TASK-2026-002"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "TASK-2026-002"); err == nil {
		t.Error("expected error after delete")
	}
	edges, _ := s.Neighbors(ctx, "TASK-2026-001", model.RelParentOf, store.Outgoing)
	if len(edges) != 0 {
		t.Errorf("parent should have 0 children after child delete, got %d", len(edges))
	}
}

func TestSQLiteNextID(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	id1, err := s.NextID(ctx, "TASK")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.NextID(ctx, "TASK")
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Errorf("sequential IDs should differ: %s vs %s", id1, id2)
	}
	if id1[len(id1)-3:] != "001" {
		t.Errorf("first ID should end in 001, got %s", id1)
	}
	if id2[len(id2)-3:] != "002" {
		t.Errorf("second ID should end in 002, got %s", id2)
	}
}

func TestSQLiteWalk(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "ROOT", Kind: "task", Status: "active", Title: "Root"})
	s.Put(ctx, &model.Artifact{ID: "A", Kind: "task", Status: "active", Title: "A", Parent: "ROOT"})
	s.Put(ctx, &model.Artifact{ID: "B", Kind: "task", Status: "active", Title: "B", Parent: "A"})
	s.Put(ctx, &model.Artifact{ID: "C", Kind: "task", Status: "active", Title: "C", Parent: "ROOT"})

	var visited []string
	s.Walk(ctx, "ROOT", model.RelParentOf, store.Outgoing, 0, func(depth int, e model.Edge) bool {
		visited = append(visited, e.To)
		return true
	})
	if len(visited) != 3 {
		t.Errorf("walk should visit 3 nodes, got %d: %v", len(visited), visited)
	}
}

func TestSQLiteLinkEdges(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "SPEC-2026-001", Kind: "specification", Status: "active", Title: "Spec"})
	s.Put(ctx, &model.Artifact{
		ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "Task",
		Links: map[string][]string{model.RelJustifies: {"SPEC-2026-001"}},
	})

	edges, _ := s.Neighbors(ctx, "TASK-2026-001", model.RelJustifies, store.Outgoing)
	if len(edges) != 1 || edges[0].To != "SPEC-2026-001" {
		t.Errorf("justifies edge = %v, want 1 edge to SPEC-2026-001", edges)
	}
}

func TestSQLiteSeedSequence(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	if err := s.SeedSequence(ctx, "TASK", 100, false); err != nil {
		t.Fatal(err)
	}
	id, _ := s.NextID(ctx, "TASK")
	if id[len(id)-3:] != "100" {
		t.Errorf("after seed(100), first ID should end in 100, got %s", id)
	}

	// Non-force seed should not lower the counter
	if err := s.SeedSequence(ctx, "TASK", 50, false); err != nil {
		t.Fatal(err)
	}
	id, _ = s.NextID(ctx, "TASK")
	if id[len(id)-3:] != "101" {
		t.Errorf("non-force seed(50) should not lower counter, got %s", id)
	}

	// Force seed should lower it
	if err := s.SeedSequence(ctx, "TASK", 10, true); err != nil {
		t.Fatal(err)
	}
	id, _ = s.NextID(ctx, "TASK")
	if id[len(id)-3:] != "010" {
		t.Errorf("force seed(10) should set counter to 10, got %s", id)
	}
}

func TestSQLiteConcurrentAccess(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")

	s1, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s1.Close()

	s2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal("second open should succeed with SQLite WAL:", err)
	}
	defer s2.Close()

	ctx := context.Background()

	if err := s1.Put(ctx, &model.Artifact{
		ID: "TASK-2026-001", Kind: "task", Status: "draft", Title: "Written by s1",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s2.Get(ctx, "TASK-2026-001")
	if err != nil {
		t.Fatal("s2 should read s1's write:", err)
	}
	if got.Title != "Written by s1" {
		t.Errorf("title = %q, want %q", got.Title, "Written by s1")
	}

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			art := &model.Artifact{
				ID: fmt.Sprintf("TASK-2026-%03d", n+100), Kind: "task",
				Status: "draft", Title: fmt.Sprintf("Concurrent %d", n),
			}
			if err := s1.Put(ctx, art); err != nil {
				errs <- fmt.Errorf("s1 put %d: %w", n, err)
			}
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, err := s2.List(ctx, model.Filter{}); err != nil {
				errs <- fmt.Errorf("s2 list %d: %w", n, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	all, _ := s1.List(ctx, model.Filter{})
	if len(all) != 11 {
		t.Errorf("expected 11 artifacts (1 + 10 concurrent), got %d", len(all))
	}
}

func TestSQLiteScopeFilter(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	for _, a := range []*model.Artifact{
		{ID: "TASK-2026-001", Kind: "task", Scope: "origami", Status: "draft", Title: "Origami task"},
		{ID: "TASK-2026-002", Kind: "task", Scope: "mos", Status: "draft", Title: "Mos task"},
		{ID: "TASK-2026-003", Kind: "task", Scope: "scribe", Status: "draft", Title: "Scribe task"},
		{ID: "TASK-2026-004", Kind: "task", Scope: "asterisk", Status: "draft", Title: "Asterisk task"},
	} {
		if err := s.Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("single scope filter", func(t *testing.T) {
		arts, err := s.List(ctx, model.Filter{Scope: "mos"})
		if err != nil {
			t.Fatal(err)
		}
		if len(arts) != 1 || arts[0].ID != "TASK-2026-002" {
			t.Errorf("expected 1 mos artifact, got %d", len(arts))
		}
	})

	t.Run("multi scope filter (Scopes)", func(t *testing.T) {
		arts, err := s.List(ctx, model.Filter{Scopes: []string{"origami", "mos", "scribe"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(arts) != 3 {
			t.Errorf("expected 3 artifacts for origami+mos+scribe, got %d", len(arts))
		}
		for _, a := range arts {
			if a.Scope == "asterisk" {
				t.Error("asterisk artifact leaked through scope filter")
			}
		}
	})

	t.Run("Scopes takes precedence over Scope", func(t *testing.T) {
		arts, err := s.List(ctx, model.Filter{
			Scope:  "asterisk",
			Scopes: []string{"origami"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(arts) != 1 || arts[0].Scope != "origami" {
			t.Errorf("Scopes should take precedence over Scope; got %d results", len(arts))
		}
	})

	t.Run("empty scopes returns all", func(t *testing.T) {
		arts, err := s.List(ctx, model.Filter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(arts) != 4 {
			t.Errorf("expected 4 artifacts with no scope filter, got %d", len(arts))
		}
	})
}
