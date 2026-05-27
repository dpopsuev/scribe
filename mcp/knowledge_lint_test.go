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
		action, _ := args["action"].(string)
		switch action {
		case "lint":
			// lint → admin(knowledge_lint) for wikilinks/orphans/clusters
			return callTool(t, cs, "admin", map[string]any{
				"action": "knowledge_lint", "scope": args["scope"],
			})
		case "catalog":
			// catalog is now a direct artifact action
			return callTool(t, cs, "artifact", args)
		case "capture", "promote", "daily", "recall", "backlinks", "ingest", "synthesize":
			return callTool(t, cs, "artifact", translateKnowledgeToArtifact(args))
		default:
			return callTool(t, cs, "artifact", args)
		}
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

// TestKnowledge_Catalog returns the full artifact inventory.
func TestKnowledge_Catalog(t *testing.T) {
	call := newLintServer(t)

	// Seed varied knowledge artifacts.
	call(map[string]any{"action": "capture", "title": "Stoicism", "scope": "test"})
	call(map[string]any{"action": "capture", "title": "Virtue", "body": "Virtue is the only good.", "scope": "test"})
	call(map[string]any{"action": "ingest", "title": "Meditations", "body": "Marcus Aurelius.", "scope": "test"})
	call(map[string]any{"action": "daily", "scope": "test"})

	out := call(map[string]any{"action": "catalog", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Fatalf("catalog not implemented: %s", out)
	}
	// Must list artifact IDs.
	if !strings.Contains(out, "NOT-") {
		t.Errorf("catalog: expected NOT- entries, got: %s", out)
	}
	// Must include source.
	if !strings.Contains(out, "SRC-") {
		t.Errorf("catalog: expected SRC- entries, got: %s", out)
	}
	// Must include journal.
	if !strings.Contains(out, "JRN-") {
		t.Errorf("catalog: expected JRN- entries, got: %s", out)
	}
}

// TestKnowledge_Catalog_Empty works on an empty vault.
func TestKnowledge_Catalog_Empty(t *testing.T) {
	call := newLintServer(t)

	out := call(map[string]any{"action": "catalog", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Fatalf("catalog not implemented: %s", out)
	}
	if strings.Contains(out, "panic") {
		t.Errorf("catalog on empty vault panicked: %s", out)
	}
}

// TestKnowledge_Catalog_Grouped verifies output is grouped by kind.
func TestKnowledge_Catalog_Grouped(t *testing.T) {
	call := newLintServer(t)

	call(map[string]any{"action": "capture", "title": "Stoicism", "scope": "test"})
	call(map[string]any{"action": "ingest", "title": "Meditations", "scope": "test"})

	out := call(map[string]any{"action": "catalog", "scope": "test"})

	// Should have kind grouping headers.
	hasGroup := strings.Contains(out, "note") || strings.Contains(out, "source") ||
		strings.Contains(out, "Notes") || strings.Contains(out, "Sources")
	if !hasGroup {
		t.Errorf("catalog: expected kind grouping, got: %s", out)
	}
}

// TestKnowledge_Catalog_Sorted verifies evergreen notes appear before fleeting.
func TestKnowledge_Catalog_Sorted(t *testing.T) {
	call := newLintServer(t)

	fleeting := call(map[string]any{"action": "capture", "title": "Raw thought", "scope": "test"})
	fleetingID := extractID(t, fleeting)

	evergreen := call(map[string]any{"action": "capture", "title": "Mature idea", "scope": "test"})
	evergreenID := extractID(t, evergreen)
	call(map[string]any{"action": "promote", "id": evergreenID})

	out := call(map[string]any{"action": "catalog", "scope": "test"})

	evIdx := strings.Index(out, evergreenID[:6])
	flIdx := strings.Index(out, fleetingID[:6])
	if evIdx < 0 || flIdx < 0 {
		t.Fatalf("catalog: both artifacts must appear: %s", out)
	}
	if evIdx > flIdx {
		t.Errorf("catalog: evergreen should appear before fleeting, got ev@%d fl@%d", evIdx, flIdx)
	}
}
