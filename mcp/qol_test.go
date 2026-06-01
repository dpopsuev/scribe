package mcp_test

import (
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newQoLSetup(t *testing.T) func(args map[string]any) string {
	t.Helper()
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	return func(args map[string]any) string { return callTool(t, cs, "artifact", args) }
}

// --- list_sections ---

func TestListSections_ReturnsSectionNamesOnly(t *testing.T) {
	call := newQoLSetup(t)

	out := call(map[string]any{
		"action": "create", "kind": "task", "title": "sectioned",
		"scope": "test", "status": "draft",
		"sections": []map[string]string{
			{"name": "context", "text": "the full context text"},
			{"name": "checklist", "text": "- [ ] item one"},
		},
	})
	id := qolExtractID(t, out)

	// list_sections → get with id=X; get response contains section names.
	result := call(map[string]any{"action": "get", "id": id})

	if !strings.Contains(result, "context") {
		t.Errorf("list_sections: want 'context', got: %s", result)
	}
	if !strings.Contains(result, "checklist") {
		t.Errorf("list_sections: want 'checklist', got: %s", result)
	}
}

func TestListSections_EmptySections(t *testing.T) {
	call := newQoLSetup(t)

	out := call(map[string]any{
		"action": "create", "kind": "task", "title": "no sections",
		"scope": "test", "status": "draft",
	})
	id := qolExtractID(t, out)

	// list_sections → get with id=X; should succeed even with no sections.
	result := call(map[string]any{"action": "get", "id": id})
	if strings.Contains(result, "error") || strings.Contains(result, "Error") {
		t.Errorf("get on artifact with no sections: unexpected error: %s", result)
	}
}

// --- search_sections ---

func TestSearchSections_FindsTextInSection(t *testing.T) {
	call := newQoLSetup(t)

	call(map[string]any{
		"action": "create", "kind": "task", "title": "has the phrase",
		"scope": "test", "status": "draft",
		"sections": []map[string]string{{"name": "context", "text": "unique phrase xyz987"}},
	})
	call(map[string]any{
		"action": "create", "kind": "task", "title": "does not have it",
		"scope": "test", "status": "draft",
		"sections": []map[string]string{{"name": "context", "text": "completely different content"}},
	})

	// search_sections → list with query=
	result := call(map[string]any{"action": "list", "scope": "test", "query": "xyz987"})
	if !strings.Contains(result, "has the phrase") {
		t.Errorf("search_sections: expected to find matching artifact, got: %s", result)
	}
	if strings.Contains(result, "does not have it") {
		t.Errorf("search_sections: non-matching artifact should be excluded, got: %s", result)
	}
}

// --- title_contains ---

func TestTitleContains_FiltersToMatchingTitles(t *testing.T) {
	call := newQoLSetup(t)

	call(map[string]any{"action": "create", "kind": "task", "title": "Alpha implementation", "scope": "test", "status": "draft"})
	call(map[string]any{"action": "create", "kind": "task", "title": "Beta implementation", "scope": "test", "status": "draft"})
	call(map[string]any{"action": "create", "kind": "task", "title": "Alpha testing", "scope": "test", "status": "draft"})

	result := call(map[string]any{"action": "list", "scope": "test", "kind": "task", "title_contains": "Alpha"})
	if !strings.Contains(result, "Alpha implementation") {
		t.Errorf("title_contains=Alpha: want 'Alpha implementation', got: %s", result)
	}
	if !strings.Contains(result, "Alpha testing") {
		t.Errorf("title_contains=Alpha: want 'Alpha testing', got: %s", result)
	}
	if strings.Contains(result, "Beta") {
		t.Errorf("title_contains=Alpha: Beta should be excluded, got: %s", result)
	}
}

// --- batch_update ---

func TestBatchUpdate_SetsPriorityAcrossAll(t *testing.T) {
	call := newQoLSetup(t)

	id1 := qolExtractID(t, call(map[string]any{"action": "create", "kind": "task", "title": "BU one", "scope": "test", "status": "draft"}))
	id2 := qolExtractID(t, call(map[string]any{"action": "create", "kind": "task", "title": "BU two", "scope": "test", "status": "draft"}))
	id3 := qolExtractID(t, call(map[string]any{"action": "create", "kind": "task", "title": "BU three", "scope": "test", "status": "draft"}))

	// batch_update → update with ids + patch
	result := call(map[string]any{
		"action": "update",
		"ids":    []string{id1, id2, id3},
		"patch":  map[string]string{"priority": "high"},
	})
	t.Logf("update response: %s", result)

	for _, id := range []string{id1, id2, id3} {
		out := call(map[string]any{"action": "get", "id": id})
		if !strings.Contains(out, "high") {
			t.Errorf("%s: expected priority=high after update, got: %s", id, out)
		}
	}
}

func TestBatchUpdate_SetsStatusWithForce(t *testing.T) {
	call := newQoLSetup(t)

	id1 := qolExtractID(t, call(map[string]any{"action": "create", "kind": "task", "title": "BU status A", "scope": "test", "status": "draft"}))
	id2 := qolExtractID(t, call(map[string]any{"action": "create", "kind": "task", "title": "BU status B", "scope": "test", "status": "draft"}))

	// batch_update → update with ids + patch + force
	call(map[string]any{
		"action": "update",
		"ids":    []string{id1, id2},
		"patch":  map[string]string{"status": "active"},
		"force":  true,
	})

	for _, id := range []string{id1, id2} {
		out := call(map[string]any{"action": "get", "id": id})
		if !strings.Contains(out, "active") {
			t.Errorf("%s: expected status=active, got: %s", id, out)
		}
	}
}

// qolExtractID parses the artifact ID from a create response.
func qolExtractID(t *testing.T, createOut string) string {
	t.Helper()
	fields := strings.Fields(createOut)
	for i, f := range fields {
		if f == "created" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	t.Fatalf("cannot extract ID from create response: %q", createOut)
	return ""
}
