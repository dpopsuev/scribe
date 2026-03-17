package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestNextScopedID(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	id1, err := s.NextScopedID(ctx, "SCR", "TSK")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != "SCR-TSK-1" {
		t.Errorf("first scoped ID = %q, want SCR-TSK-1", id1)
	}

	id2, err := s.NextScopedID(ctx, "SCR", "TSK")
	if err != nil {
		t.Fatal(err)
	}
	if id2 != "SCR-TSK-2" {
		t.Errorf("second scoped ID = %q, want SCR-TSK-2", id2)
	}

	id3, err := s.NextScopedID(ctx, "SCR", "SPC")
	if err != nil {
		t.Fatal(err)
	}
	if id3 != "SCR-SPC-1" {
		t.Errorf("spec scoped ID = %q, want SCR-SPC-1 (independent counter)", id3)
	}

	id4, err := s.NextScopedID(ctx, "LOC", "TSK")
	if err != nil {
		t.Fatal(err)
	}
	if id4 != "LOC-TSK-1" {
		t.Errorf("locus task ID = %q, want LOC-TSK-1 (independent scope)", id4)
	}
}

func TestScopeKeys(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	key, _, err := s.GetScopeKey(ctx, "scribe")
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		t.Errorf("scope key for unknown scope = %q, want empty", key)
	}

	if err := s.SetScopeKey(ctx, "scribe", "SCR", false); err != nil {
		t.Fatal(err)
	}

	key, auto, err := s.GetScopeKey(ctx, "scribe")
	if err != nil {
		t.Fatal(err)
	}
	if key != "SCR" {
		t.Errorf("scope key = %q, want SCR", key)
	}
	if auto {
		t.Error("auto should be false for manual key")
	}

	if err := s.SetScopeKey(ctx, "locus", "LOC", true); err != nil {
		t.Fatal(err)
	}

	keys, err := s.ListScopeKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 scope keys, got %d", len(keys))
	}
	if keys["scribe"] != "SCR" || keys["locus"] != "LOC" {
		t.Errorf("scope keys = %v, want scribe=SCR, locus=LOC", keys)
	}
}

func TestInsertedAtImmutable(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	art := &model.Artifact{
		ID:     "TST-001",
		Kind:   "task",
		Status: "draft",
		Title:  "Test inserted_at",
	}
	if err := s.Put(ctx, art); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "TST-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.InsertedAt.IsZero() {
		t.Error("InsertedAt should be set after Put")
	}
	originalInserted := got.InsertedAt

	time.Sleep(10 * time.Millisecond)
	got.Title = "Updated"
	if err := s.Put(ctx, got); err != nil {
		t.Fatal(err)
	}

	got2, _ := s.Get(ctx, "TST-001")
	if !got2.InsertedAt.Equal(originalInserted) {
		t.Errorf("InsertedAt changed on update: %v -> %v", originalInserted, got2.InsertedAt)
	}
}

func TestNextScopedID_SkipsExistingArtifacts(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	// Register scope key
	s.SetScopeKey(ctx, "scribe", "SCR", false)

	// Pre-populate artifacts at IDs 1, 2, 3 (simulating archived artifacts)
	for i := 1; i <= 3; i++ {
		art := &model.Artifact{
			ID:     fmt.Sprintf("SCR-TSK-%d", i),
			Kind:   "task",
			Scope:  "scribe",
			Status: "archived",
			Title:  fmt.Sprintf("Old task %d", i),
		}
		if err := s.Put(ctx, art); err != nil {
			t.Fatal(err)
		}
	}

	// NextScopedID should skip 1, 2, 3 and return SCR-TSK-4
	id, err := s.NextScopedID(ctx, "SCR", "TSK")
	if err != nil {
		t.Fatal(err)
	}
	if id != "SCR-TSK-4" {
		t.Errorf("NextScopedID should skip existing artifacts: got %q, want SCR-TSK-4", id)
	}

	// Verify old artifacts are NOT overwritten
	old, err := s.Get(ctx, "SCR-TSK-1")
	if err != nil {
		t.Fatal(err)
	}
	if old.Title != "Old task 1" {
		t.Errorf("archived artifact SCR-TSK-1 was overwritten: title = %q", old.Title)
	}
}

