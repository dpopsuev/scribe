package service

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"gopkg.in/yaml.v3"
)

// IngestPlaybook configures how a git repository is materialized into Scribe.
type IngestPlaybook struct {
	Version        int                     `yaml:"version"`
	Scope          string                  `yaml:"scope"`
	Sources        map[string]IngestSource `yaml:"sources"`
	TicketPatterns []IngestTicketPattern   `yaml:"ticket_patterns"`
}

// IngestSource defines a directory-to-kind mapping.
type IngestSource struct {
	Path string `yaml:"path"`
	Kind string `yaml:"kind"`
}

// IngestTicketPattern maps a regex to an external backend for ticket linkage.
type IngestTicketPattern struct {
	Regex   string `yaml:"regex"`
	Backend string `yaml:"backend"`
	re      *regexp.Regexp
}

// IngestResult summarizes a repo ingest operation.
type IngestResult struct {
	Scope      string   `json:"scope"`
	Files      int      `json:"files"`
	Commits    int      `json:"commits"`
	TicketRefs int      `json:"ticket_refs"`
	Artifacts  int      `json:"artifacts"`
	Edges      int      `json:"edges"`
	Errors     []string `json:"errors,omitempty"`
}

// LoadPlaybook reads a .scribe.yaml playbook from a repo root.
// Returns a default playbook if the file doesn't exist.
func LoadPlaybook(repoRoot string) (*IngestPlaybook, error) {
	path := filepath.Join(repoRoot, ".scribe.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied repo path
	if os.IsNotExist(err) {
		return defaultPlaybook(repoRoot), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read playbook: %w", err)
	}
	var pb IngestPlaybook
	if err := yaml.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("parse playbook: %w", err)
	}
	for i := range pb.TicketPatterns {
		re, err := regexp.Compile(pb.TicketPatterns[i].Regex)
		if err != nil {
			return nil, fmt.Errorf("invalid ticket pattern %q: %w", pb.TicketPatterns[i].Regex, err)
		}
		pb.TicketPatterns[i].re = re
	}
	if pb.Scope == "" {
		pb.Scope = filepath.Base(repoRoot)
	}
	return &pb, nil
}

func defaultPlaybook(repoRoot string) *IngestPlaybook {
	defaultTicketRe := regexp.MustCompile(`([A-Z][A-Z0-9]+-\d+)`)
	return &IngestPlaybook{
		Version: 1,
		Scope:   filepath.Base(repoRoot),
		Sources: map[string]IngestSource{
			"all": {Path: ".", Kind: "knowledge.note"}, //nolint:goconst // vocabulary data
		},
		TicketPatterns: []IngestTicketPattern{
			{Regex: `([A-Z][A-Z0-9]+-\d+)`, Backend: "jira", re: defaultTicketRe},
		},
	}
}

// RepoIngest materializes a git repository into Scribe's artifact graph.
func (s *Service) RepoIngest(ctx context.Context, repoRoot string) (*IngestResult, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repo path: %w", err)
	}

	pb, err := LoadPlaybook(abs)
	if err != nil {
		return nil, err
	}

	result := &IngestResult{Scope: pb.Scope}

	arts, fileCount, errs := ingestSources(ctx, abs, pb)
	result.Files = fileCount
	result.Errors = append(result.Errors, errs...)

	commits, ticketEdges, commitCount := ingestGitLog(ctx, abs, pb)
	result.Commits = commitCount
	result.TicketRefs = len(ticketEdges)

	arts = append(arts, commits...)

	store := s.Proto.Store()
	if len(arts) > 0 {
		putErrs := store.BulkPut(ctx, arts)
		for i, e := range putErrs {
			if e != nil && i < len(arts) {
				result.Errors = append(result.Errors, fmt.Sprintf("put %s: %s", arts[i].ID, e))
			}
		}
	}
	result.Artifacts = len(arts)

	edgeCount := 0
	for _, edge := range ticketEdges {
		if err := store.AddEdge(ctx, edge); err != nil {
			slog.DebugContext(ctx, "repo_ingest: edge failed", slog.String("from", edge.From), slog.String("to", edge.To), slog.Any("error", err)) //nolint:sloglint // debug-level, no constants
		} else {
			edgeCount++
		}
	}
	result.Edges = edgeCount

	slog.InfoContext(ctx, "repo_ingest: done",
		slog.String(logKeySyncScope, pb.Scope), slog.Int(logKeySyncCount, result.Files),
		slog.Int("commits", result.Commits), slog.Int("artifacts", result.Artifacts)) //nolint:sloglint // "commits"/"artifacts" have no LogKey constants
	return result, nil
}

