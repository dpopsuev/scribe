package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

const (
	syncSourceKey   = "sync_source"
	logKeySyncPath  = "path"
	logKeySyncID    = "id"
	logKeySyncCount = "count"
	logKeySyncError = "error"
)

// SyncDir walks path for .md files, upserts each as a Parchment artifact,
// and prunes artifacts from a previous sync of the same path that no longer exist.
func (s *Service) SyncDir(ctx context.Context, path string) (int, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("resolve path: %w", err)
	}

	var arts []*parchment.Artifact
	currentIDs := make(map[string]bool)

	err = filepath.Walk(abs, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable paths
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}

		art, err := parchment.ParseMDFile(p)
		if err != nil {
			slog.WarnContext(ctx, "sync: parse failed", slog.String(logKeySyncPath, p), slog.Any(logKeySyncError, err))
			return nil
		}

		if art.ID == "" {
			rel, _ := filepath.Rel(abs, p)
			art.ID = syncDerivedID(rel)
		}
		if art.Scope == "" {
			art.Scope = "global"
		}
		if art.Extra == nil {
			art.Extra = make(map[string]any)
		}
		art.Extra[syncSourceKey] = abs

		currentIDs[art.ID] = true
		arts = append(arts, art)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("walk %s: %w", abs, err)
	}

	store := s.Proto.Store()

	if len(arts) > 0 {
		errs := store.BulkPut(ctx, arts)
		for i, e := range errs {
			if e != nil && i < len(arts) {
				slog.WarnContext(ctx, "sync: bulk put failed",
					slog.String(logKeySyncID, arts[i].ID), slog.Any(logKeySyncError, e))
			}
		}
	}

	// Prune artifacts sourced from this path that are no longer on disk.
	existing, _ := store.List(ctx, parchment.Filter{})
	for _, art := range existing {
		src, _ := art.Extra[syncSourceKey].(string)
		if src != abs {
			continue
		}
		if !currentIDs[art.ID] {
			_ = store.Delete(ctx, art.ID)
		}
	}

	slog.InfoContext(ctx, "sync: done", slog.String(logKeySyncPath, abs), slog.Int(logKeySyncCount, len(arts)))
	return len(arts), nil
}

// syncDerivedID returns a deterministic artifact ID from a relative file path.
func syncDerivedID(rel string) string {
	h := sha256.Sum256([]byte(rel))
	slug := strings.TrimSuffix(filepath.Base(rel), ".md")
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '-'
	}, slug)
	return fmt.Sprintf("SYN-%x--%s", h[:3], slug)
}
