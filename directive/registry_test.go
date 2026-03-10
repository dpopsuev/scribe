package directive_test

import (
	"testing"

	"github.com/dpopsuev/scribe/mcp"
)

func TestToolRegistry_AllToolsRegistered(t *testing.T) {
	reg := mcp.ToolRegistry()
	tools := reg.List()

	if len(tools) != 23 {
		t.Fatalf("expected 23 tools, got %d", len(tools))
	}

	for _, tool := range tools {
		if tool.Name == "" {
			t.Error("tool has empty name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if len(tool.Keywords) == 0 {
			t.Errorf("tool %q has no keywords", tool.Name)
		}
		if len(tool.Categories) == 0 {
			t.Errorf("tool %q has no categories", tool.Name)
		}
	}
}

func TestToolRegistry_ByCategory(t *testing.T) {
	reg := mcp.ToolRegistry()

	crud := reg.ByCategory("crud")
	if len(crud) == 0 {
		t.Fatal("expected at least one tool in crud category")
	}

	for _, tool := range crud {
		found := false
		for _, c := range tool.Categories {
			if c == "crud" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool %q returned by ByCategory(crud) but lacks crud category", tool.Name)
		}
	}

	empty := reg.ByCategory("nonexistent")
	if len(empty) != 0 {
		t.Errorf("expected 0 tools for nonexistent category, got %d", len(empty))
	}
}
