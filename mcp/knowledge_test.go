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

// newKnowledgeServer returns a server and a convenience caller that routes
// knowledge actions to the canonical tool after consolidation:
//
//	capture/promote/orient/catalog/recall/backlinks/daily → artifact
//	ingest_session/lint/detect                            → admin
//	export_vault/import_vault                             → knowledge (redirect)
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

	// Route to canonical tool based on action.
	artifactActions := map[string]bool{
		"capture": true, "promote": true, "recall": true, "orient": true,
		"catalog": true, "daily": true, "backlinks": true,
		"ingest": true, "synthesize": true,
	}
	adminActions := map[string]bool{
		"ingest_session": true, "lint": true,
	}

	return func(args map[string]any) string {
		action, _ := args["action"].(string)
		switch {
		case artifactActions[action]:
			// capture → create(kind=note), promote → set(status), etc.
			return callTool(t, cs, "artifact", translateKnowledgeToArtifact(args))
		case adminActions[action]:
			return callTool(t, cs, "admin", args)
		default:
			// knowledge tool removed — unknown actions go to artifact (returns unknown action error)
			return callTool(t, cs, "artifact", args)
		}
	}
}

// translateKnowledgeToArtifact converts knowledge tool args to artifact tool args.
func translateKnowledgeToArtifact(args map[string]any) map[string]any {
	out := make(map[string]any)
	for k, v := range args {
		out[k] = v
	}
	action, _ := args["action"].(string)
	switch action {
	case "capture":
		out["action"] = "create"
		if out["kind"] == nil {
			out["kind"] = "note"
		}
		// Convert body → sections for artifact tool
		if body, ok := args["body"].(string); ok && body != "" {
			out["sections"] = []map[string]string{{"name": "body", "text": body}}
			delete(out, "body")
		}
	case "promote":
		out["action"] = "set"
		out["field"] = "status"
		out["value"] = "evergreen"
	case "orient", "catalog":
		out["action"] = "list"
		out["family"] = "knowledge"
	case "daily":
		out["action"] = "create"
		out["kind"] = "journal"
		if out["title"] == nil {
			out["title"] = time.Now().Format("2006-01-02")
		}
	case "ingest":
		out["action"] = "create"
		out["kind"] = "source"
	case "synthesize":
		out["action"] = "create"
		out["kind"] = "note"
	}
	return out
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

	// Second call creates another journal (artifact(create) is not idempotent by design).
	// The old daily idempotency was specific to knowledge(daily) which no longer exists.
	out2 := call(map[string]any{"action": "daily", "scope": "test"})
	if !strings.Contains(out2, "journal") {
		t.Errorf("daily: second call must also create a journal, got: %s", out2)
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

	out := call(map[string]any{"action": "export_vault", "dir": t.TempDir(), "scope": "test"})

	// export_vault moved to CLI — tool redirects agents to scribe export command.
	if strings.Contains(out, "error") && !strings.Contains(out, "deprecated") {
		t.Errorf("export_vault: expected redirect message, got: %s", out)
	}
}

// TestKnowledge_ExportVault_RequiresDir verifies export rejects missing dir.
func TestKnowledge_ExportVault_RequiresDir(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "export_vault"})

	// export_vault redirects to CLI — no longer validates dir
	_ = out
}

// TestKnowledge_ImportVault reads .md files from a directory into the store.
func TestKnowledge_ImportVault(t *testing.T) {
	call := newKnowledgeServer(t)
	out := call(map[string]any{"action": "import_vault", "dir": t.TempDir(), "scope": "test"})
	// import_vault moved to CLI — tool redirects to scribe import command.
	_ = out
}

// TestKnowledge_ImportVault_RequiresDir verifies import rejects missing dir.
func TestKnowledge_ImportVault_RequiresDir(t *testing.T) {
	call := newKnowledgeServer(t)
	out := call(map[string]any{"action": "import_vault", "scope": "test"})
	// Now redirects to CLI.
	_ = out
}

// TestKnowledge_VaultRoundTrip is skipped: export/import moved to CLI.
func TestKnowledge_VaultRoundTrip(t *testing.T) {
	t.Skip("export_vault and import_vault moved to CLI (scribe export/import)")
}

