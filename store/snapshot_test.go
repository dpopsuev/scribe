package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/store"
)

func openSnapshotStore(t *testing.T) (*store.SQLiteStore, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "snap.db")
	s, err := store.OpenSQLiteConfig(store.SQLiteConfig{
		Path:      dbPath,
		Snapshots: store.SnapshotConfig{TimeDeltaH: 9999}, // disable auto-snapshot
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dbPath
}

func TestSnapshot_CreateAndList(t *testing.T) {
	s, dbPath := openSnapshotStore(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "T-1", Kind: "task", Status: "draft", Title: "Task 1"})
	s.Put(ctx, &model.Artifact{ID: "T-2", Kind: "task", Status: "draft", Title: "Task 2"})

	backend := store.NewLocalSnapshotBackend(dbPath, s.Writer())
	snapshotter := store.NewSnapshotter(backend, s)

	meta, err := snapshotter.Create(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Artifacts != 2 {
		t.Errorf("snapshot artifacts = %d, want 2", meta.Artifacts)
	}
	if meta.SizeBytes == 0 {
		t.Error("snapshot size should be non-zero")
	}

	list, err := snapshotter.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// May have auto-snapshot from open + our manual one
	found := false
	for _, snap := range list {
		if snap.Name == "test" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected snapshot named 'test' in list of %d", len(list))
	}
}

func TestSnapshot_Diff(t *testing.T) {
	s, dbPath := openSnapshotStore(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "A-1", Kind: "task", Status: "draft", Title: "Will stay"})
	s.Put(ctx, &model.Artifact{ID: "A-2", Kind: "task", Status: "draft", Title: "Will be removed"})

	backend := store.NewLocalSnapshotBackend(dbPath, s.Writer())
	snapshotter := store.NewSnapshotter(backend, s)

	meta, _ := snapshotter.Create(ctx, "before")

	s.Delete(ctx, "A-2")
	a1, _ := s.Get(ctx, "A-1")
	a1.Title = "Updated"
	time.Sleep(10 * time.Millisecond)
	s.Put(ctx, a1)
	s.Put(ctx, &model.Artifact{ID: "A-3", Kind: "task", Status: "draft", Title: "New"})

	diff, err := snapshotter.Diff(ctx, meta.Key)
	if err != nil {
		t.Fatal(err)
	}

	if len(diff.Added) != 1 || diff.Added[0] != "A-3" {
		t.Errorf("added = %v, want [A-3]", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0] != "A-2" {
		t.Errorf("removed = %v, want [A-2]", diff.Removed)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "A-1" {
		t.Errorf("modified = %v, want [A-1]", diff.Modified)
	}
}

func TestSnapshot_Clean(t *testing.T) {
	s, dbPath := openSnapshotStore(t)
	ctx := context.Background()

	s.Put(ctx, &model.Artifact{ID: "T-1", Kind: "task", Status: "draft", Title: "Test"})

	backend := store.NewLocalSnapshotBackend(dbPath, s.Writer())
	snapshotter := store.NewSnapshotter(backend, s)

	cfg := store.SnapshotConfig{MaxCount: 3}

	// Create 6 snapshots with unique names
	for i := 0; i < 6; i++ {
		_, err := snapshotter.Create(ctx, fmt.Sprintf("s%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}

	list, _ := snapshotter.List(ctx)
	beforeClean := len(list)
	t.Logf("snapshots before cleanup: %d (includes auto-snapshot from open)", beforeClean)

	deleted, _ := snapshotter.Clean(ctx, cfg)
	t.Logf("deleted: %d", deleted)

	list, _ = snapshotter.List(ctx)
	if len(list) > 3 {
		t.Errorf("after cleanup with max_count=3, got %d snapshots", len(list))
	}
}