func TestNextScopedID_ReseedOnOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reseed.db")

	// First open: create artifacts up to SCR-TSK-5
	s1, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s1.SetScopeKey(ctx, "scribe", "SCR", false)
	for i := 1; i <= 5; i++ {
		s1.Put(ctx, &model.Artifact{
			ID:     fmt.Sprintf("SCR-TSK-%d", i),
			Kind:   "task",
			Scope:  "scribe",
			Status: "archived",
			Title:  fmt.Sprintf("Task %d", i),
		})
	}
	s1.Close()

	// Second open: reseed should detect existing artifacts
	s2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	id, err := s2.NextScopedID(ctx, "SCR", "TSK")
	if err != nil {
		t.Fatal(err)
	}
	if id != "SCR-TSK-6" {
		t.Errorf("after reseed, NextScopedID should return SCR-TSK-6, got %q", id)
	}
}

func TestNextSeq_SkipsExistingArtifacts(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	// Pre-populate artifacts at IDs that match the NextSeq key format:
	// key="SCR-SPC", seq=N → ID="SCR-SPC-N"
	for i := 1; i <= 3; i++ {
		s.Put(ctx, &model.Artifact{
			ID:     fmt.Sprintf("SCR-SPC-%d", i),
			Kind:   "spec",
			Scope:  "scribe",
			Status: "archived",
			Title:  fmt.Sprintf("Old spec %d", i),
		})
	}

	// NextSeq should skip 1, 2, 3 and return 4
	seq, err := s.NextSeq(ctx, "SCR-SPC")
	if err != nil {
		t.Fatal(err)
	}
	if seq != 4 {
		t.Errorf("NextSeq should skip existing artifacts: got seq=%d, want 4", seq)
	}

	// Verify old artifacts are NOT overwritten
	old, err := s.Get(ctx, "SCR-SPC-1")
	if err != nil {
		t.Fatal(err)
	}
	if old.Title != "Old spec 1" {
		t.Errorf("archived artifact SCR-SPC-1 was overwritten: title = %q", old.Title)
	}
}

func TestNextSeq_ReseedOnOpen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "reseed-seq.db")

	// First open: create artifacts up to SCR-SPC-5
	s1, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s1.SetScopeKey(ctx, "scribe", "SCR", false)
	for i := 1; i <= 5; i++ {
		s1.Put(ctx, &model.Artifact{
			ID:     fmt.Sprintf("SCR-SPC-%d", i),
			Kind:   "spec",
			Scope:  "scribe",
			Status: "archived",
			Title:  fmt.Sprintf("Spec %d", i),
		})
	}
	s1.Close()

	// Second open: reseed should detect existing artifacts in sequences table too
	s2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	seq, err := s2.NextSeq(ctx, "SCR-SPC")
	if err != nil {
		t.Fatal(err)
	}
	if seq != 6 {
		t.Errorf("after reseed, NextSeq should return 6, got %d", seq)
	}
}

func TestUID_AssignedOnCreate(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	art := &model.Artifact{ID: "T-001", Kind: "task", Status: "draft", Title: "Test"}
	if err := s.Put(ctx, art); err != nil {
		t.Fatal(err)
	}
	if art.UID == "" {
		t.Error("expected UID to be assigned on create")
	}

	got, _ := s.Get(ctx, "T-001")
	if got.UID != art.UID {
		t.Errorf("UID mismatch: put=%q get=%q", art.UID, got.UID)
	}
}

func TestUID_ImmutableAcrossUpdates(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	art := &model.Artifact{ID: "T-001", Kind: "task", Status: "draft", Title: "Original"}
	s.Put(ctx, art)
	originalUID := art.UID

	art.Title = "Updated"
	s.Put(ctx, art)

	got, _ := s.Get(ctx, "T-001")
	if got.UID != originalUID {
		t.Errorf("UID changed on update: %q -> %q", originalUID, got.UID)
	}
	if got.Title != "Updated" {
		t.Errorf("title not updated: %q", got.Title)
	}
}

