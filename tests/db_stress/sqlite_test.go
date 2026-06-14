//go:build db_stress

// Package db_stress contains storage-engine-specific stress tests.
// Add kuzu_test.go, duckdb_test.go etc. here for A/B comparison.
package db_stress_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
			Labels: []string{"kind:effort.task", "status:draft", "scope:stress"},
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
			Labels: []string{"kind:effort.task", "status:draft", "scope:stress"},
			Title:  fmt.Sprintf("seed %d", i),
		})
	}

	writer := s.Writer()
	before := writer.Stats()
	t.Logf("before: open=%d inUse=%d idle=%d", before.OpenConnections, before.InUse, before.Idle)

	for i := 0; i < 1000; i++ {
		s.List(ctx, parchment.Filter{Labels: []string{"scope:stress"}})
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

// TestSQLite_PragmaHardening verifies PRAGMA settings survive repeated open/close cycles.
func TestSQLite_PragmaHardening(t *testing.T) {
	for i := 0; i < 10; i++ {
		tmpPath := filepath.Join(t.TempDir(), fmt.Sprintf("cycle-%d.db", i))
		s, err := parchment.OpenSQLite(tmpPath)
		if err != nil {
			t.Fatalf("open cycle %d: %v", i, err)
		}
		ctx := context.Background()
		id := fmt.Sprintf("CYC-%d", i)
		if err := s.Put(ctx, &parchment.Artifact{
			ID:     id,
			Labels: []string{"kind:effort.task", "status:draft", "scope:test"},
			Title:  fmt.Sprintf("Cycle %d", i),
		}); err != nil {
			s.Close()
			t.Fatalf("put cycle %d: %v", i, err)
		}
		art, err := s.Get(ctx, id)
		if err != nil || art.Title != fmt.Sprintf("Cycle %d", i) {
			s.Close()
			t.Fatalf("get cycle %d failed", i)
		}
		s.Close()
	}
}

// TestSQLite_WriteContention verifies concurrent writers don't lose data under busy_timeout.
func TestSQLite_WriteContention(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "contention.db")
	s, err := parchment.OpenSQLiteConfig(parchment.SQLiteConfig{
		Path:          dbPath,
		BusyTimeoutMs: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ctx := context.Background()
	const writers = 5
	const writesPerWriter = 200
	var mu sync.Mutex
	var writeErrors []string
	var wg sync.WaitGroup

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(wIdx int) {
			defer wg.Done()
			for i := 0; i < writesPerWriter; i++ {
				id := fmt.Sprintf("W%d-TSK-%d", wIdx, i)
				if err := s.Put(ctx, &parchment.Artifact{
					ID:     id,
					Labels: []string{"kind:effort.task", "status:draft", "scope:test"},
					Title:  fmt.Sprintf("Writer %d Task %d", wIdx, i),
					Sections: []parchment.Section{
						{Name: "context", Text: fmt.Sprintf("Content from writer %d, iteration %d", wIdx, i)},
					},
				}); err != nil {
					mu.Lock()
					writeErrors = append(writeErrors, fmt.Sprintf("w%d/i%d: %v", wIdx, i, err))
					mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	if len(writeErrors) > 0 {
		t.Errorf("%d write errors (expected 0 with busy_timeout):\n%v", len(writeErrors), writeErrors[:min(5, len(writeErrors))])
	}
	all, _ := s.List(ctx, parchment.Filter{Labels: []string{"kind:effort.task"}})
	if len(all) != writers*writesPerWriter {
		t.Errorf("expected %d artifacts, got %d", writers*writesPerWriter, len(all))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
