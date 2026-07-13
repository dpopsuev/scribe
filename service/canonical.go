//nolint:goconst,gocritic // Extra key literals; named results add noise
package service

import (
	"context"
	"os/exec"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// RelGovernedBy is the discovery alias for canonical architecture intent.
// Stored edges use decision -justifies-> subject; governed_by is the inverse view.
const RelGovernedBy = "governed_by"

// ResolveCanonicalDecision finds the governing architecture decision for an artifact.
// Prefers accepted decisions that justify the artifact (or its ancestors).
func ResolveCanonicalDecision(ctx context.Context, svc *Service, id string) (*parchment.Artifact, string) {
	seen := map[string]bool{}
	cur := id
	for cur != "" && !seen[cur] {
		seen[cur] = true
		if d, via := canonicalFromIncoming(ctx, svc, cur); d != nil {
			return d, via
		}
		parents, _ := svc.Proto.Neighbors(ctx, cur, parchment.RelParentOf, parchment.Incoming)
		if len(parents) == 0 {
			break
		}
		cur = parents[0].From
	}
	return nil, ""
}

func canonicalFromIncoming(ctx context.Context, svc *Service, id string) (*parchment.Artifact, string) {
	edges, _ := svc.Proto.Neighbors(ctx, id, parchment.RelJustifies, parchment.Incoming)
	var fallback *parchment.Artifact
	for _, e := range edges {
		src, err := svc.Proto.GetArtifact(ctx, e.From)
		if err != nil {
			continue
		}
		if src.Label(parchment.LabelPrefixKind) != "intent.decision" {
			continue
		}
		status := parchment.StatusFromLabels(src.Labels)
		if status == "decision.accepted" || hasCanonicalLabel(src) {
			return src, RelGovernedBy
		}
		if fallback == nil {
			fallback = src
		}
	}
	if fallback != nil {
		return fallback, RelGovernedBy
	}
	return nil, ""
}

func hasCanonicalLabel(art *parchment.Artifact) bool {
	for _, l := range art.Labels {
		if l == "role:canonical-architecture" || l == "canonical:architecture" {
			return true
		}
	}
	return false
}

// ExpandGovernedBy rewrites subject -governed_by-> decisions into decision -justifies-> subject edges.
func ExpandGovernedBy(from, relation string, targets []string) []EdgeRef {
	if relation != RelGovernedBy {
		out := make([]EdgeRef, 0, len(targets))
		for _, t := range targets {
			out = append(out, EdgeRef{From: from, Relation: relation, To: t})
		}
		return out
	}
	out := make([]EdgeRef, 0, len(targets))
	for _, decision := range targets {
		out = append(out, EdgeRef{From: decision, Relation: parchment.RelJustifies, To: from})
	}
	return out
}

// CaptureGitState observes local repository provenance (source=observed).
type GitState struct {
	Path     string `json:"path,omitempty"`
	Revision string `json:"revision,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Dirty    bool   `json:"dirty"`
	Source   string `json:"source"` // observed | asserted
}

// ObserveGitState runs git in dir. Returns nil when dir is not a git work tree.
func ObserveGitState(dir string) *GitState {
	if dir == "" {
		dir = "."
	}
	rev, err := gitOut(dir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	branch, _ := gitOut(dir, "rev-parse", "--abbrev-ref", "HEAD")
	top, _ := gitOut(dir, "rev-parse", "--show-toplevel")
	dirty := false
	if status, err := gitOut(dir, "status", "--porcelain"); err == nil && strings.TrimSpace(status) != "" {
		dirty = true
	}
	return &GitState{
		Path:     top,
		Revision: rev,
		Branch:   branch,
		Dirty:    dirty,
		Source:   "observed",
	}
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...) //nolint:gosec // fixed git binary; args are flags only
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// StampGitProvenance merges observed git state under Extra["repo"].
func StampGitProvenance(extra map[string]any, g *GitState) map[string]any {
	if g == nil {
		return extra
	}
	if extra == nil {
		extra = make(map[string]any)
	}
	repo := map[string]any{
		"source":   g.Source,
		"revision": g.Revision,
		"branch":   g.Branch,
		"dirty":    g.Dirty,
	}
	if g.Path != "" {
		repo["path"] = g.Path
	}
	extra["repo"] = repo
	return extra
}

// SanitizeExtraIDs coerces opaque numeric identifiers to strings so JSON
// round-trips do not emit scientific notation for GitHub run IDs, etc.
func SanitizeExtraIDs(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	out := make(map[string]any, len(extra))
	for k, v := range extra {
		out[k] = sanitizeValue(k, v)
	}
	return out
}

func sanitizeValue(key string, v any) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, vv := range t {
			m[k] = sanitizeValue(k, vv)
		}
		return m
	case []any:
		arr := make([]any, len(t))
		for i, vv := range t {
			arr[i] = sanitizeValue(key, vv)
		}
		return arr
	case float64:
		if looksLikeOpaqueID(key) && t == float64(int64(t)) {
			return formatIntString(int64(t))
		}
		return t
	default:
		return v
	}
}

func looksLikeOpaqueID(key string) bool {
	k := strings.ToLower(key)
	if strings.HasSuffix(k, "_id") || strings.HasSuffix(k, "_ids") || k == "id" || k == "ids" {
		return true
	}
	for _, needle := range []string{"run_id", "github", "commit", "jira", "launch", "build", "hash", "sha"} {
		if strings.Contains(k, needle) {
			return true
		}
	}
	return false
}

func formatIntString(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// EnrichWriteExtra applies string-safe ID coercion and optional observed git stamp.
func EnrichWriteExtra(extra map[string]any, gitDir string, stampGit bool) map[string]any {
	extra = SanitizeExtraIDs(extra)
	if stampGit {
		if _, ok := extra["repo"]; !ok {
			extra = StampGitProvenance(extra, ObserveGitState(gitDir))
		}
	}
	return extra
}