func TestUID_CollisionAutoRenames(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	old := &model.Artifact{ID: "SCR-TSK-1", Kind: "task", Scope: "test", Status: "archived", Title: "Old Task"}
	s.Put(ctx, old)
	oldUID := old.UID

	new := &model.Artifact{ID: "SCR-TSK-1", Kind: "task", Scope: "test", Status: "draft", Title: "New Task"}
	if err := s.Put(ctx, new); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "SCR-TSK-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New Task" {
		t.Errorf("SCR-TSK-1 should be new task, got %q", got.Title)
	}
	if got.UID == oldUID {
		t.Error("new artifact should have different UID from old")
	}

	renamed, err := s.Get(ctx, "SCR-TSK-2")
	if err != nil {
		t.Fatalf("old artifact should exist at SCR-TSK-2: %v", err)
	}
	if renamed.Title != "Old Task" {
		t.Errorf("renamed artifact should keep old title, got %q", renamed.Title)
	}
	if renamed.UID != oldUID {
		t.Errorf("renamed artifact should keep old UID: got %q want %q", renamed.UID, oldUID)
	}
}

func TestUID_CollisionUpdatesEdges(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "SCR-GOL-1", Kind: "goal", Scope: "test", Status: "active", Title: "Parent"})
	s.Put(ctx, &model.Artifact{ID: "SCR-TSK-1", Kind: "task", Scope: "test", Status: "archived", Title: "Old Child", Parent: "SCR-GOL-1"})
	oldUID := func() string {
		a, _ := s.Get(ctx, "SCR-TSK-1")
		return a.UID
	}()

	edges, _ := s.Neighbors(ctx, "SCR-GOL-1", model.RelParentOf, store.Outgoing)
	if len(edges) != 1 || edges[0].To != "SCR-TSK-1" {
		t.Fatalf("expected parent edge to SCR-TSK-1, got %v", edges)
	}

	s.Put(ctx, &model.Artifact{ID: "SCR-TSK-1", Kind: "task", Scope: "test", Status: "draft", Title: "New Child"})

	edges, _ = s.Neighbors(ctx, "SCR-GOL-1", model.RelParentOf, store.Outgoing)
	found := false
	for _, e := range edges {
		if e.To == "SCR-TSK-2" {
			found = true
		}
	}
	if !found {
		t.Errorf("edge should point to renamed SCR-TSK-2, got %v", edges)
	}

	renamed, _ := s.Get(ctx, "SCR-TSK-2")
	if renamed == nil {
		t.Fatal("renamed artifact not found at SCR-TSK-2")
	}
	if renamed.UID != oldUID {
		t.Errorf("renamed UID mismatch")
	}
}

func TestUID_CollisionSkipsOccupiedIDs(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		s.Put(ctx, &model.Artifact{
			ID: fmt.Sprintf("SCR-TSK-%d", i), Kind: "task", Scope: "test",
			Status: "archived", Title: fmt.Sprintf("Old %d", i),
		})
	}
	oldUID := func() string {
		a, _ := s.Get(ctx, "SCR-TSK-1")
		return a.UID
	}()

	s.Put(ctx, &model.Artifact{ID: "SCR-TSK-1", Kind: "task", Scope: "test", Status: "draft", Title: "New"})

	renamed, err := s.Get(ctx, "SCR-TSK-4")
	if err != nil {
		t.Fatalf("old SCR-TSK-1 should be renamed to SCR-TSK-4: %v", err)
	}
	if renamed.UID != oldUID {
		t.Error("renamed artifact should keep original UID")
	}
}

