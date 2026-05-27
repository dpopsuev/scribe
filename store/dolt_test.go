package store_test

// dolt_test.go — DoltStore RED tests.
//
// Two contracts:
//
// 1. Store contract — same suite as SQLiteStore and MemoryStore.
//    DoltStore must be a drop-in replacement for Protocol.
//
// 2. Session contract — the unique value Dolt adds over SQLite.
//    Each agent session works on its own branch; human reviews the diff
//    before merging to main. Multiple agents write concurrently without
//    interleaving their deposits.

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/store"
)

// newDoltStore creates an ephemeral in-process Dolt store for testing.
// Each call gets a fresh database — no shared state between tests.
func newDoltStore(t *testing.T) parchment.Store {
	t.Helper()
	s, err := store.OpenDolt(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDolt: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ─── Store contract ───────────────────────────────────────────────────────────

// TestDoltStore_Contract runs the full parchment Store contract suite against
// DoltStore. All methods must behave identically to SQLiteStore and MemoryStore.
func TestDoltStore_Contract(t *testing.T) {
	// The contract suite lives in parchment — run it against DoltStore.
	// This test will FAIL until DoltStore satisfies parchment.Store.
	_ = newDoltStore(t)
	t.Log("DoltStore contract: placeholder — implement storeContract call once DoltStore compiles")
}

// ─── Session contract ─────────────────────────────────────────────────────────

// TestDoltStore_SessionIsolation verifies that two concurrent agent sessions
// write to separate branches and do not see each other's deposits.
func TestDoltStore_SessionIsolation(t *testing.T) {
	dir := t.TempDir()

	// Agent A — working on EDA patterns
	storeA, err := store.OpenDoltSession(dir, "session-agent-a")
	if err != nil {
		t.Fatalf("OpenDoltSession agent-a: %v", err)
	}
	defer storeA.Close() //nolint:errcheck

	// Agent B — working on Go patterns
	storeB, err := store.OpenDoltSession(dir, "session-agent-b")
	if err != nil {
		t.Fatalf("OpenDoltSession agent-b: %v", err)
	}
	defer storeB.Close() //nolint:errcheck

	ctx := context.Background()

	// Agent A deposits an EDA concept
	_ = storeA.Put(ctx, &parchment.Artifact{
		ID: "CON-EDA-1", Kind: "concept", Status: "fleeting",
		Title: "Three-bus EDA — sensory motor signal",
	})

	// Agent B deposits a Go concept
	_ = storeB.Put(ctx, &parchment.Artifact{
		ID: "CON-GO-1", Kind: "concept", Status: "fleeting",
		Title: "Composition over inheritance in Go",
	})

	// Agent A must NOT see Agent B's deposit
	arts, _ := storeA.List(ctx, parchment.Filter{})
	for _, a := range arts {
		if a.ID == "CON-GO-1" {
			t.Errorf("session-agent-a must not see session-agent-b deposit CON-GO-1")
		}
	}

	// Agent B must NOT see Agent A's deposit
	arts, _ = storeB.List(ctx, parchment.Filter{})
	for _, a := range arts {
		if a.ID == "CON-EDA-1" {
			t.Errorf("session-agent-b must not see session-agent-a deposit CON-EDA-1")
		}
	}
}

// TestDoltStore_SessionCommitAndDiff verifies that committing a session
// produces a readable diff showing exactly what the agent deposited.
func TestDoltStore_SessionCommitAndDiff(t *testing.T) {
	dir := t.TempDir()

	s, err := store.OpenDoltSession(dir, "session-test")
	if err != nil {
		t.Fatalf("OpenDoltSession: %v", err)
	}
	defer s.Close() //nolint:errcheck

	ctx := context.Background()
	_ = s.Put(ctx, &parchment.Artifact{
		ID: "CON-1", Kind: "concept", Status: "fleeting",
		Title: "HNSW — hierarchical navigable small world",
	})

	// Commit the session
	if err := s.Commit(ctx, "session-test: HNSW concept deposited"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Diff against main — must show the deposited artifact
	diff, err := s.DiffFromMain(ctx)
	if err != nil {
		t.Fatalf("DiffFromMain: %v", err)
	}
	if len(diff) == 0 {
		t.Error("DiffFromMain must return non-empty diff after commit")
	}
	// The diff must mention the artifact
	found := false
	for _, d := range diff {
		if strings.Contains(d, "CON-1") || strings.Contains(d, "HNSW") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("DiffFromMain must mention deposited artifact CON-1\nGot: %v", diff)
	}
}

// TestDoltStore_SessionMerge verifies that merging a session branch into main
// makes the deposits visible to a store opened on main.
func TestDoltStore_SessionMerge(t *testing.T) {
	dir := t.TempDir()

	// Open session branch
	session, err := store.OpenDoltSession(dir, "session-merge-test")
	if err != nil {
		t.Fatalf("OpenDoltSession: %v", err)
	}

	ctx := context.Background()
	_ = session.Put(ctx, &parchment.Artifact{
		ID: "CON-MERGE-1", Kind: "concept", Status: "evergreen",
		Title: "RRF — Reciprocal Rank Fusion for hybrid search",
	})
	if err := session.Commit(ctx, "deposit: RRF concept"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := session.MergeToMain(ctx); err != nil {
		t.Fatalf("MergeToMain: %v", err)
	}
	_ = session.Close()

	// Open main — merged artifact must be visible
	main, err := store.OpenDolt(dir)
	if err != nil {
		t.Fatalf("OpenDolt main: %v", err)
	}
	defer main.Close() //nolint:errcheck

	art, err := main.Get(ctx, "CON-MERGE-1")
	if err != nil {
		t.Fatalf("merged artifact not found on main: %v", err)
	}
	if art.Title != "RRF — Reciprocal Rank Fusion for hybrid search" {
		t.Errorf("wrong title after merge: %q", art.Title)
	}
}

// TestDoltStore_BadSessionNotMerged verifies that a session branch that is
// never merged to main leaves main clean. Bad deposits stay isolated.
func TestDoltStore_BadSessionNotMerged(t *testing.T) {
	dir := t.TempDir()

	// Bad agent session — deposits wrong concept, never merged
	bad, err := store.OpenDoltSession(dir, "session-bad")
	if err != nil {
		t.Fatalf("OpenDoltSession: %v", err)
	}

	ctx := context.Background()
	_ = bad.Put(ctx, &parchment.Artifact{
		ID: "CON-BAD-1", Kind: "concept", Status: "fleeting",
		Title: "sensory-motor-bus — organs must not read sensory bus", // WRONG
	})
	_ = bad.Commit(ctx, "deposit: sensory-motor-bus (bad)")
	// Not merged — bad.MergeToMain intentionally not called
	_ = bad.Close()

	// Main remains clean
	main, err := store.OpenDolt(dir)
	if err != nil {
		t.Fatalf("OpenDolt main: %v", err)
	}
	defer main.Close() //nolint:errcheck

	arts, _ := main.List(ctx, parchment.Filter{})
	for _, a := range arts {
		if a.ID == "CON-BAD-1" {
			t.Errorf("bad deposit CON-BAD-1 must not appear on main — it was never merged")
		}
	}
}
