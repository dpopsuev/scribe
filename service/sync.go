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
	logKeySyncScope = "scope"
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

	err = filepath.Walk(abs, func(p string, info os.FileInfo, walkErr error) error {
		return s.syncWalkFn(ctx, abs, p, info, walkErr, currentIDs, &arts)
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

// syncWalkFn is the filepath.Walk callback for SyncDir.
// It parses each .md file and appends the resulting artifact to arts.
func (s *Service) syncWalkFn(ctx context.Context, abs, p string, info os.FileInfo, walkErr error, currentIDs map[string]bool, arts *[]*parchment.Artifact) error {
	if walkErr != nil {
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
	if art.Label(parchment.LabelPrefixKind) == "" {
		art.Labels = append(art.Labels, parchment.LabelPrefixKind+"knowledge.note")
	}
	if art.Label(parchment.LabelPrefixScope) == "" {
		art.Labels = append(art.Labels, parchment.LabelPrefixScope+"global")
	}
	// Behavioral labels ("rule", "skill") are not registered kinds; convert to knowledge.note.
	if k := art.Label(parchment.LabelPrefixKind); k == "rule" || k == "skill" {
		art.Labels = appendIfMissing(art.Labels, k)
		for i, l := range art.Labels {
			if l == parchment.LabelPrefixKind+k {
				art.Labels[i] = parchment.LabelPrefixKind + "knowledge.note"
				break
			}
		}
	}
	if art.Extra == nil {
		art.Extra = make(map[string]any)
	}
	art.Extra[syncSourceKey] = abs
	currentIDs[art.ID] = true
	*arts = append(*arts, art)
	return nil
}

// appendIfMissing returns labels with label appended if it is not already present.
func appendIfMissing(labels []string, label string) []string {
	for _, l := range labels {
		if l == label {
			return labels
		}
	}
	return append(labels, label)
}

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
