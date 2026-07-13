package mcp_test

import (
	"encoding/json"
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

func TestArtifactTool_NoOutputSchema(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if tool.Name != "artifact" {
			continue
		}
		if tool.OutputSchema != nil {
			t.Fatalf("artifact must not advertise OutputSchema (Cursor rejects polymorphic SC); got %#v", tool.OutputSchema)
		}
		return
	}
	t.Fatal("artifact tool not found")
}

func TestGet_TextOnlySucceeds(t *testing.T) {
	s := openStore(t)
	if err := s.Put(t.Context(), &parchment.Artifact{
		ID:     "get-sc-probe",
		Title:  "Get SC probe",
		Labels: []string{"kind:knowledge.note", "scope:test"},
	}); err != nil {
		t.Fatal(err)
	}
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	result := callToolRaw(t, cs, "artifact", map[string]any{
		"action": "get",
		"id":     "get-sc-probe",
		"format": "summary",
	})
	if result.IsError {
		t.Fatalf("get failed: %#v", result)
	}
	var text string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text = tc.Text
		}
	}
	if text == "" {
		t.Fatal("expected text content from get")
	}
}

func TestArtifactInputSchema_NoGraphOnlyFields(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	tools, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	for _, tool := range tools.Tools {
		if tool.Name != "artifact" {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		break
	}
	if schema == nil {
		t.Fatal("artifact tool missing")
	}
	props, _ := schema["properties"].(map[string]any)
	for _, banned := range []string{"targets", "edges", "alias", "term", "min_shared", "iterations", "old_target"} {
		if _, ok := props[banned]; ok {
			t.Errorf("artifact InputSchema still exposes graph-only field %q", banned)
		}
	}
	raw, _ := json.Marshal(schema)
	if len(raw) > 9000 {
		t.Fatalf("artifact InputSchema still too fat: %d bytes", len(raw))
	}
}
