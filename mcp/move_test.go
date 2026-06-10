package mcp_test

import (
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func TestArtifactMove_ReparentsAtomically(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	call := func(args map[string]any) string { return callTool(t, cs, "artifact", args) }
	gcall := func(args map[string]any) string { return callTool(t, cs, "artifact", args) }

	// Create parent A, parent B, and a child.
	parentA := qolExtractID(t, call(map[string]any{"action": "create", "kind": "goal", "title": "Parent A", "scope": "test", "status": "work.draft"}))
	parentB := qolExtractID(t, call(map[string]any{"action": "create", "kind": "goal", "title": "Parent B", "scope": "test", "status": "work.draft"}))
	child := qolExtractID(t, call(map[string]any{
		"action": "create", "kind": "task", "title": "Child task",
		"scope": "test", "status": "work.draft", "parent": parentA,
		"sections": []map[string]string{{"name": "context", "text": "x"}},
	}))

	// Re-parent via set(field=parent).
	out := call(map[string]any{"action": "set", "id": child, "field": "parent", "value": parentB})
	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("set(field=parent) should succeed, got: %s", out)
	}

	// Verify parent field updated.
	getOut := call(map[string]any{"action": "get", "id": child})
	if !strings.Contains(getOut, parentB) {
		t.Errorf("child should reference parentB after move, got: %s", getOut)
	}

	// Verify tree: parentA no longer has child, parentB does.
	treeA := gcall(map[string]any{"action": "get", "format": "tree", "id": parentA})
	if strings.Contains(treeA, "Child task") {
		t.Errorf("parentA tree should not contain child after move, got: %s", treeA)
	}
	treeB := gcall(map[string]any{"action": "get", "format": "tree", "id": parentB})
	if !strings.Contains(treeB, "Child task") {
		t.Errorf("parentB tree should contain child after move, got: %s", treeB)
	}
}
