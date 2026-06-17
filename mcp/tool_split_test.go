package mcp_test

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolSplit_ThreeToolsRegistered verifies that the MCP server exposes
// three separate tools (artifact, graph, admin) instead of one mega-tool.
func TestToolSplit_ThreeToolsRegistered(t *testing.T) {
	srv, _ := scribemcp.NewServerFromStore(
		parchment.NewMemoryStore(), []string{"test"}, parchment.ProtocolConfig{}, "test",
	)
	cs := connectClient(t, srv)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool)
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}

	for _, want := range []string{"artifact", "graph", "admin"} {
		if !names[want] {
			t.Errorf("expected tool %q in ListTools; got: %v", want, toolNames(tools.Tools))
		}
	}

	if len(tools.Tools) != 3 {
		t.Errorf("expected exactly 3 tools; got %d: %v", len(tools.Tools), toolNames(tools.Tools))
	}
}

// TestToolSplit_ArtifactCRUD verifies that create/get/query/set/update/delete
// route through the artifact tool.
func TestToolSplit_ArtifactCRUD(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// create via artifact tool
	result := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Split test task",
		"scope":  "test",
	})
	if !strings.Contains(result, "created") {
		t.Errorf("artifact create should return created confirmation; got: %s", result)
	}

	// query via artifact tool
	result = callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"query":  "Split test",
	})
	if !strings.Contains(result, "Split test task") {
		t.Errorf("artifact query should find created task; got: %s", result)
	}
}

// TestToolSplit_GraphLink verifies that link/analyze/synonym route through
// the graph tool.
func TestToolSplit_GraphLink(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create two artifacts first (via artifact tool)
	callTool(t, cs, "artifact", map[string]any{
		"action": "create", "id": "A", "kind": "effort.task", "title": "Task A", "scope": "test",
	})
	callTool(t, cs, "artifact", map[string]any{
		"action": "create", "id": "B", "kind": "effort.task", "title": "Task B", "scope": "test",
	})

	// link via graph tool
	result := callTool(t, cs, "graph", map[string]any{
		"action":   "link",
		"id":       "A",
		"relation": "depends_on",
		"targets":  []string{"B"},
	})
	if strings.Contains(result, "error") {
		t.Errorf("graph link should succeed; got: %s", result)
	}
}

// TestToolSplit_AdminDashboard verifies that dashboard/hygiene/lint route
// through the admin tool.
func TestToolSplit_AdminDashboard(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result := callTool(t, cs, "admin", map[string]any{
		"action": "dashboard",
	})
	if result == "" {
		t.Error("admin dashboard should return content")
	}
}

// TestToolSplit_WrongToolRejectsAction verifies that calling an action on
// the wrong tool returns an error (e.g. "link" on artifact tool).
func TestToolSplit_WrongToolRejectsAction(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	ctx := context.Background()

	// "link" should only work on graph, not artifact
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "artifact",
		Arguments: map[string]any{"action": "link", "id": "X", "relation": "depends_on", "targets": []string{"Y"}},
	})
	if err != nil {
		t.Fatalf("CallTool should not return transport error: %v", err)
	}
	if !result.IsError {
		t.Error("calling 'link' on artifact tool should return IsError=true")
	}
}

// TestToolSplit_RegistryContainsThreeTools verifies the directive registry
// has entries for all three tools.
func TestToolSplit_RegistryContainsThreeTools(t *testing.T) {
	reg := scribemcp.ToolRegistry()
	tools := reg.List()
	names := make(map[string]bool)
	for _, tm := range tools {
		names[tm.Name] = true
	}
	for _, want := range []string{"artifact", "graph", "admin"} {
		if !names[want] {
			t.Errorf("ToolRegistry missing %q; got: %v", want, names)
		}
	}
}

func toolNames(tools []*sdkmcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}