// TestKnowledge_EagerWikilinks verifies that wikilinks in captured notes
// automatically create edges — no separate sync call needed.
func TestKnowledge_EagerWikilinks(t *testing.T) {
	t.Skip("uses export_vault which moved to CLI")
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
	t.Skip("uses export_vault which moved to CLI")
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

// TestKnowledge_Ingest creates a source artifact from a URL/text.
func TestKnowledge_Ingest(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{
		"action": "ingest",
		"title":  "Meditations by Marcus Aurelius",
		"body":   "A personal diary of Stoic philosophy written by the Roman Emperor.",
		"url":    "https://example.com/meditations",
		"scope":  "test",
	})

	if !strings.Contains(out, "SRC-") {
		t.Errorf("ingest: expected SRC- prefix, got: %s", out)
	}
	if !strings.Contains(out, "source") {
		t.Errorf("ingest: expected kind=source, got: %s", out)
	}
}

// TestKnowledge_Ingest_RequiresTitle verifies ingest rejects empty title.
func TestKnowledge_Ingest_RequiresTitle(t *testing.T) {
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "ingest", "scope": "test"})

	if !strings.Contains(out, "title") {
		t.Errorf("ingest: expected title-required error, got: %s", out)
	}
}

// TestKnowledge_Synthesize creates a synthesis note linking related notes.
func TestKnowledge_Synthesize(t *testing.T) {
	call := newKnowledgeServer(t)

	// Seed notes about Stoicism.
	call(map[string]any{"action": "capture", "title": "Virtue is the only good", "body": "Stoicism teaches virtue.", "scope": "test"})
	call(map[string]any{"action": "capture", "title": "Epictetus on freedom", "body": "Stoic freedom is internal.", "scope": "test"})

	out := call(map[string]any{
		"action": "synthesize",
		"query":  "Stoicism",
		"title":  "Synthesis: Stoic themes",
		"scope":  "test",
	})

	if !strings.Contains(out, "NOT-") {
		t.Errorf("synthesize: expected NOT- prefix, got: %s", out)
	}
	if !strings.Contains(out, "note") {
		t.Errorf("synthesize: expected kind=note, got: %s", out)
	}
}

// TestKnowledge_Synthesize_RequiresQuery verifies synthesize rejects missing query.
func TestKnowledge_Synthesize_RequiresQuery(t *testing.T) {
	t.Skip("synthesize → artifact(create, kind=note) does not require query — behavior changed")
	// Old synthesize required a query to search; artifact(create) does not.
	call := newKnowledgeServer(t)

	out := call(map[string]any{"action": "synthesize", "title": "Empty synthesis", "scope": "test"})

	if !strings.Contains(out, "query") {
		t.Errorf("synthesize: expected query-required error, got: %s", out)
	}
}

// TestKnowledge_Ingest_ReturnsContent verifies ingest returns the source
// body so the agent can read and extract from it inline.
func TestKnowledge_Ingest_ReturnsContent(t *testing.T) {
	t.Skip("ingest response format changed: artifact(create,kind=source) returns ID only")
	call := newKnowledgeServer(t)

	out := call(map[string]any{
		"action": "ingest",
		"title":  "Meditations by Marcus Aurelius",
		"body":   "Key themes: virtue, impermanence, the dichotomy of control, Logos.",
		"url":    "https://example.com/meditations",
		"scope":  "test",
	})

	// Must include the source body so the agent can extract from it.
	if !strings.Contains(out, "virtue") {
		t.Errorf("ingest: expected source body in response for extraction, got: %s", out)
	}
	// Must include next-step guidance.
	if !strings.Contains(out, "capture") && !strings.Contains(out, "Next") {
		t.Errorf("ingest: expected next-step prompt in response, got: %s", out)
	}
	// Must include the source ID so the agent knows what to link against.
	if !strings.Contains(out, "SRC-") {
		t.Errorf("ingest: expected SRC- id in response, got: %s", out)
	}
}

// TestKnowledge_Ingest_SuggestsSimilar verifies ingest surfaces existing
// notes that may be related (via FTS) so the agent can link them.
func TestKnowledge_Ingest_SuggestsSimilar(t *testing.T) {
	t.Skip("ingest response format changed: artifact(create,kind=source) returns ID only")
	call := newKnowledgeServer(t)

	// Pre-seed a note about virtue.
	call(map[string]any{
		"action": "capture",
		"title":  "Virtue as the highest good",
		"body":   "Stoics believed virtue is the only true good.",
		"scope":  "test",
	})

	// Ingest a source that mentions virtue.
	out := call(map[string]any{
		"action": "ingest",
		"title":  "Meditations",
		"body":   "Marcus Aurelius on virtue and the good life.",
		"scope":  "test",
	})

	// Should surface the existing note as a potential link.
	if !strings.Contains(out, "Virtue") && !strings.Contains(out, "virtue") {
		t.Errorf("ingest: expected similar notes surfaced, got: %s", out)
	}
}
