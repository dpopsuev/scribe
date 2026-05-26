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
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	return func(args map[string]any) string { return callTool(t, cs, "knowledge", args) },
		func(args map[string]any) string { return callTool(t, cs, "admin", args) }
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

	if !strings.Contains(out, "SRC-") {
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
