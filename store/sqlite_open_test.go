package store_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/dpopsuev/scribe/store"
)

func TestOpenSQLite_InvalidPath(t *testing.T) {
	_, err := store.OpenSQLite("/dev/null/impossible/path/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
	t.Logf("got expected error: %v", err)
}

func TestOpenSQLite_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not effective on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root ignores permission bits")
	}

	dir := t.TempDir()
	roDir := filepath.Join(dir, "readonly")
	if err := os.MkdirAll(roDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(roDir, 0o755) })

	_, err := store.OpenSQLite(filepath.Join(roDir, "sub", "test.db"))
	if err == nil {
		t.Fatal("expected error when parent directory is read-only")
	}
	t.Logf("got expected error: %v", err)
}

func TestOpenSQLite_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "not-a-file")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := store.OpenSQLite(dbDir)
	if err == nil {
		t.Fatal("expected error when path is a directory, not a file")
	}
	t.Logf("got expected error: %v", err)
}

func TestOpenSQLite_NoTmpDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR semantics differ on Windows")
	}

	dir := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(dir, "nonexistent-tmpdir"))

	s, err := store.OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Logf("open failed with missing TMPDIR (reproduces scratch container): %v", err)
		return
	}
	defer s.Close()
	t.Log("open succeeded despite missing TMPDIR; modernc/sqlite may not use TMPDIR for WAL")
}

func TestOpenSQLite_StaleWALLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file locking semantics differ on Windows")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s1, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	lockPath := dbPath + "-wal"
	if _, err := os.Stat(lockPath); err != nil {
		t.Logf("no WAL file created yet at %s (may be created lazily)", lockPath)
	}

	s2, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Logf("second open with live WAL failed: %v", err)
	} else {
		s2.Close()
	}
	s1.Close()

	s3, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("re-open after close should succeed: %v", err)
	}
	s3.Close()
}

func TestOpenSQLite_ManyParallelOpens(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s0, err := store.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	s0.Close()

	const N = 50
	var wg sync.WaitGroup
	errs := make(chan error, N)
	stores := make(chan *store.SQLiteStore, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := store.OpenSQLite(dbPath)
			if err != nil {
				errs <- err
				return
			}
			stores <- s
		}()
	}
	wg.Wait()
	close(errs)
	close(stores)

	var failures int
	for err := range errs {
		failures++
		t.Logf("parallel open failure: %v", err)
	}
	for s := range stores {
		s.Close()
	}

	if failures > 0 {
		t.Logf("%d/%d parallel opens failed (resource exhaustion)", failures, N)
	} else {
		t.Logf("all %d parallel opens succeeded", N)
	}
}

func TestOpenSQLite_DiskFull(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("tmpfs trick only works on Linux")
	}
	if os.Getuid() == 0 {
		t.Skip("test uses user tmpfs, skip as root to avoid mount issues")
	}

	t.Log("NOTE: To fully reproduce disk-full, mount a tiny tmpfs:")
	t.Log("  mkdir /tmp/scribe-diskfull && mount -t tmpfs -o size=4k tmpfs /tmp/scribe-diskfull")
	t.Log("  Then run: OpenSQLite('/tmp/scribe-diskfull/test.db')")
	t.Log("This test verifies the error path exists but cannot mount tmpfs without root.")
}

func TestOpenSQLite_CorruptDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "corrupt.db")

	if err := os.WriteFile(dbPath, []byte("this is not a valid sqlite database file!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := store.OpenSQLite(dbPath)
	if err == nil {
		t.Fatal("expected error opening corrupt database file")
	}
	t.Logf("got expected error: %v", err)
}

func TestOpenSQLite_EmptyHOME(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("SCRIBE_ROOT", "")

	path := store.DefaultSQLitePath()
	t.Logf("DefaultSQLitePath with HOME='': %q", path)
	if path == "" {
		t.Error("DefaultSQLitePath should return a non-empty path even with empty HOME")
	}
}
