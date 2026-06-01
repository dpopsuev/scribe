package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

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
		switch art.Kind {
		case "rule", "skill":
			art.Labels = appendIfMissing(art.Labels, art.Kind)
			art.Kind = "note"
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

// ExportScope writes all artifacts in scope to outDir as .md files.
// Each file contains YAML frontmatter + ## section headings.
// Round-trip: ExportScope then SyncDir reproduces the same graph.
func (s *Service) ExportScope(ctx context.Context, scope, outDir string) (int, error) {
	if err := os.MkdirAll(outDir, 0o750); err != nil { //nolint:gosec // operator-controlled output dir
		return 0, fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	arts, err := s.Proto.Store().List(ctx, parchment.Filter{Scope: scope})
	if err != nil {
		return 0, err
	}

	for _, art := range arts {
		slug := toSlug(art.Title)
		filename := fmt.Sprintf("%s--%s.md", art.ID, slug)
		path := filepath.Join(outDir, filename)

		content, err := serializeArtifact(art)
		if err != nil {
			slog.WarnContext(ctx, "export: serialize failed",
				slog.String(logKeySyncID, art.ID), slog.Any(logKeySyncError, err))
			continue
		}
		if err := os.WriteFile(path, content, 0o644); err != nil { //nolint:gosec // operator-controlled path
			return 0, fmt.Errorf("write %s: %w", path, err)
		}
	}

	slog.InfoContext(ctx, "export: done",
		slog.String(logKeySyncScope, scope), slog.Int(logKeySyncCount, len(arts)))
	return len(arts), nil
}

func serializeArtifact(art *parchment.Artifact) ([]byte, error) {
	fm := map[string]any{"id": art.ID, "kind": art.Kind}
	if art.Title != "" {
		fm["title"] = art.Title
	}
	if art.Scope != "" {
		fm["scope"] = art.Scope
	}
	if art.Status != "" {
		fm["status"] = art.Status
	}
	if art.Priority != "" && art.Priority != "none" {
		fm["priority"] = art.Priority
	}
	if len(art.Labels) > 0 {
		fm["labels"] = art.Labels
	}
	if art.Parent != "" {
		fm["parent"] = art.Parent
	}
	if len(art.DependsOn) > 0 {
		fm["depends_on"] = art.DependsOn
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmBytes)
	buf.WriteString("---\n")

	for _, sec := range art.Sections {
		buf.WriteString("\n## ")
		buf.WriteString(strings.ReplaceAll(sec.Name, "_", " "))
		buf.WriteString("\n\n")
		buf.WriteString(strings.TrimSpace(sec.Text))
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

func toSlug(s string) string {
	s = strings.ToLower(s)
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
}
