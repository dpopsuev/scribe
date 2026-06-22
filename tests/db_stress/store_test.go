//go:build db_stress

package db_stress_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	parchment "github.com/dpopsuev/parchment"
)

type storeOpener func(path string) (parchment.Store, error)

func tursoOpen(path string) (parchment.Store, error) {
	return parchment.OpenTursoConfig(parchment.TursoConfig{Path: path})
}

func sqliteOpen(path string) (parchment.Store, error) {
	return parchment.OpenSQLiteConfig(parchment.SQLiteConfig{Path: path, BusyTimeoutMs: 5000})
}

// writeContentionTest is the backend-generic write contention harness.
func writeContentionTest(t *testing.T, open storeOpener) {
	t.Helper()
	dbPath := t.TempDir() + "/contention.db"
	s, err := open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := s.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	})

	ctx := context.Background()
	const writers = 5
	const writesPerWriter = 200
	var mu sync.Mutex
	var writeErrors []string
	var wg sync.WaitGroup

	for w := range writers {
		wg.Add(1)
		go func(wIdx int) {
			defer wg.Done()
			for i := range writesPerWriter {
				id := fmt.Sprintf("W%d-TSK-%d", wIdx, i)
				if err := s.Put(ctx, &parchment.Artifact{
					ID:     id,
					Labels: []string{"kind:effort.task", "status:draft", "project:test"},
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
		t.Errorf("%d write errors (expected 0):\n%v", len(writeErrors), writeErrors[:min(5, len(writeErrors))])
	}
	all, _ := s.List(ctx, parchment.Filter{Labels: []string{"kind:effort.task"}})
	if len(all) != writers*writesPerWriter {
		t.Errorf("expected %d artifacts, got %d", writers*writesPerWriter, len(all))
	}
}

// crudCycleTest verifies open/close cycles don't lose data.
func crudCycleTest(t *testing.T, open storeOpener) {
	t.Helper()
	for i := range 10 {
		path := t.TempDir() + fmt.Sprintf("/cycle-%d.db", i)
		s, err := open(path)
		if err != nil {
			t.Fatalf("open cycle %d: %v", i, err)
		}
		ctx := context.Background()
		id := fmt.Sprintf("CYC-%d", i)
		if err := s.Put(ctx, &parchment.Artifact{
			ID:     id,
			Labels: []string{"kind:effort.task", "status:draft", "project:test"},
			Title:  fmt.Sprintf("Cycle %d", i),
		}); err != nil {
			if c, ok := s.(interface{ Close() error }); ok {
				c.Close()
			}
			t.Fatalf("put cycle %d: %v", i, err)
		}
		art, err := s.Get(ctx, id)
		if err != nil || art.Title != fmt.Sprintf("Cycle %d", i) {
			if c, ok := s.(interface{ Close() error }); ok {
				c.Close()
			}
			t.Fatalf("get cycle %d failed", i)
		}
		if c, ok := s.(interface{ Close() error }); ok {
			c.Close()
		}
	}
}

func TestSQLite_WriteContention(t *testing.T) { writeContentionTest(t, sqliteOpen) }
func TestTurso_WriteContention(t *testing.T)  { writeContentionTest(t, tursoOpen) }
func TestSQLite_CRUDCycle(t *testing.T)       { crudCycleTest(t, sqliteOpen) }
func TestTurso_CRUDCycle(t *testing.T)        { crudCycleTest(t, tursoOpen) }
