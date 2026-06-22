//go:build db_stress

// Package db_stress contains storage-engine-specific stress tests.
// Add kuzu_test.go, duckdb_test.go etc. here for A/B comparison.
package db_stress_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	parchment "github.com/dpopsuev/parchment"
)

// TestSQLite_WALGrowth verifies the WAL file stays bounded under sustained writes.
func TestSQLite_WALGrowth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal-stress.db")
	s, err := parchment.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	for i := 0; i < 2000; i++ {
		art := &parchment.Artifact{
			ID:     fmt.Sprintf("WAL-TSK-%d", i+1),
			Labels: []string{"kind:effort.task", "status:draft", "project:stress"},
			Title:  fmt.Sprintf("WAL stress task %d with some padding text", i+1),
			Sections: []parchment.Section{
				{Name: "context", Text: fmt.Sprintf("Section content for artifact %d. Representative of real-world section sizes.", i+1)},
			},
		}
		if err := s.Put(ctx, art); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	walPath := dbPath + "-wal"
	info, err := os.Stat(walPath)
	if err != nil {
		t.Logf("WAL file not found (may be checkpointed): %v", err)
	} else {
		walSizeMB := float64(info.Size()) / (1024 * 1024)
		t.Logf("WAL size after 2000 writes: %.1fMB", walSizeMB)
		if walSizeMB > 50 {
			t.Errorf("WAL file too large: %.1fMB (expected < 50MB)", walSizeMB)
		}
	}
	dbInfo, _ := os.Stat(dbPath)
	t.Logf("DB size: %.1fMB", float64(dbInfo.Size())/(1024*1024))
}

// TestSQLite_ConnectionPool verifies connections are bounded and not leaking.
func TestSQLite_ConnectionPool(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "conn-stress.db")
	s, err := parchment.OpenSQLiteConfig(parchment.SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	for i := 0; i < 50; i++ {
		_ = s.Put(ctx, &parchment.Artifact{
			ID:     fmt.Sprintf("SEED-%d", i),
			Labels: []string{"kind:effort.task", "status:draft", "project:stress"},
			Title:  fmt.Sprintf("seed %d", i),
		})
	}

	writer := s.Writer()
	before := writer.Stats()
	t.Logf("before: open=%d inUse=%d idle=%d", before.OpenConnections, before.InUse, before.Idle)

	for i := 0; i < 1000; i++ {
		s.List(ctx, parchment.Filter{Labels: []string{"project:stress"}})
		if i%100 == 0 {
			stats := writer.Stats()
			t.Logf("  after %d queries: open=%d inUse=%d idle=%d", i, stats.OpenConnections, stats.InUse, stats.Idle)
		}
	}

	after := writer.Stats()
	t.Logf("after: open=%d inUse=%d idle=%d waitCount=%d", after.OpenConnections, after.InUse, after.Idle, after.WaitCount)
	if after.OpenConnections > 10 {
		t.Errorf("too many open connections: %d (expected <= 10)", after.OpenConnections)
	}
}

// WriteContention and CRUDCycle tests moved to store_test.go (backend-generic).
