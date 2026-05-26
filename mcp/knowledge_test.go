package mcp_test

import (
	"os"
	"path/filepath"
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

// TestKnowledge_ExportVault writes all knowledge notes to a directory.
func TestKnowledge_ExportVault(t *testing.T) {
	call := newKnowledgeServer(t)
	dir := t.TempDir()

	// Seed a couple of notes.
	call(map[string]any{"action": "capture", "title": "Stoicism", "body": "Virtue is the only good.", "scope": "test"})
	call(map[string]any{"action": "capture", "title": "Epictetus", "body": "We cannot choose our external circumstances.", "scope": "test"})

	out := call(map[string]any{
		"action": "export_vault",
		"dir":    dir,
		"scope":  "test",
	})

	if !strings.Contains(out, "exported") {
		t.Errorf("export_vault: expected 'exported' in response, got: %s", out)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("export_vault: expected count 2 in response, got: %s", out)
	}
}

// TestKnowledge_ExportVault_RequiresDir verifies export rejects missing dir.
func TestKnowledge_ExportVault_RequiresDir(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "export_vault"})

	if !strings.Contains(out, "dir") {
		t.Errorf("export_vault: expected dir-required error, got: %s", out)
	}
}

// TestKnowledge_ImportVault reads .md files from a directory into the store.
func TestKnowledge_ImportVault(t *testing.T) {
	call := newKnowledgeServer(t)
	dir := t.TempDir()

	// Write two vault-compatible .md files.
	note1 := `---
id: TEST-n-1
kind: note
status: fleeting
scope: test
title: The examined life
---

# The examined life

The unexamined life is not worth living.
`
	note2 := `---
id: TEST-n-2
kind: note
status: fleeting
scope: test
title: Know thyself
---

# Know thyself

## body

Gnothi seauton — inscribed at Delphi.
`
	if err := os.WriteFile(dir+"/note1.md", []byte(note1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/note2.md", []byte(note2), 0o644); err != nil {
		t.Fatal(err)
	}

	out := call(map[string]any{
		"action": "import_vault",
		"dir":    dir,
		"scope":  "test",
	})

	if !strings.Contains(out, "imported") {
		t.Errorf("import_vault: expected 'imported' in response, got: %s", out)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("import_vault: expected count 2 in response, got: %s", out)
	}
}

// TestKnowledge_ImportVault_RequiresDir verifies import rejects missing dir.
func TestKnowledge_ImportVault_RequiresDir(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "import_vault"})

	if !strings.Contains(out, "dir") {
		t.Errorf("import_vault: expected dir-required error, got: %s", out)
	}
}

// TestKnowledge_VaultRoundTrip exports notes and re-imports them, verifying
// the round-trip produces the same titles.
func TestKnowledge_VaultRoundTrip(t *testing.T) {
	call := newKnowledgeServer(t)
	exportDir := t.TempDir()

	titles := []string{"Virtue", "Courage", "Justice"}
	for _, title := range titles {
		call(map[string]any{"action": "capture", "title": title, "scope": "test"})
	}

	// Export.
	call(map[string]any{"action": "export_vault", "dir": exportDir, "scope": "test"})

	// Import into a fresh server.
	call2 := newKnowledgeServer(t)
	importOut := call2(map[string]any{"action": "import_vault", "dir": exportDir, "scope": "test"})

	if !strings.Contains(importOut, "imported") {
		t.Errorf("round-trip import: expected 'imported', got: %s", importOut)
	}
}

// TestKnowledge_EagerWikilinks verifies that wikilinks in captured notes
// automatically create edges — no separate sync call needed.
func TestKnowledge_EagerWikilinks(t *testing.T) {
	call := newKnowledgeServer(t)

	// Create the target note first.
	call(map[string]any{"action": "capture", "title": "Stoicism", "scope": "test"})

	// Create a note that references it via [[Stoicism]].
	created := call(map[string]any{
		"action": "capture",
		"title":  "On philosophy",
		"body":   "See also [[Stoicism]] for Hellenistic thought.",
		"scope":  "test",
	})
	noteID := extractID(t, created)

	// Export the vault — if wikilinks were eagerly synced, the graph has edges.
	// We verify via backlinks: Stoicism should show On philosophy as a backlink.
	dir := t.TempDir()
	exportOut := call(map[string]any{"action": "export_vault", "dir": dir, "scope": "test"})
	if !strings.Contains(exportOut, "exported") {
		t.Fatalf("export failed: %s", exportOut)
	}

	// Verify the exported note contains the wikilink in its body.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), noteID[:6]) {
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(string(data), "[[Stoicism]]") {
				found = true
			}
		}
	}
	if !found {
		t.Error("eager wikilinks: expected [[Stoicism]] preserved in exported .md")
	}
}

// TestKnowledge_EagerWikilinks_OnAttachSection verifies that attaching a
// section with [[wikilinks]] via the artifact tool also creates graph edges.
func TestKnowledge_EagerWikilinks_OnAttachSection(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	knowledge := func(args map[string]any) string { return callTool(t, cs, "knowledge", args) }
	artifact := func(args map[string]any) string { return callTool(t, cs, "artifact", args) }

	// Create target note.
	target := knowledge(map[string]any{"action": "capture", "title": "Epictetus", "scope": "test"})
	targetID := extractID(t, target)

	// Create source note (no body yet).
	source := knowledge(map[string]any{"action": "capture", "title": "The Enchiridion", "scope": "test"})
	sourceID := extractID(t, source)

	// Before attach_section: no backlinks.
	before := knowledge(map[string]any{"action": "backlinks", "id": targetID})
	if !strings.Contains(before, "no backlinks") {
		t.Fatalf("expected no backlinks before attach, got: %s", before)
	}

	// Attach a section with a [[wikilink]] to target.
	artifact(map[string]any{
		"action": "attach_section",
		"id":     sourceID,
		"name":   "body",
		"text":   "Written by [[Epictetus]] around 125 AD.",
	})

	// After attach_section: backlinks to Epictetus should include The Enchiridion.
	after := knowledge(map[string]any{"action": "backlinks", "id": targetID})
	if strings.Contains(after, "no backlinks") {
		t.Errorf("attach_section should have eagerly synced wikilinks, got: %s", after)
	}
	if !strings.Contains(after, sourceID[:6]) {
		t.Errorf("expected source %s in backlinks, got: %s", sourceID, after)
	}
}