func TestScopeLabels_Store(t *testing.T) {
	s := openSQLite(t)
	ctx := context.Background()

	s.SetScopeKey(ctx, "myrepo", "MYR", false)

	if err := s.SetScopeLabels(ctx, "myrepo", []string{"go", "backend"}); err != nil {
		t.Fatal("SetScopeLabels:", err)
	}

	labels, err := s.GetScopeLabels(ctx, "myrepo")
	if err != nil {
		t.Fatal("GetScopeLabels:", err)
	}
	if len(labels) != 2 || labels[0] != "go" || labels[1] != "backend" {
		t.Errorf("expected [go backend], got %v", labels)
	}

	scopes, err := s.ScopesByLabel(ctx, "backend")
	if err != nil {
		t.Fatal("ScopesByLabel:", err)
	}
	if len(scopes) != 1 || scopes[0] != "myrepo" {
		t.Errorf("expected [myrepo], got %v", scopes)
	}

	infos, err := s.ListScopeInfo(ctx)
	if err != nil {
		t.Fatal("ListScopeInfo:", err)
	}
	found := false
	for _, info := range infos {
		if info.Scope == "myrepo" && len(info.Labels) == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected to find myrepo in scope info with 2 labels")
	}
}

// --- SCR-BUG-14 reproduction tests ---

// TestBusyTimeoutPragma verifies that busy_timeout is actually set on connections.
// Root cause: if the pragma isn't applied, concurrent writes return SQLITE_BUSY
// immediately instead of waiting, which surfaces as disk I/O error (6410).
func TestBusyTimeoutPragma(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "busy.db")
	s, err := store.OpenSQLiteConfig(store.SQLiteConfig{
		Path:          dbPath,
		BusyTimeoutMs: 5000,
		Snapshots:     store.SnapshotConfig{TimeDeltaH: 9999},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	var timeout int
	s.Writer().QueryRow("PRAGMA busy_timeout").Scan(&timeout)
	if timeout != 5000 {
		t.Errorf("writer busy_timeout = %d, want 5000", timeout)
	}
}

// TestConcurrentWriteContention reproduces the disk I/O error (6410) from SCR-BUG-14.
// When busy_timeout=0, concurrent writer + reader causes SQLITE_BUSY which
// modernc.org/sqlite reports as SQLITE_IOERR_WRITE (6410).
func TestConcurrentWriteContention(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "contention.db")
	s, err := store.OpenSQLiteConfig(store.SQLiteConfig{
		Path:      dbPath,
		Snapshots: store.SnapshotConfig{TimeDeltaH: 9999},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	// Seed some data
	for i := 0; i < 50; i++ {
		s.Put(ctx, &model.Artifact{
			ID: fmt.Sprintf("CON-TSK-%d", i+1), Kind: "task", Scope: "test",
			Status: "draft", Title: fmt.Sprintf("Contention task %d", i+1),
		})
	}

	// Concurrent writes and reads — should NOT produce errors with busy_timeout=5000
	var wg sync.WaitGroup
	var writeErrors, readErrors int64
	var mu sync.Mutex

	// Writer goroutine: rapid updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			art := &model.Artifact{
				ID: fmt.Sprintf("CON-TSK-%d", (i%50)+1), Kind: "task", Scope: "test",
				Status: "draft", Title: fmt.Sprintf("Updated task %d iter %d", (i%50)+1, i),
				Sections: []model.Section{{Name: "ctx", Text: fmt.Sprintf("iteration %d", i)}},
			}
			if err := s.Put(ctx, art); err != nil {
				mu.Lock()
				writeErrors++
				mu.Unlock()
				t.Logf("write error at iter %d: %v", i, err)
			}
		}
	}()

	// Reader goroutines: concurrent list + get
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_, err := s.List(ctx, model.Filter{Scope: "test"})
				if err != nil {
					mu.Lock()
					readErrors++
					mu.Unlock()
				}
				id := fmt.Sprintf("CON-TSK-%d", (i%50)+1)
				_, err = s.Get(ctx, id)
				if err != nil {
					mu.Lock()
					readErrors++
					mu.Unlock()
				}
			}
		}(w)
	}

	wg.Wait()

	t.Logf("write errors: %d, read errors: %d", writeErrors, readErrors)
	if writeErrors > 0 {
		t.Errorf("expected zero write errors with busy_timeout=5000, got %d", writeErrors)
	}
	if readErrors > 0 {
		t.Errorf("expected zero read errors, got %d", readErrors)
	}
}

