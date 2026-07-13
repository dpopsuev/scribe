package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const (
	exportDirPerm    = 0o750
	exportFilePerm   = 0o644
	exportSlugMaxLen = 80
)

func init() {
	Registry = append(Registry, opExport)
}

type exportInput struct {
	ID     string `json:"id,omitempty"`
	Scope  string `json:"scope,omitempty"`
	OutDir string `json:"out_dir,omitempty"`
	Format string `json:"format,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

var opExport = Op{
	Name: "export",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in exportInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Format != "" && in.Format != "markdown" {
			return "", fmt.Errorf("unsupported export format %q (supported: markdown)", in.Format) //nolint:err113 // agent-facing
		}
		if in.ID != "" {
			md, err := svc.ExportArtifactMarkdown(ctx, in.ID)
			if err != nil {
				return "", err
			}
			if in.OutDir != "" {
				art, err := svc.Proto.GetArtifact(ctx, in.ID)
				if err != nil {
					return "", err
				}
				status, err := writeExportFile(in.OutDir, art, []byte(md), art.UpdatedAt, in.Force)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("exported %s (%s)", in.ID, status), nil
			}
			return md, nil
		}
		if in.Scope == "" || in.OutDir == "" {
			return "", fmt.Errorf("export requires id= or scope= + out_dir=") //nolint:err113 // agent-facing
		}
		n, err := svc.ExportScope(ctx, in.Scope, in.OutDir, in.Force)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("exported %d artifact(s) to %s", n, in.OutDir), nil
	},
}

// ExportArtifactMarkdown returns portable markdown for one artifact.
func (s *Service) ExportArtifactMarkdown(ctx context.Context, id string) (string, error) {
	art, err := s.Proto.GetArtifact(ctx, id)
	if err != nil {
		return "", err
	}
	return serializeArtifactMarkdown(ctx, s, art), nil
}

// ExportScope writes all artifacts in scope to outDir as markdown files.
// When force is false, skips unchanged files; writes .conflict.md when disk is newer and differs.
func (s *Service) ExportScope(ctx context.Context, scope, outDir string, force bool) (int, error) {
	if err := os.MkdirAll(outDir, exportDirPerm); err != nil { //nolint:gosec // operator-controlled output dir
		return 0, fmt.Errorf("mkdir %s: %w", outDir, err)
	}

	arts, err := s.Proto.List(ctx, parchment.Filter{Labels: []string{parchment.LabelPrefixScope + scope}})
	if err != nil {
		return 0, err
	}

	written := 0
	for _, art := range arts {
		content := serializeArtifactMarkdown(ctx, s, art)
		status, err := writeExportFile(outDir, art, []byte(content), art.UpdatedAt, force)
		if err != nil {
			return written, err
		}
		if status == "wrote" || status == "conflict" {
			written++
		}
	}
	return written, nil
}

func serializeArtifactMarkdown(ctx context.Context, svc *Service, art *parchment.Artifact) string {
	edges, err := svc.Proto.Neighbors(ctx, art.ID, "", parchment.Outgoing)
	if err != nil {
		edges = nil
	}
	titles := map[string]string{}
	for _, e := range edges {
		if target, gerr := svc.Proto.GetArtifact(ctx, e.To); gerr == nil && target != nil {
			titles[e.To] = target.Title
		}
	}
	links := parchment.ExportLinksFromEdges(edges, titles)
	md := parchment.ExportMarkdown(art, links)
	scope := art.Label(parchment.LabelPrefixScope)
	stable := StableArtifactLink(scope, art.ID)
	if d, via := ResolveCanonicalDecision(ctx, svc, art.ID); d != nil {
		return fmt.Sprintf("<!-- scribe:%s -->\n<!-- canonical:%s via=%s -->\n%s", stable, d.ID, via, md)
	}
	return fmt.Sprintf("<!-- scribe:%s -->\n%s", stable, md)
}

// StableArtifactLink is a portable reference: scribe://scope/id
func StableArtifactLink(scope, id string) string {
	if scope == "" {
		scope = "_"
	}
	return fmt.Sprintf("scribe://%s/%s", scope, id)
}

func exportFilename(art *parchment.Artifact) string {
	slug := toSlug(art.Title)
	if len(slug) > exportSlugMaxLen {
		slug = slug[:exportSlugMaxLen]
	}
	slug = strings.TrimRight(slug, "-")
	return fmt.Sprintf("%s--%s.md", art.ID, slug)
}

func writeExportFile(outDir string, art *parchment.Artifact, content []byte, updatedAt time.Time, force bool) (string, error) {
	path := filepath.Join(outDir, exportFilename(art))
	if !force {
		if status, done, err := exportIncrementalDecision(path, content, updatedAt); done {
			return status, err
		}
	}
	if err := os.WriteFile(path, content, exportFilePerm); err != nil { //nolint:gosec // operator path
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return "wrote", nil
}

func exportIncrementalDecision(path string, content []byte, updatedAt time.Time) (status string, done bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, nil
	}
	existing, rerr := os.ReadFile(path) //nolint:gosec // operator path
	sameHash := rerr == nil && sha256.Sum256(existing) == sha256.Sum256(content)
	if sameHash {
		return "skipped", true, nil
	}
	diskNewer := !updatedAt.IsZero() && info.ModTime().After(updatedAt)
	if diskNewer && rerr == nil {
		conflictPath := path + ".conflict.md"
		if werr := os.WriteFile(conflictPath, content, exportFilePerm); werr != nil { //nolint:gosec // operator path
			return "", true, fmt.Errorf("write conflict %s: %w", conflictPath, werr)
		}
		return "conflict", true, nil
	}
	return "", false, nil
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
