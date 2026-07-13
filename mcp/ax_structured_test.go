package mcp_test

import (
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCreate_StructuredContent_HasIDs(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result := callToolRaw(t, cs, "artifact", map[string]any{
		"action": "create",
		"artifacts": []any{
			map[string]any{"kind": "effort.goal", "title": "SC-Parent", "scope": "test"},
			map[string]any{"kind": "effort.task", "title": "SC-Child", "scope": "test", "parent": "$0",
				"priority": "high",
				"sections": []map[string]string{{"name": "context", "text": "x"}},
			},
		},
	})
	if result.StructuredContent == nil {
		t.Fatal("expected StructuredContent on create")
	}
	m, ok := result.StructuredContent.(map[string]any)
	if !ok {
		// Direct struct if no re-marshal
		t.Logf("StructuredContent type=%T val=%v", result.StructuredContent, result.StructuredContent)
		return
	}
	arts, _ := m["artifacts"].([]any)
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts in structured content, got %#v", m)
	}
	first, _ := arts[0].(map[string]any)
	if id, _ := first["id"].(string); id == "" || strings.HasPrefix(id, "$") {
		t.Fatalf("expected real id in structured content, got %#v", first)
	}
}

func TestCreate_DryRun_StructuredPlan(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result := callToolRaw(t, cs, "artifact", map[string]any{
		"action":  "create",
		"dry_run": true,
		"artifacts": []any{
			map[string]any{"kind": "effort.campaign", "title": "PlanCamp", "scope": "test"},
		},
	})
	if result.StructuredContent == nil {
		t.Fatal("expected StructuredContent on dry_run create")
	}
	var text string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text = tc.Text
		}
	}
	if !strings.Contains(text, "plan:") {
		t.Fatalf("expected plan text, got %q", text)
	}
}
