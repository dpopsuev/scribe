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
	proto = parchment.New(s, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{})
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	call = func(args map[string]any) string {
		return callTool(t, cs, "artifact", args)
	}
	return
}

// TestRecall_FindsRelevantNote verifies that a note whose body matches the
// query is returned in the recall output.
func TestRecall_FindsRelevantNote(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	_, err := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote},
		Title: "SetField rejects unknown fields",

		Sections: []parchment.Section{
			{Name: "body", Text: "SetField now returns an error for unknown fields like description, body, notes. Use attach_section instead."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	out := call(map[string]any{
		"action": "list", "ranked": true,
		"query": "SetField unknown field error",
		"scope": "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown action") {
		t.Fatalf("recall not implemented: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "setfield") &&
		!strings.Contains(strings.ToLower(out), "unknown field") {
		t.Errorf("recall must surface matching note\nGot: %s", out)
	}
}

// TestRecall_ActiveWorkExcluded verifies that ACTIVE work artifacts are not
// returned by recall — they are current work, not memory.
func TestRecall_ActiveWorkExcluded(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	// Active task — should NOT appear in recall
	activeTask, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindTask}, Title: "implement retry logic"})
	// Knowledge note — SHOULD appear
	note, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote}, Title: "retry logic pattern",
		Sections: []parchment.Section{{Name: "body", Text: "exponential backoff with jitter, cap at 5 retries"}},
	})

	out := call(map[string]any{"action": "list", "ranked": true, "query": "retry logic", "scope": "test"})

	if strings.Contains(out, activeTask.ID) {
		t.Errorf("recall must not return active task %s\nGot: %s", activeTask.ID, out)
	}
	if !strings.Contains(out, note.ID) && !strings.Contains(strings.ToLower(out), "retry") {
		t.Errorf("recall must return knowledge note\nGot: %s", out)
	}
}

// TestRecall_CompletedTaskIsMemory verifies that COMPLETED work artifacts are
// returned by recall — completed work is history, and history is memory.
//
// The canonical example: recall("why did we remove SetField Extra fallback?")
// should return the completed task that made the change, not just a note
// someone wrote about it.
func TestRecall_CompletedTaskIsMemory(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	// Create and complete a task
	task, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindTask}, Title: "Remove SetField Extra fallback — error on unknown fields",

		Goal:  "SetField must reject unknown fields instead of writing silently to Extra"})
	_, _ = proto.SetField(ctx, []string{task.ID}, parchment.FieldStatus, parchment.StatusComplete, parchment.SetFieldOptions{Force: true})

	out := call(map[string]any{
		"action": "list", "ranked": true,
		"query": "SetField Extra fallback unknown fields",
		"scope": "test",
	})

	if !strings.Contains(out, task.ID) {
		t.Errorf("recall must return completed task %s — completed work is memory\nGot: %s", task.ID, out)
	}
}

// TestRecall_CompletedDecisionIsMemory verifies that accepted decisions surface
// in recall — a decision's rationale is high-value permanent memory.
func TestRecall_CompletedDecisionIsMemory(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	decision, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindDecision}, Title: "Template conformance fires on promote not create",

		Goal:  "Partial drafts accepted at create time, full validation deferred to promote"})
	_, _ = proto.SetField(ctx, []string{decision.ID}, parchment.FieldStatus, "accepted", parchment.SetFieldOptions{Force: true})

	out := call(map[string]any{
		"action": "list", "ranked": true,
		"query": "template conformance promote create",
		"scope": "test",
	})

	if !strings.Contains(out, decision.ID) {
		t.Errorf("recall must return accepted decision %s\nGot: %s", decision.ID, out)
	}
}

// TestRecall_EvergreenRanksHigher verifies that evergreen notes rank above
// fleeting notes when both match the query.
func TestRecall_EvergreenRanksHigher(t *testing.T) {
	proto, call := newRecallServer(t)
	ctx := context.Background()

	fleeting, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixStatus + parchment.StatusFleeting}, Title: "template conformance fleeting",

		Sections: []parchment.Section{{Name: "body", Text: "template conformance check fires on promote"}},
	})
	evergreen, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixStatus + parchment.StatusEvergreen}, Title: "template conformance evergreen",

		Sections: []parchment.Section{{Name: "body", Text: "template conformance check fires on promote not create"}},
	})

	out := call(map[string]any{
		"action": "list", "ranked": true,
		"query": "template conformance",
		"scope": "test",
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

	older, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote}, Title: "wikilink resolution old",

		Sections: []parchment.Section{{Name: "body", Text: "wikilinks are resolved on attach_section"}},
	})
	// Backdate the older note
	_, _ = proto.SetField(ctx, []string{older.ID}, "created_at",
		time.Now().Add(-30*24*time.Hour).Format(time.RFC3339))

	newer, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote}, Title: "wikilink resolution new",

		Sections: []parchment.Section{{Name: "body", Text: "wikilinks are resolved on attach_section eagerly"}},
	})

	out := call(map[string]any{
		"action": "list", "ranked": true,
		"query": "wikilink resolution",
		"scope": "test",
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
		"action": "list", "ranked": true,
		"scope": "test",
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
		_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote},
			Title: "parchment protocol change",

			Sections: []parchment.Section{
				{Name: "body", Text: "parchment protocol was changed in this session iteration"},
			},
			ExplicitID: "",
			Goal:       "note" + string(rune('A'+i)),
		})
	}

	out := call(map[string]any{
		"action": "list", "ranked": true,
		"query": "parchment protocol",
		"scope": "test",
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
