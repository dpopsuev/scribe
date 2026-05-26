package mcp_test

import (
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

// newKnowledgeServer returns a server and a convenience caller for the
// knowledge tool, scoped to "test".
func newKnowledgeServer(t *testing.T) func(map[string]any) string {
	t.Helper()
	s := openStore(t)
	srv, _ := scribemcp.NewServer(
		s,
		[]string{"test"},
		nil,
		parchment.ProtocolConfig{},
		"test",
	)
	cs := connectClient(t, srv)
	return func(args map[string]any) string {
		return callTool(t, cs, "knowledge", args)
	}
}

// TestKnowledge_Capture creates a fleeting note and verifies its structure.
func TestKnowledge_Capture(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{
		"action": "capture",
		"title":  "The dichotomy of control",
		"body":   "Focus only on what is in your power.",
		"scope":  "test",
	})

	if !strings.Contains(out, "NOT-") {
		t.Errorf("capture: expected NOT- prefix in response, got: %s", out)
	}
	if !strings.Contains(out, "fleeting") {
		t.Errorf("capture: expected status=fleeting in response, got: %s", out)
	}
	if !strings.Contains(out, "note") {
		t.Errorf("capture: expected kind=note in response, got: %s", out)
	}
}

// TestKnowledge_Capture_RequiresTitle verifies capture rejects empty title.
func TestKnowledge_Capture_RequiresTitle(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{
		"action": "capture",
		"scope":  "test",
	})

	if !strings.Contains(out, "title") {
		t.Errorf("capture: expected title-required error, got: %s", out)
	}
}

// TestKnowledge_Promote transitions a fleeting note to evergreen.
func TestKnowledge_Promote(t *testing.T) {
	call := newKnowledgeServer(t)

	// Create a fleeting note first.
	created := call(map[string]any{
		"action": "capture",
		"title":  "Stoicism is about virtue",
		"scope":  "test",
	})

	id := extractID(t, created)

	// Promote to evergreen.
	out := call(map[string]any{
		"action": "promote",
		"id":     id,
	})

	if !strings.Contains(out, "evergreen") {
		t.Errorf("promote: expected evergreen in response, got: %s", out)
	}
}

// TestKnowledge_Promote_RequiresID verifies promote rejects missing ID.
func TestKnowledge_Promote_RequiresID(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "promote"})

	if !strings.Contains(out, "id") {
		t.Errorf("promote: expected id-required error, got: %s", out)
	}
}

// TestKnowledge_Daily creates today's journal on first call, returns the
// same artifact on the second call (idempotent).
func TestKnowledge_Daily(t *testing.T) {
	call := newKnowledgeServer(t)

	today := time.Now().Format("2006-01-02")

	out1 := call(map[string]any{"action": "daily", "scope": "test"})

	if !strings.Contains(out1, today) {
		t.Errorf("daily: expected today's date %s in response, got: %s", today, out1)
	}
	if !strings.Contains(out1, "journal") {
		t.Errorf("daily: expected kind=journal in response, got: %s", out1)
	}

	id1 := extractID(t, out1)

	// Second call — idempotent, must return the same artifact.
	out2 := call(map[string]any{"action": "daily", "scope": "test"})
	id2 := extractID(t, out2)

	if id1 != id2 {
		t.Errorf("daily: second call returned different artifact: %s vs %s", id1, id2)
	}
}

// TestKnowledge_Backlinks returns notes pointing TO a given artifact.
func TestKnowledge_Backlinks(t *testing.T) {
	call := newKnowledgeServer(t)

	// Create a target note.
	target := call(map[string]any{
		"action": "capture",
		"title":  "Virtue",
		"scope":  "test",
	})
	targetID := extractID(t, target)

	// Backlinks with no edges yet — must return empty without error.
	out := call(map[string]any{
		"action": "backlinks",
		"id":     targetID,
	})

	if strings.Contains(out, "unknown knowledge action") {
		t.Errorf("backlinks: action not implemented: %s", out)
	}
	if strings.Contains(out, "error") && strings.Contains(out, "backlinks") {
		t.Errorf("backlinks: unexpected error: %s", out)
	}
}

// TestKnowledge_Backlinks_RequiresID verifies backlinks rejects missing ID.
func TestKnowledge_Backlinks_RequiresID(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "backlinks"})

	if !strings.Contains(out, "id") {
		t.Errorf("backlinks: expected id-required error, got: %s", out)
	}
}

// TestKnowledge_UnknownAction returns a clear error mentioning the action name.
func TestKnowledge_UnknownAction(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "explode"})

	if !strings.Contains(out, "explode") {
		t.Errorf("unknown action: expected action name in error, got: %s", out)
	}
}
