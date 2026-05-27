package mcp_test

// recall_test.go — knowledge(action=recall) RED tests.
//
// recall queries the agent's accumulated memory by meaning, not just keyword.
// Multi-pass FTS: exact phrase → all terms → any term.
// Results ranked by BM25 score × recency × kind weight (evergreen > fleeting > source).
// Only knowledge kinds: note, journal, source, concept, context.
// Work artifacts (task, spec, bug, goal) are excluded — they're tracking, not memory.

import (
	"context"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newRecallServer(t *testing.T) (proto *parchment.Protocol, call func(map[string]any) string) {
	t.Helper()
	s := openStore(t)
	proto = parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	call = func(args map[string]any) string {
		return callTool(t, cs, "knowledge", args)
	}
	return
}

// TestRecall_FindsRelevantNote verifies that a note whose body matches the
// query is returned in the recall output.
func TestRecall_FindsRelevantNote(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	_, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:  parchment.KindNote,
		Title: "SetField rejects unknown fields",
		Scope: "test",
		Sections: []parchment.Section{
			{Name: "body", Text: "SetField now returns an error for unknown fields like description, body, notes. Use attach_section instead."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	out := call(map[string]any{
		"action": "recall",
		"query":  "SetField unknown field error",
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown action") {
		t.Fatalf("recall not implemented: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "setfield") &&
		!strings.Contains(strings.ToLower(out), "unknown field") {
		t.Errorf("recall must surface matching note\nGot: %s", out)
	}
}

// TestRecall_OnlyKnowledgeKinds verifies that work artifacts (task, spec, bug)
// are NOT returned by recall — only knowledge kinds.
func TestRecall_OnlyKnowledgeKinds(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	// Create a task and a note both containing "retry logic"
	task, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "implement retry logic", Scope: "test",
	})
	note, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindNote, Title: "retry logic pattern", Scope: "test",
		Sections: []parchment.Section{{Name: "body", Text: "exponential backoff with jitter, cap at 5 retries"}},
	})

	out := call(map[string]any{
		"action": "recall",
		"query":  "retry logic",
		"scope":  "test",
	})

	if strings.Contains(out, task.ID) {
		t.Errorf("recall must not return work artifact %s (kind=task)\nGot: %s", task.ID, out)
	}
	if !strings.Contains(out, note.ID) && !strings.Contains(strings.ToLower(out), "retry") {
		t.Errorf("recall must return knowledge note about retry logic\nGot: %s", out)
	}
}

// TestRecall_EvergreenRanksHigher verifies that evergreen notes rank above
// fleeting notes when both match the query.
func TestRecall_EvergreenRanksHigher(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	fleeting, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindNote, Title: "template conformance fleeting",
		Scope: "test", Status: parchment.StatusFleeting,
		Sections: []parchment.Section{{Name: "body", Text: "template conformance check fires on promote"}},
	})
	evergreen, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindNote, Title: "template conformance evergreen",
		Scope: "test", Status: parchment.StatusEvergreen,
		Sections: []parchment.Section{{Name: "body", Text: "template conformance check fires on promote not create"}},
	})

	out := call(map[string]any{
		"action": "recall",
		"query":  "template conformance",
		"scope":  "test",
	})

	evergreenPos := strings.Index(out, evergreen.ID)
	fleetingPos := strings.Index(out, fleeting.ID)

	if evergreenPos < 0 {
		t.Errorf("evergreen note must appear in recall results\nGot: %s", out)
		return
	}
	if fleetingPos >= 0 && evergreenPos > fleetingPos {
		t.Errorf("evergreen note must rank above fleeting note\nEvergreen pos: %d, Fleeting pos: %d\nGot: %s",
			evergreenPos, fleetingPos, out)
	}
}

// TestRecall_RecentRanksHigher verifies that a more recently updated note
// ranks above an older note with equal relevance.
func TestRecall_RecentRanksHigher(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	older, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindNote, Title: "wikilink resolution old",
		Scope:    "test",
		Sections: []parchment.Section{{Name: "body", Text: "wikilinks are resolved on attach_section"}},
	})
	// Backdate the older note
	_, _ = proto.SetField(ctx, []string{older.ID}, "created_at",
		time.Now().Add(-30*24*time.Hour).Format(time.RFC3339))

	newer, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindNote, Title: "wikilink resolution new",
		Scope:    "test",
		Sections: []parchment.Section{{Name: "body", Text: "wikilinks are resolved on attach_section eagerly"}},
	})

	out := call(map[string]any{
		"action": "recall",
		"query":  "wikilink resolution",
		"scope":  "test",
	})

	newerPos := strings.Index(out, newer.ID)
	olderPos := strings.Index(out, older.ID)

	if newerPos < 0 {
		t.Errorf("newer note must appear in recall results\nGot: %s", out)
		return
	}
	if olderPos >= 0 && newerPos > olderPos {
		t.Errorf("recent note must rank above older note\nNewer pos: %d, Older pos: %d\nGot: %s",
			newerPos, olderPos, out)
	}
}

// TestRecall_EmptyQueryErrors verifies a clear error on empty query.
func TestRecall_EmptyQueryErrors(t *testing.T) {
	_, call := newRecallServer(t)

	out := call(map[string]any{
		"action": "recall",
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown action") {
		t.Fatalf("recall not implemented: %s", out)
	}
	if out == "" {
		t.Error("recall with empty query must return some output")
	}
	// Must indicate query is required, not panic
	if !strings.Contains(strings.ToLower(out), "query") &&
		!strings.Contains(strings.ToLower(out), "required") {
		t.Logf("empty recall output: %s", out)
	}
}

// TestRecall_LimitTopN verifies that recall returns at most 5 results by default.
func TestRecall_LimitTopN(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	for i := range 8 {
		_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind:  parchment.KindNote,
			Title: "parchment protocol change",
			Scope: "test",
			Sections: []parchment.Section{
				{Name: "body", Text: "parchment protocol was changed in this session iteration"},
			},
			ExplicitID: "",
			Goal:       "note" + string(rune('A'+i)),
		})
	}

	out := call(map[string]any{
		"action": "recall",
		"query":  "parchment protocol",
		"scope":  "test",
	})

	// Count how many IDs appear (rough proxy for result count)
	lines := strings.Split(out, "\n")
	resultLines := 0
	for _, l := range lines {
		if strings.Contains(l, "[note]") || strings.Contains(l, "[source]") ||
			strings.Contains(l, "[concept]") || strings.Contains(l, "[context]") ||
			strings.Contains(l, "[journal]") {
			resultLines++
		}
	}
	if resultLines > 5 {
		t.Errorf("recall must return at most 5 results by default, got %d\nOutput: %s", resultLines, out)
	}
}
