package mcp_test

import (
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func TestBulkSectionUpdate_ReplacesInAllSections(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	call := func(args map[string]any) string { return callTool(t, cs, "artifact", args) }

	id := qolExtractID(t, call(map[string]any{
		"action": "create", "kind": "task", "title": "Replace test",
		"scope": "test", "status": "draft",
		"sections": []map[string]string{
			{"name": "context", "text": "old value in context"},
			{"name": "checklist", "text": "old value in checklist"},
		},
	}))

	out := call(map[string]any{
		"action": "update",
		"id":     id,
		"query":  "old value",
		"text":   "new value",
	})
	if strings.Contains(out, "error") || strings.Contains(out, "unknown") {
		t.Fatalf("bulk_section_update failed: %s", out)
	}

	getOut := call(map[string]any{"action": "get", "id": id})
	if !strings.Contains(getOut, "new value in context") {
		t.Errorf("context section: expected 'new value', got: %s", getOut)
	}
	if !strings.Contains(getOut, "new value in checklist") {
		t.Errorf("checklist section: expected 'new value', got: %s", getOut)
	}
	if strings.Contains(getOut, "old value") {
		t.Error("bulk_section_update: 'old value' should be replaced everywhere")
	}
}

func TestBulkSectionUpdate_NoMatchIsNoop(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	call := func(args map[string]any) string { return callTool(t, cs, "artifact", args) }

	id := qolExtractID(t, call(map[string]any{
		"action": "create", "kind": "task", "title": "Noop test",
		"scope": "test", "status": "draft",
		"sections": []map[string]string{{"name": "context", "text": "untouched content"}},
	}))

	out := call(map[string]any{
		"action": "update",
		"id":     id,
		"query":  "nonexistent phrase",
		"text":   "replacement",
	})
	// Should not error, should say 0 sections updated.
	if strings.Contains(out, "error") || strings.Contains(out, "unknown") {
		t.Fatalf("bulk_section_update with no match failed: %s", out)
	}

	getOut := call(map[string]any{"action": "get", "id": id})
	if !strings.Contains(getOut, "untouched content") {
		t.Error("content should be unchanged when query has no match")
	}
}