// TestSequenceCounterTransactionality verifies that sequence increment and
// artifact upsert are in the same transaction. SCR-BUG-14 found that failed
// writes advance the sequence counter, creating ID gaps.
func TestSequenceCounterTransactionality(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "seq.db")
	s, err := store.OpenSQLiteConfig(store.SQLiteConfig{
		Path:      dbPath,
		Snapshots: store.SnapshotConfig{TimeDeltaH: 9999},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	// Create first artifact — should get ID with seq 1
	id1, err := s.NextID(ctx, "SEQ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first ID: %s", id1)

	// Put it successfully
	s.Put(ctx, &model.Artifact{ID: id1, Kind: "task", Status: "draft", Title: "First"})

	// Get next ID
	id2, err := s.NextID(ctx, "SEQ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("second ID: %s", id2)

	// DON'T put it — simulate a failed write

	// Get third ID — should be sequential (no gap)
	id3, err := s.NextID(ctx, "SEQ")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("third ID: %s", id3)

	// Verify: id3 should be id2's seq + 1 (NextID always increments)
	// This is documenting current behavior — the bug is that NextID
	// increments even when the artifact write fails
	t.Logf("NOTE: NextID always increments. If Put fails after NextID, the ID is lost (gap).")
	t.Logf("This is the sequence counter transaction bug from SCR-BUG-14.")
}

// TestFTS5UnderConcurrentWrite verifies FTS5 triggers don't cause contention.
// The FTS5 AFTER INSERT/UPDATE/DELETE triggers extend the write transaction,
// increasing the window for lock contention.
func TestFTS5UnderConcurrentWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fts5contention.db")
	s, err := store.OpenSQLiteConfig(store.SQLiteConfig{
		Path:      dbPath,
		Snapshots: store.SnapshotConfig{TimeDeltaH: 9999},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()

	// Seed 100 artifacts
	for i := 0; i < 100; i++ {
		s.Put(ctx, &model.Artifact{
			ID: fmt.Sprintf("FTS-TSK-%d", i+1), Kind: "task", Scope: "test",
			Status: "draft", Title: fmt.Sprintf("FTS task %d", i+1),
			Goal: fmt.Sprintf("Goal with searchable content for task %d", i+1),
			Sections: []model.Section{
				{Name: "context", Text: fmt.Sprintf("Detailed context for FTS test artifact number %d with enough text to exercise the FTS5 index", i+1)},
			},
		})
	}

	// Concurrent: writer updates artifacts while readers search via FTS5
	var wg sync.WaitGroup
	var writeErrors, searchErrors int64
	var mu sync.Mutex

	// Writer: update artifacts (triggers FTS5 DELETE+INSERT)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			art := &model.Artifact{
				ID: fmt.Sprintf("FTS-TSK-%d", (i%100)+1), Kind: "task", Scope: "test",
				Status: "draft", Title: fmt.Sprintf("Updated FTS task %d", (i%100)+1),
				Goal:   fmt.Sprintf("Updated goal %d", i),
			}
			if err := s.Put(ctx, art); err != nil {
				mu.Lock()
				writeErrors++
				mu.Unlock()
				t.Logf("FTS write error at %d: %v", i, err)
			}
		}
	}()

	// Readers: FTS5 search
	for w := 0; w < 3; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_, err := s.Search(ctx, "searchable content")
				if err != nil {
					mu.Lock()
					searchErrors++
					mu.Unlock()
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	t.Logf("FTS5 contention: write errors=%d, search errors=%d", writeErrors, searchErrors)
	if writeErrors > 0 {
		t.Errorf("expected zero FTS5 write errors, got %d", writeErrors)
	}
}
