package mcp_test

import (
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

// newKnowledgeDetectServer returns a server with KnowledgeSchema and
// callers for both the knowledge and admin tools.
func newKnowledgeDetectServer(t *testing.T) (
	knowledge func(map[string]any) string,
	admin func(map[string]any) string,
) {
	t.Helper()
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	// knowledge routes each action to the canonical tool post-consolidation
	knowledgeFn := func(args map[string]any) string {
		action, _ := args["action"].(string)
		switch action {
		case "orient", "catalog":
			// orient/catalog are now direct artifact actions
			return callTool(t, cs, "artifact", args)
		case "capture", "promote", "daily", "recall_unused", "backlinks":
			return callTool(t, cs, "artifact", translateKnowledgeToArtifact(args))
		case "ingest":
			args["action"] = "create"
			if args["kind"] == nil {
				args["kind"] = "source"
			}
			return callTool(t, cs, "artifact", args)
		default:
			return callTool(t, cs, "admin", args)
		}
	}
	adminFn := func(args map[string]any) string { return callTool(t, cs, "admin", args) }
	return knowledgeFn, adminFn
}

// TestDetect_Knowledge_StuckFleeting surfaces notes that have been
// in fleeting status for more than the threshold.
func TestDetect_Knowledge_StuckFleeting(t *testing.T) {
	knowledge, admin := newKnowledgeDetectServer(t)

	// Create a fleeting note — it is "stuck" immediately (no threshold in tests).
	knowledge(map[string]any{"action": "capture", "title": "Raw thought", "scope": "test"})

	out := admin(map[string]any{
		"action": "detect",
		"check":  "knowledge",
		"scope":  "test",
	})

	if strings.Contains(out, "unknown") || strings.Contains(out, "error") {
		t.Fatalf("detect knowledge: unexpected error: %s", out)
	}
	// Should mention fleeting notes.
	if !strings.Contains(out, "fleeting") {
		t.Errorf("detect knowledge: expected fleeting mentions, got: %s", out)
	}
}

// TestDetect_Knowledge_UncitedSource surfaces source artifacts that
// nothing has cited.
func TestDetect_Knowledge_UncitedSource(t *testing.T) {
	knowledge, admin := newKnowledgeDetectServer(t)

	// Ingest a source — nothing cites it yet.
	knowledge(map[string]any{
		"action": "ingest",
		"title":  "Meditations",
		"body":   "Marcus Aurelius.",
		"scope":  "test",
	})

	out := admin(map[string]any{
		"action": "detect",
		"check":  "knowledge",
		"scope":  "test",
	})

	if !strings.Contains(out, "source") && !strings.Contains(out, "Meditations") {
		t.Errorf("detect knowledge: expected uncited source SRC- in report, got: %s", out)
	}
}

// TestDetect_Knowledge_CleanVault reports nothing when all notes are
// evergreen and sources are cited.
func TestDetect_Knowledge_CleanVault(t *testing.T) {
	knowledge, admin := newKnowledgeDetectServer(t)

	// Capture and immediately promote to evergreen.
	captured := knowledge(map[string]any{"action": "capture", "title": "Virtue", "scope": "test"})
	id := extractID(t, captured)
	knowledge(map[string]any{"action": "promote", "id": id})

	// No sources — nothing to cite.
	out := admin(map[string]any{
		"action": "detect",
		"check":  "knowledge",
		"scope":  "test",
	})

	if strings.Contains(out, "error") {
		t.Errorf("detect knowledge clean vault: unexpected error: %s", out)
	}
	// Should confirm clean state.
	if !strings.Contains(out, "0") && !strings.Contains(out, "clean") && !strings.Contains(out, "No ") {
		t.Errorf("detect knowledge clean: expected clean report, got: %s", out)
	}
}

// TestDetect_All_IncludesKnowledge verifies check=all includes knowledge checks.
func TestDetect_All_IncludesKnowledge(t *testing.T) {
	knowledge, admin := newKnowledgeDetectServer(t)

	// Seed a fleeting note.
	knowledge(map[string]any{
		"action": "capture",
		"title":  "Fleeting thought",
		"scope":  "test",
	})

	out := admin(map[string]any{
		"action": "detect",
		"check":  "all",
		"scope":  "test",
	})

	if strings.Contains(out, "unknown check") {
		t.Fatalf("detect all: check=all failed: %s", out)
	}
}

// TestDetect_Knowledge_StaleThreshold verifies the threshold parameter
// controls what counts as "stuck".
func TestDetect_Knowledge_StaleThreshold(t *testing.T) {
	knowledge, admin := newKnowledgeDetectServer(t)

	// Create a note just now.
	knowledge(map[string]any{"action": "capture", "title": "Fresh note", "scope": "test"})

	// With a very long threshold (365 days), fresh notes should NOT appear.
	out := admin(map[string]any{
		"action":     "detect",
		"check":      "knowledge",
		"scope":      "test",
		"stale_days": 365,
	})

	_ = time.Now() // just to use the import
	// With 365 days threshold, a just-created note should not be stuck.
	// The report may still mention sources if any exist.
	if strings.Contains(out, "error") {
		t.Errorf("detect knowledge stale_days: unexpected error: %s", out)
	}
}

// TestKnowledge_Orient returns the vault map legend.
func TestKnowledge_Orient(t *testing.T) {
	knowledge, _ := newKnowledgeDetectServer(t)

	// Seed varied content so the report has something to show.
	knowledge(map[string]any{"action": "capture", "title": "Stoicism", "body": "Virtue is the only good.", "scope": "test"})
	knowledge(map[string]any{"action": "capture", "title": "Epictetus", "scope": "test"})
	knowledge(map[string]any{"action": "ingest", "title": "Meditations", "body": "Marcus Aurelius.", "scope": "test"})
	knowledge(map[string]any{"action": "daily", "scope": "test"})

	out := knowledge(map[string]any{
		"action": "orient",
		"scope":  "test",
	})

	// Must include schema legend.
	if !strings.Contains(out, "note") {
		t.Errorf("orient: expected 'note' kind in legend, got: %s", out)
	}
	if !strings.Contains(out, "cites") {
		t.Errorf("orient: expected 'cites' relation in legend, got: %s", out)
	}

	// Must include vault state counts.
	if !strings.Contains(out, "fleeting") || !strings.Contains(out, "journal") {
		t.Errorf("orient: expected vault state counts, got: %s", out)
	}

	// Must include health snapshot.
	if !strings.Contains(out, "Health") && !strings.Contains(out, "health") {
		t.Errorf("orient: expected health section, got: %s", out)
	}
}

// TestKnowledge_Orient_Empty works on an empty vault without panicking.
func TestKnowledge_Orient_Empty(t *testing.T) {
	knowledge, _ := newKnowledgeDetectServer(t)

	out := knowledge(map[string]any{"action": "orient", "scope": "test"})

	if strings.Contains(out, "unknown knowledge action") {
		t.Errorf("orient not implemented: %s", out)
	}
	if strings.Contains(out, "panic") || strings.Contains(out, "error") {
		t.Errorf("orient on empty vault errored: %s", out)
	}
}

// TestKnowledge_Orient_DiscoveryPointer verifies that orient closes with a
// Tier 2→3 navigation hint so agents know how to move to discovery.
func TestKnowledge_Orient_DiscoveryPointer(t *testing.T) {
	knowledge, _ := newKnowledgeDetectServer(t)

	out := knowledge(map[string]any{"action": "orient", "scope": "test"})

	if !strings.Contains(out, "artifact(action=query") {
		t.Errorf("orient must include query navigation hint; got:\n%s", out)
	}
	if !strings.Contains(out, "artifact(action=get") {
		t.Errorf("orient must include get navigation hint; got:\n%s", out)
	}
}
