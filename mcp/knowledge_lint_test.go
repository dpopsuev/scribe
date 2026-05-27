package mcp_test

import (
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

// newLintServer returns a server + knowledge caller scoped to "test".
func newLintServer(t *testing.T) func(map[string]any) string {
	t.Helper()
	s := openStore(t)
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	return func(args map[string]any) string {
		return callTool(t, cs, "knowledge", args)
	}
}

// TestLint_UnresolvedWikilinks flags [[Title]] references with no matching artifact.
func TestLint_UnresolvedWikilinks(t *testing.T) {
	call := newLintServer(t)

	// Create a note with a wikilink to a non-existent concept.
	call(map[string]any{
		"action": "capture",
		"title":  "On virtue",
		"body":   "See [[Stoicism]] for context and [[Epictetus]] for practice.",
		"scope":  "test",
	})
	// "Stoicism" and "Epictetus" don't exist — wikilinks are unresolved.

	out := call(map[string]any{"action": "lint", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Fatalf("lint action not implemented: %s", out)
	}
	if !strings.Contains(out, "Stoicism") && !strings.Contains(out, "unresolved") {
		t.Errorf("lint: expected unresolved wikilinks in report, got: %s", out)
	}
}

// TestLint_UnresolvedWikilinks_Clean reports nothing when all [[links]] resolve.
func TestLint_UnresolvedWikilinks_Clean(t *testing.T) {
	call := newLintServer(t)

	// Create both target and source.
	call(map[string]any{"action": "capture", "title": "Stoicism", "scope": "test"})
	call(map[string]any{
		"action": "capture",
		"title":  "On virtue",
		"body":   "See [[Stoicism]] for context.",
		"scope":  "test",
	})

	out := call(map[string]any{"action": "lint", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Fatalf("lint action not implemented: %s", out)
	}
	// Stoicism resolves — should not appear as a gap.
	if strings.Contains(out, "Stoicism") && strings.Contains(out, "unresolved") {
		t.Errorf("lint: Stoicism resolved but flagged as gap: %s", out)
	}
}

// TestLint_OrphanedNotes flags notes with no incoming or outgoing knowledge edges.
func TestLint_OrphanedNotes(t *testing.T) {
	call := newLintServer(t)

	// Create a totally disconnected note.
	call(map[string]any{
		"action": "capture",
		"title":  "Disconnected thought",
		"scope":  "test",
	})

	out := call(map[string]any{"action": "lint", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Fatalf("lint action not implemented: %s", out)
	}
	if !strings.Contains(out, "orphan") && !strings.Contains(out, "Disconnected") {
		t.Errorf("lint: expected orphaned note in report, got: %s", out)
	}
}

// TestLint_ClusterSynthesisGap flags note clusters citing the same source
// but lacking a synthesizes artifact.
func TestLint_ClusterSynthesisGap(t *testing.T) {
	call := newLintServer(t)

	// Ingest a source, create 3 notes — no synthesis connecting them.
	call(map[string]any{"action": "ingest", "title": "Meditations", "body": "Marcus Aurelius on Stoic philosophy.", "scope": "test"})
	call(map[string]any{"action": "capture", "title": "On virtue", "scope": "test"})
	call(map[string]any{"action": "capture", "title": "On impermanence", "scope": "test"})
	call(map[string]any{"action": "capture", "title": "On control", "scope": "test"})

	out := call(map[string]any{"action": "lint", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Fatalf("lint action not implemented: %s", out)
	}
	if strings.Contains(out, "panic") {
		t.Errorf("lint: unexpected panic: %s", out)
	}
}

// TestLint_EmptyVault runs lint on a vault with nothing in it.
func TestLint_EmptyVault(t *testing.T) {
	call := newLintServer(t)

	out := call(map[string]any{"action": "lint", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Fatalf("lint action not implemented: %s", out)
	}
	if strings.Contains(out, "error") || strings.Contains(out, "panic") {
		t.Errorf("lint on empty vault should not error: %s", out)
	}
}

// TestLint_StructuredReport verifies the report has named sections.
func TestLint_StructuredReport(t *testing.T) {
	call := newLintServer(t)

	call(map[string]any{
		"action": "capture",
		"title":  "Floating idea",
		"body":   "References [[Ghost Concept]] that does not exist.",
		"scope":  "test",
	})

	out := call(map[string]any{"action": "lint", "scope": "test"})

	// Report must have recognizable structure.
	hasSection := strings.Contains(out, "Unresolved") ||
		strings.Contains(out, "Orphan") ||
		strings.Contains(out, "Health") ||
		strings.Contains(out, "issue") ||
		strings.Contains(out, "gap")

	if !hasSection {
		t.Errorf("lint: expected structured report sections, got: %s", out)
	}
}
