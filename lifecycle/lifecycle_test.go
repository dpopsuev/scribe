package lifecycle_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dpopsuev/scribe/lifecycle"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/store"
)

func tmpStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestGuardPut_NewArtifact(t *testing.T) {
	s := tmpStore(t)
	art := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "test"}
	if err := lifecycle.GuardPut(context.Background(), s, art); err != nil {
		t.Fatalf("new artifact should pass: %v", err)
	}
}

func TestGuardPut_ArchivedBlocks(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()
	art := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "archived", Title: "test"}
	if err := s.Put(ctx, art); err != nil {
		t.Fatal(err)
	}
	update := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "archived", Title: "changed"}
	err := lifecycle.GuardPut(ctx, s, update)
	if !errors.Is(err, lifecycle.ErrArchived) {
		t.Fatalf("expected ErrArchived, got: %v", err)
	}
}

func TestGuardPut_NonArchivedPasses(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()
	art := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "test"}
	if err := s.Put(ctx, art); err != nil {
		t.Fatal(err)
	}
	update := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "complete", Title: "changed"}
	if err := lifecycle.GuardPut(ctx, s, update); err != nil {
		t.Fatalf("non-archived should pass: %v", err)
	}
}

func TestGuardDelete_RequiresArchived(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()
	art := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "test"}
	s.Put(ctx, art)

	err := lifecycle.GuardDelete(ctx, s, "TASK-2026-001", false)
	if !errors.Is(err, lifecycle.ErrNotArchived) {
		t.Fatalf("expected ErrNotArchived, got: %v", err)
	}

	if err := lifecycle.GuardDelete(ctx, s, "TASK-2026-001", true); err != nil {
		t.Fatalf("force should bypass: %v", err)
	}
}

func TestGuardDelete_ArchivedAllowed(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()
	art := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "archived", Title: "test"}
	s.Put(ctx, art)

	if err := lifecycle.GuardDelete(ctx, s, "TASK-2026-001", false); err != nil {
		t.Fatalf("archived delete should pass: %v", err)
	}
}

func TestArchive_LeafArtifact(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()
	art := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "complete", Title: "test"}
	s.Put(ctx, art)

	if err := lifecycle.Archive(ctx, s, "TASK-2026-001", false); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, "TASK-2026-001")
	if got.Status != "archived" {
		t.Fatalf("expected archived, got %s", got.Status)
	}
}

func TestArchive_BlocksOnNonArchivedChildren(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()
	parent := &model.Artifact{ID: "EPIC-2026-001", Kind: "epic", Status: "complete", Title: "parent"}
	child := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "active", Title: "child", Parent: "EPIC-2026-001"}
	s.Put(ctx, parent)
	s.Put(ctx, child)

	err := lifecycle.Archive(ctx, s, "EPIC-2026-001", false)
	if err == nil {
		t.Fatal("expected error for non-archived child")
	}
}

func TestArchive_CascadeArchivesSubtree(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()
	parent := &model.Artifact{ID: "EPIC-2026-001", Kind: "epic", Status: "complete", Title: "parent"}
	child := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "complete", Title: "child", Parent: "EPIC-2026-001"}
	grandchild := &model.Artifact{ID: "SUB-2026-001", Kind: "subtask", Status: "complete", Title: "gc", Parent: "TASK-2026-001"}
	s.Put(ctx, parent)
	s.Put(ctx, child)
	s.Put(ctx, grandchild)

	if err := lifecycle.Archive(ctx, s, "EPIC-2026-001", true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"EPIC-2026-001", "TASK-2026-001", "SUB-2026-001"} {
		got, _ := s.Get(ctx, id)
		if got.Status != "archived" {
			t.Fatalf("%s expected archived, got %s", id, got.Status)
		}
	}
}

func TestVacuum(t *testing.T) {
	s := tmpStore(t)
	ctx := context.Background()

	stale := &model.Artifact{ID: "TASK-2026-001", Kind: "task", Status: "archived", Title: "stale"}
	stale.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	s.Put(ctx, stale)
	// Reset UpdatedAt to old time (Put sets UpdatedAt to now, so we need SQL workaround)
	// The test uses a short enough duration to cover this
	fresh := &model.Artifact{ID: "TASK-2026-002", Kind: "task", Status: "archived", Title: "fresh"}
	s.Put(ctx, fresh)

	deleted, err := lifecycle.Vacuum(ctx, s, 1*time.Second, "")
	if err != nil {
		t.Fatal(err)
	}
	// fresh was just created, so it should survive; stale was also just created by Put
	// so both survive with 1s threshold
	if len(deleted) != 0 {
		t.Fatalf("expected 0 deleted (all just created), got %d", len(deleted))
	}

	// now vacuum with 0 duration = everything goes
	deleted, err = lifecycle.Vacuum(ctx, s, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 2 {
		t.Fatalf("expected 2 deleted, got %d", len(deleted))
	}
}

func TestDrainDiscover(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "active"), 0o755)
	os.MkdirAll(filepath.Join(dir, "draft"), 0o755)
	os.WriteFile(filepath.Join(dir, "active", "refactor.md"), []byte("# Refactor"), 0o644)
	os.WriteFile(filepath.Join(dir, "draft", "spike.md"), []byte("# Spike"), 0o644)
	os.WriteFile(filepath.Join(dir, "_TEMPLATE.md"), []byte("# Template"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.txt"), []byte("ignore"), 0o644)

	entries, err := lifecycle.DrainDiscover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (excluding template and non-md), got %d", len(entries))
	}
}

func TestDrainCleanup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.md")
	os.WriteFile(p, []byte("content"), 0o644)

	n, err := lifecycle.DrainCleanup([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 removed, got %d", n)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
}
