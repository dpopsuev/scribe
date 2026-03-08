package lifecycle

import (
	"os"
	"path/filepath"
	"strings"
)

// DrainEntry represents a discovered contract markdown file.
type DrainEntry struct {
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	Filename string `json:"filename"`
	SizeB    int64  `json:"size_bytes"`
}

// DrainDiscover walks root for .md files (skipping templates) and returns
// the discovered entries. The agent is responsible for reading each file
// and creating artifacts via Scribe tools.
func DrainDiscover(root string) ([]DrainEntry, error) {
	var entries []DrainEntry
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		if strings.HasPrefix(info.Name(), "_") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		entries = append(entries, DrainEntry{
			Path:     path,
			Dir:      filepath.Dir(rel),
			Filename: info.Name(),
			SizeB:    info.Size(),
		})
		return nil
	})
	return entries, err
}

// DrainCleanup deletes the given files from disk.
func DrainCleanup(paths []string) (int, error) {
	var removed int
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
