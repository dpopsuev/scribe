package parchment

import (
	"context"
	"testing"
)

// Compile-time interface verification.
var _ Store = (*SQLiteStore)(nil)

// storeContract runs the full Store contract test suite against any Store implementation.
// This enables testing SQLiteStore now and MemoryStore (future) with the same tests.
func storeContract(t *testing.T, newStore func(t *testing.T) Store) { //nolint:gocyclo // contract suite is intentionally comprehensive
	t.Helper()

	t.Run("PutGet", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		art := &Artifact{UID: "u1", ID: "TST-TSK-1", Kind: "task", Status: "draft", Title: "test"}
		if err := s.Put(ctx, art); err != nil {
			t.Fatal(err)
		}

		got, err := s.Get(ctx, "TST-TSK-1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "test" {
			t.Errorf("title = %q, want %q", got.Title, "test")
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		_, err := s.Get(ctx, "NONEXISTENT")
		if err == nil {
			t.Fatal("expected error for missing artifact")
		}
	})

	t.Run("ListFilter", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		s.Put(ctx, &Artifact{UID: "u1", ID: "T-1", Kind: "task", Scope: "a", Status: "draft", Title: "one"})    //nolint:errcheck // test seeding
		s.Put(ctx, &Artifact{UID: "u2", ID: "T-2", Kind: "spec", Scope: "a", Status: "draft", Title: "two"})    //nolint:errcheck // test seeding
		s.Put(ctx, &Artifact{UID: "u3", ID: "T-3", Kind: "task", Scope: "b", Status: "active", Title: "three"}) //nolint:errcheck // test seeding

		arts, err := s.List(ctx, Filter{Kind: "task"})
		if err != nil {
			t.Fatal(err)
		}
		if len(arts) != 2 {
			t.Errorf("expected 2 tasks, got %d", len(arts))
		}

		arts, err = s.List(ctx, Filter{Scope: "a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(arts) != 2 {
			t.Errorf("expected 2 in scope a, got %d", len(arts))
		}
	})

	t.Run("AddEdgeNeighbors", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		s.Put(ctx, &Artifact{UID: "u1", ID: "A", Kind: "goal", Status: "draft", Title: "a"}) //nolint:errcheck // test seeding
		s.Put(ctx, &Artifact{UID: "u2", ID: "B", Kind: "task", Status: "draft", Title: "b"}) //nolint:errcheck // test seeding

		if err := s.AddEdge(ctx, Edge{From: "A", To: "B", Relation: RelParentOf}); err != nil {
			t.Fatal(err)
		}

		edges, err := s.Neighbors(ctx, "A", RelParentOf, Outgoing)
		if err != nil {
			t.Fatal(err)
		}
		if len(edges) != 1 || edges[0].To != "B" {
			t.Errorf("expected edge A→B, got %+v", edges)
		}

		edges, err = s.Neighbors(ctx, "B", RelParentOf, Incoming)
		if err != nil {
			t.Fatal(err)
		}
		if len(edges) != 1 || edges[0].From != "A" {
			t.Errorf("expected edge A→B (incoming to B), got %+v", edges)
		}
	})

	t.Run("NextScopedID_Monotonic", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		id1, err := s.NextScopedID(ctx, "TST", "TSK")
		if err != nil {
			t.Fatal(err)
		}
		id2, err := s.NextScopedID(ctx, "TST", "TSK")
		if err != nil {
			t.Fatal(err)
		}
		if id1 >= id2 {
			t.Errorf("IDs not monotonic: %s >= %s", id1, id2)
		}
	})

	t.Run("DeleteArtifact", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		s.Put(ctx, &Artifact{UID: "u1", ID: "DEL-1", Kind: "task", Status: "draft", Title: "delete me"}) //nolint:errcheck // test seeding

		if err := s.Delete(ctx, "DEL-1"); err != nil {
			t.Fatal(err)
		}
		_, err := s.Get(ctx, "DEL-1")
		if err == nil {
			t.Fatal("expected error after delete")
		}
	})

	t.Run("SearchFTS", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		ctx := context.Background()

		s.Put(ctx, &Artifact{UID: "u1", ID: "S-1", Kind: "task", Status: "draft", Title: "uniquesearchterm"}) //nolint:errcheck // test seeding

		ids, err := s.Search(ctx, "uniquesearchterm")
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) == 0 {
			t.Error("expected search results")
		}
	})
}

// TestSQLiteStore_Contract runs the full Store contract against SQLiteStore.
func TestSQLiteStore_Contract(t *testing.T) {
	storeContract(t, func(t *testing.T) Store {
		t.Helper()
		path := t.TempDir() + "/contract.db"
		s, err := OpenSQLite(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	})
}