func ingestSources(_ context.Context, repoRoot string, pb *IngestPlaybook) (arts []*parchment.Artifact, fileCount int, errs []string) {
	for name, src := range pb.Sources {
		srcPath := filepath.Join(repoRoot, src.Path)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			continue
		}

		_ = filepath.Walk(srcPath, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
				return nil
			}
			art, err := parchment.ParseMDFile(p)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %s", p, err))
				return nil
			}
			fileCount++

			rel, _ := filepath.Rel(repoRoot, p)
			if art.ID == "" {
				art.ID = syncDerivedID(rel)
			}
			if art.Label(parchment.LabelPrefixKind) == "" {
				art.Labels = append(art.Labels, parchment.LabelPrefixKind+src.Kind)
			}
			if art.Label(parchment.LabelPrefixScope) == "" {
				art.Labels = append(art.Labels, parchment.LabelPrefixScope+pb.Scope)
			}
			if art.Extra == nil {
				art.Extra = make(map[string]any)
			}
			art.Extra["ingest_source"] = name
			art.Extra["repo_path"] = rel
			art.Extra["repo_root"] = repoRoot

			arts = append(arts, art)
			return nil
		})
	}

	return arts, fileCount, errs
}

func ingestGitLog(_ context.Context, repoRoot string, pb *IngestPlaybook) ([]*parchment.Artifact, []parchment.Edge, int) {
	cmd := exec.Command("git", "log", "--format=%H|%an|%aI|%s", "--no-merges", "-200") //nolint:gosec // controlled args
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, 0
	}

	var arts []*parchment.Artifact
	var edges []parchment.Edge
	commitCount := 0

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 4)
		if len(parts) < 4 {
			continue
		}
		commitCount++
		hash, author, dateStr, subject := parts[0], parts[1], parts[2], parts[3]
		date, _ := time.Parse(time.RFC3339, dateStr)

		refs := extractTicketRefs(subject, pb.TicketPatterns)
		if len(refs) == 0 {
			continue
		}

		commitID := fmt.Sprintf("COMMIT-%s", hash[:8])
		art := &parchment.Artifact{
			ID:    commitID,
			Title: subject,
			Labels: []string{
				parchment.LabelPrefixKind + "knowledge.source",
				parchment.LabelPrefixScope + pb.Scope,
			},
			Extra: map[string]any{
				"commit_hash":   hash,
				"commit_author": author,
				"ticket_refs":   refs,
				"repo_root":     repoRoot,
			},
			CreatedAt: date,
			UpdatedAt: date,
		}
		arts = append(arts, art)

		for _, ref := range refs {
			edges = append(edges, parchment.Edge{
				From:     commitID,
				To:       ref,
				Relation: edgeImplements,
			})
		}
	}

	return arts, edges, commitCount
}

func extractTicketRefs(text string, patterns []IngestTicketPattern) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, p := range patterns {
		if p.re == nil {
			continue
		}
		for _, match := range p.re.FindAllStringSubmatch(text, -1) {
			if len(match) > 1 && !seen[match[1]] {
				seen[match[1]] = true
				refs = append(refs, match[1])
			}
		}
	}
	return refs
}
