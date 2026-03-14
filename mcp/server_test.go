package mcp_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	scribemcp "github.com/dpopsuev/scribe/mcp"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func openStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedArtifacts(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	for _, a := range []*model.Artifact{
		{ID: "TASK-2026-001", Kind: "task", Scope: "origami", Status: "draft", Title: "Origami A"},
		{ID: "TASK-2026-002", Kind: "task", Scope: "origami", Status: "draft", Title: "Origami B"},
		{ID: "TASK-2026-003", Kind: "task", Scope: "mos", Status: "draft", Title: "Mos A"},
		{ID: "TASK-2026-004", Kind: "task", Scope: "asterisk", Status: "draft", Title: "Asterisk A"},
	} {
		if err := s.Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
}

func connectClient(t *testing.T, srv *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	t1, t2 := sdkmcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func callTool(t *testing.T, cs *sdkmcp.ClientSession, name string, args map[string]any) string {
	t.Helper()
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func TestScopedList_HomeScopes(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"origami", "mos"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "list",
		"kind":   "task",
	})

	if !strings.Contains(text, "Origami A") {
		t.Error("expected origami artifact in scoped list")
	}
	if !strings.Contains(text, "Mos A") {
		t.Error("expected mos artifact in scoped list")
	}
	if strings.Contains(text, "Asterisk A") {
		t.Error("asterisk artifact leaked through home scope filter")
	}
}

func TestScopedList_ExplicitScope(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"origami"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "list",
		"kind":   "task",
		"scope":  "asterisk",
	})

	if !strings.Contains(text, "Asterisk A") {
		t.Error("explicit scope=asterisk should override home scopes")
	}
	if strings.Contains(text, "Origami") {
		t.Error("origami should not appear when explicit scope=asterisk")
	}
}

func TestScopedCreate_DefaultScope(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	srv, _ := scribemcp.NewServer(s, []string{"origami"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "Auto-scoped task",
	})

	if !strings.Contains(text, "origami") {
		t.Errorf("expected scope=origami in created artifact, got: %s", text)
	}

	arts, _ := s.List(ctx, model.Filter{Scope: "origami"})
	found := false
	for _, a := range arts {
		if a.Title == "Auto-scoped task" {
			found = true
			break
		}
	}
	if !found {
		t.Error("created artifact not found with scope=origami")
	}
}

func TestScopedList_HomeScoped_KindFilter(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"mos"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "list",
		"kind":   "task",
		"status": "draft",
	})

	if !strings.Contains(text, "Mos A") {
		t.Errorf("expected mos task in scoped list, got: %s", text)
	}
	if strings.Contains(text, "Origami") || strings.Contains(text, "Asterisk") {
		t.Error("list should be scoped to home scopes")
	}
}

func TestCrossScopeGet(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"mos"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "get",
		"id":     "TASK-2026-004",
	})

	if !strings.Contains(text, "Asterisk A") {
		t.Errorf("cross-scope get by ID should work, got: %s", text)
	}
}

func TestNoHomeScopes_ShowsAll(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s)

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "list",
		"kind":   "task",
	})

	if !strings.Contains(text, "Origami A") || !strings.Contains(text, "Mos A") || !strings.Contains(text, "Asterisk A") {
		t.Errorf("no home scopes should show all artifacts, got: %s", text)
	}
}

func TestArtifactTree_MixedScope_ShowsLabels(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "SPR-1", Kind: "sprint", Scope: "origami", Status: "active", Title: "Sprint One"})
	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Scope: "origami", Status: "draft", Title: "Origami Work", Parent: "SPR-1"})
	_ = s.Put(ctx, &model.Artifact{ID: "TASK-2", Kind: "task", Scope: "scribe", Status: "draft", Title: "Scribe Work", Parent: "SPR-1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "graph", map[string]any{"action": "tree", "id": "SPR-1"})

	if !strings.Contains(text, "[origami]") {
		t.Errorf("mixed-scope tree should show [origami] label, got:\n%s", text)
	}
	if !strings.Contains(text, "[scribe]") {
		t.Errorf("mixed-scope tree should show [scribe] label, got:\n%s", text)
	}
}

func TestArtifactTree_SingleScope_OmitsLabels(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "SPR-1", Kind: "sprint", Scope: "origami", Status: "active", Title: "Sprint One"})
	_ = s.Put(ctx, &model.Artifact{ID: "TASK-1", Kind: "task", Scope: "origami", Status: "draft", Title: "Work A", Parent: "SPR-1"})
	_ = s.Put(ctx, &model.Artifact{ID: "TASK-2", Kind: "task", Scope: "origami", Status: "draft", Title: "Work B", Parent: "SPR-1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "graph", map[string]any{"action": "tree", "id": "SPR-1"})

	if strings.Contains(text, "[origami]") {
		t.Errorf("single-scope tree should omit scope labels, got:\n%s", text)
	}
	if !strings.Contains(text, "Work A") || !strings.Contains(text, "Work B") {
		t.Errorf("tree should contain both children, got:\n%s", text)
	}
}

func TestBriefing_EdgeAwareOutput(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "CAM-1", Kind: "campaign", Scope: "go4", Status: "active", Title: "Gang of Four"})
	_ = s.Put(ctx, &model.Artifact{ID: "TSK-1", Kind: "task", Scope: "go4", Status: "draft", Title: "Remove MCP clients", Parent: "CAM-1"})
	_ = s.Put(ctx, &model.Artifact{ID: "BUG-1", Kind: "bug", Scope: "limes", Status: "draft", Title: "Hardcoded deps"})
	_ = s.AddEdge(ctx, model.Edge{From: "TSK-1", To: "BUG-1", Relation: "implements"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "graph", map[string]any{"action": "briefing", "id": "CAM-1"})

	if !strings.Contains(text, "[campaign|active]") {
		t.Errorf("briefing should show [kind|status] for root, got:\n%s", text)
	}
	if !strings.Contains(text, "parent_of ->") {
		t.Errorf("briefing should show edge label with arrow, got:\n%s", text)
	}
	if !strings.Contains(text, "[task|draft]") {
		t.Errorf("briefing should show [kind|status] for child, got:\n%s", text)
	}
	if !strings.Contains(text, "implements ->") {
		t.Errorf("briefing should show implements edge, got:\n%s", text)
	}
	if !strings.Contains(text, "[bug|draft]") {
		t.Errorf("briefing should show bug kind, got:\n%s", text)
	}
}

func TestBriefing_IncomingEdgeArrow(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "SPC-1", Kind: "spec", Scope: "scribe", Status: "draft", Title: "Spec"})
	_ = s.Put(ctx, &model.Artifact{ID: "TSK-1", Kind: "task", Scope: "scribe", Status: "draft", Title: "Task"})
	_ = s.AddEdge(ctx, model.Edge{From: "TSK-1", To: "SPC-1", Relation: "implements"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "graph", map[string]any{"action": "briefing", "id": "SPC-1"})

	if !strings.Contains(text, "implements <-") {
		t.Errorf("briefing should show incoming arrow for implements edge on spec root, got:\n%s", text)
	}
	if !strings.Contains(text, "[task|draft]") {
		t.Errorf("briefing should show task as child of spec, got:\n%s", text)
	}
}

func TestTree_EdgeLabelsShownWhenPresent(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &model.Artifact{ID: "TSK-1", Kind: "task", Scope: "scribe", Status: "draft", Title: "Task"})
	_ = s.Put(ctx, &model.Artifact{ID: "SPC-1", Kind: "spec", Scope: "scribe", Status: "draft", Title: "Spec"})
	_ = s.AddEdge(ctx, model.Edge{From: "TSK-1", To: "SPC-1", Relation: "implements"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "graph", map[string]any{
		"action":    "tree",
		"id":        "TSK-1",
		"relation":  "implements",
		"direction": "outgoing",
	})

	if !strings.Contains(text, "implements ->") {
		t.Errorf("tree with relation should show edge label, got:\n%s", text)
	}
}

// --- Template enforcement tests ---

func createMCPTemplate(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	err := s.Put(ctx, &model.Artifact{
		ID: "SCR-TPL-1", Kind: "template", Status: "active", Title: "Task Template", Scope: "test",
		Sections: []model.Section{
			{Name: "content", Text: "full raw template markdown"},
			{Name: "context", Text: "Background and motivation"},
			{Name: "checklist", Text: "Ordered steps for execution"},
			{Name: "acceptance", Text: "Given/When/Then criteria"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func createMCPRealisticTemplate(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	err := s.Put(ctx, &model.Artifact{
		ID: "TPL-2026-002", Kind: "template", Status: "active", Title: "Eight Section Template", Scope: "test",
		Sections: []model.Section{
			{Name: "content", Text: "full raw template markdown"},
			{Name: "overview", Text: "High-level summary"},
			{Name: "context", Text: "Background and motivation"},
			{Name: "requirements", Text: "Functional requirements"},
			{Name: "design", Text: "Architecture and design"},
			{Name: "implementation", Text: "Implementation details"},
			{Name: "testing", Text: "Test plan"},
			{Name: "deployment", Text: "Deployment strategy"},
			{Name: "acceptance", Text: "Acceptance criteria"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTemplate_MCPCreateWithZeroSections(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "spec",
			"title":  "Test Spec",
			"scope":  "test",
			"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
			// NO sections provided - this should FAIL
		},
	})

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error (IsError=true) when creating artifact with no sections but linked to template")
	}

	// Get error message from result content
	var errMsg string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			errMsg = tc.Text
			break
		}
	}

	if !strings.Contains(errMsg, "does not conform to template") {
		t.Errorf("error should mention template conformance, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "context") {
		t.Errorf("error should mention missing section 'context', got: %s", errMsg)
	}
}

func TestTemplate_MCPCreateWithAllSections(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
		"title":  "Test Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
		"sections": []map[string]string{
			{"name": "context", "text": "Background info"},
			{"name": "checklist", "text": "Steps to follow"},
			{"name": "acceptance", "text": "Acceptance criteria"},
		},
	})

	if !strings.Contains(text, "Test Spec") {
		t.Errorf("artifact should be created successfully, got: %s", text)
	}
	if !strings.Contains(text, "SPEC-") {
		t.Errorf("artifact ID should be present with SPEC prefix, got: %s", text)
	}
	if !strings.Contains(text, "context") || !strings.Contains(text, "checklist") || !strings.Contains(text, "acceptance") {
		t.Errorf("artifact should include all sections, got: %s", text)
	}
}

func TestTemplate_MCPCreateWithPartialSections(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "spec",
			"title":  "Test Spec",
			"scope":  "test",
			"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
			"sections": []map[string]string{
				{"name": "context", "text": "Background info"},
				// Missing checklist and acceptance
			},
		},
	})

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error (IsError=true) when creating artifact with partial sections")
	}

	// Get error message from result content
	var errMsg string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			errMsg = tc.Text
			break
		}
	}

	if !strings.Contains(errMsg, "does not conform to template") {
		t.Errorf("error should mention template conformance, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "checklist") {
		t.Errorf("error should mention missing section 'checklist', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "acceptance") {
		t.Errorf("error should mention missing section 'acceptance', got: %s", errMsg)
	}
}

func TestTemplate_MCPCreateWithRealisticTemplate(t *testing.T) {
	s := openStore(t)
	createMCPRealisticTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
		"title":  "Real World Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"TPL-2026-002"}},
		"sections": []map[string]string{
			{"name": "overview", "text": "High-level summary text"},
			{"name": "context", "text": "Background and motivation text"},
			{"name": "requirements", "text": "Functional requirements text"},
			{"name": "design", "text": "Architecture and design text"},
			{"name": "implementation", "text": "Implementation details text"},
			{"name": "testing", "text": "Test plan text"},
			{"name": "deployment", "text": "Deployment strategy text"},
			{"name": "acceptance", "text": "Acceptance criteria text"},
		},
	})

	if !strings.Contains(text, "Real World Spec") {
		t.Errorf("artifact should be created successfully with all 8 sections, got: %s", text)
	}
	if !strings.Contains(text, "SPEC-") {
		t.Errorf("artifact ID should be present with SPEC prefix, got: %s", text)
	}
	// Verify all 8 sections are present in the output
	sections := []string{"overview", "context", "requirements", "design", "implementation", "testing", "deployment", "acceptance"}
	for _, sec := range sections {
		if !strings.Contains(text, sec) {
			t.Errorf("artifact should include section %q, got: %s", sec, text)
		}
	}
}

func TestTemplate_MCPLinkSatisfiesBlocksMissingSections(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	createMCPTemplate(t, s)

	// Create artifact with incomplete sections (only 1 of 3 required)
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-2026-001", Kind: "spec", Status: "draft", Title: "Incomplete Spec", Scope: "test",
		Sections: []model.Section{
			{Name: "context", Text: "Background info"},
			// Missing checklist and acceptance
		},
	})

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	// Try to link to template via MCP - should fail
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "graph",
		Arguments: map[string]any{
			"action":   "link",
			"id":       "SPEC-2026-001",
			"relation": "satisfies",
			"targets":  []string{"SCR-TPL-1"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error (IsError=true) when linking to template with missing sections")
	}

	// Get error message from result content
	var errMsg string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			errMsg = tc.Text
			break
		}
	}

	if !strings.Contains(errMsg, "does not conform to template") {
		t.Errorf("error should mention template conformance, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "checklist") {
		t.Errorf("error should mention missing section 'checklist', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "acceptance") {
		t.Errorf("error should mention missing section 'acceptance', got: %s", errMsg)
	}
}

func TestTemplate_MCPLinkSatisfiesAllowsConformant(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	createMCPTemplate(t, s)

	// Create artifact with all required sections
	s.Put(ctx, &model.Artifact{
		ID: "SPEC-2026-002", Kind: "spec", Status: "draft", Title: "Complete Spec", Scope: "test",
		Sections: []model.Section{
			{Name: "context", Text: "Background info"},
			{Name: "checklist", Text: "Steps to follow"},
			{Name: "acceptance", Text: "Acceptance criteria"},
		},
	})

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	// Link to template via MCP - should succeed
	text := callTool(t, cs, "graph", map[string]any{
		"action":   "link",
		"id":       "SPEC-2026-002",
		"relation": "satisfies",
		"targets":  []string{"SCR-TPL-1"},
	})

	if !strings.Contains(text, "linked") {
		t.Errorf("expected successful link, got: %s", text)
	}
	if !strings.Contains(text, "SPEC-2026-002") {
		t.Errorf("expected source ID in result, got: %s", text)
	}
	if !strings.Contains(text, "SCR-TPL-1") {
		t.Errorf("expected target ID in result, got: %s", text)
	}

	// Verify link was added
	art, _ := s.Get(ctx, "SPEC-2026-002")
	if len(art.Links["satisfies"]) != 1 || art.Links["satisfies"][0] != "SCR-TPL-1" {
		t.Errorf("satisfies link not added, links: %+v", art.Links)
	}
}

// --- SCR-TSK-7: Bulk set_field ---

func TestBulkSetField(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		s.Put(ctx, &model.Artifact{
			ID: fmt.Sprintf("TASK-2026-%03d", i), Kind: "task", Scope: "test",
			Status: "draft", Title: fmt.Sprintf("Task %d", i),
		})
	}

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "set",
		"ids":    []any{"TASK-2026-001", "TASK-2026-002", "TASK-2026-003", "TASK-2026-004", "TASK-2026-005"},
		"field":  "priority",
		"value":  "high",
	})

	for i := 1; i <= 5; i++ {
		id := fmt.Sprintf("TASK-2026-%03d", i)
		if !strings.Contains(text, id+".priority = high") {
			t.Errorf("expected %s.priority = high in result, got: %s", id, text)
		}
		art, _ := s.Get(ctx, id)
		if art.Priority != "high" {
			t.Errorf("%s priority = %q, want high", id, art.Priority)
		}
	}
}

func TestBulkSetField_SingleID(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "set",
		"id":     "TASK-2026-001",
		"field":  "priority",
		"value":  "high",
	})
	if !strings.Contains(text, "TASK-2026-001.priority = high") {
		t.Errorf("single id backward compat failed: %s", text)
	}
}

// --- SCR-TSK-8: Batch attach_sections ---

func TestBatchAttachSections(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{ID: "SPEC-2026-001", Kind: "spec", Scope: "test", Status: "draft", Title: "S1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "attach_section",
		"id":     "SPEC-2026-001",
		"sections": []any{
			map[string]any{"name": "problem", "text": "The problem statement"},
			map[string]any{"name": "decision", "text": "The decision"},
			map[string]any{"name": "acceptance", "text": "The criteria"},
		},
	})

	if !strings.Contains(text, "3 sections added") {
		t.Errorf("expected '3 sections added', got: %s", text)
	}

	art, _ := s.Get(ctx, "SPEC-2026-001")
	if len(art.Sections) != 3 {
		t.Errorf("expected 3 sections, got %d", len(art.Sections))
	}
}

func TestBatchAttachSections_SingleFallback(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{ID: "SPEC-2026-001", Kind: "spec", Scope: "test", Status: "draft", Title: "S1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "attach_section",
		"id":     "SPEC-2026-001",
		"name":   "problem",
		"text":   "Single section",
	})
	if !strings.Contains(text, "section \"problem\" added") {
		t.Errorf("single attach backward compat failed: %s", text)
	}
}

// --- SCR-TSK-14: Batch create ---

func TestBatchCreate(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "batch_create",
		"artifacts": []any{
			map[string]any{"kind": "task", "title": "Batch Task 1", "scope": "test"},
			map[string]any{"kind": "task", "title": "Batch Task 2", "scope": "test"},
			map[string]any{"kind": "task", "title": "Batch Task 3", "scope": "test"},
		},
	})

	if !strings.Contains(text, "created 3 artifacts") {
		t.Errorf("expected 'created 3 artifacts', got: %s", text)
	}
	if !strings.Contains(text, "Batch Task 1") || !strings.Contains(text, "Batch Task 3") {
		t.Errorf("missing task titles in result: %s", text)
	}
}

func TestBatchCreate_IntraBatchParent(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "batch_create",
		"artifacts": []any{
			map[string]any{"kind": "goal", "title": "Parent Goal", "scope": "test"},
			map[string]any{"kind": "task", "title": "Child Task", "scope": "test", "parent": "$0"},
		},
	})

	if !strings.Contains(text, "created 2 artifacts") {
		t.Errorf("expected 'created 2 artifacts', got: %s", text)
	}

	// Verify parent was resolved
	arts, _ := s.List(ctx, model.Filter{Kind: "task"})
	found := false
	for _, a := range arts {
		if a.Title == "Child Task" && a.Parent != "" && a.Parent != "$0" {
			found = true
		}
	}
	if !found {
		t.Error("child task parent reference was not resolved")
	}
}

// --- SCR-TSK-18: Multi-action update ---

func TestUpdate_FieldsAndSections(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action":   "update",
		"id":       "TASK-2026-001",
		"priority": "high",
		"sections": []any{
			map[string]any{"name": "notes", "text": "Started work"},
		},
	})

	if !strings.Contains(text, "priority = high") {
		t.Errorf("expected priority update in result: %s", text)
	}
	if !strings.Contains(text, "section \"notes\" added") {
		t.Errorf("expected section add in result: %s", text)
	}

	art, _ := s.Get(ctx, "TASK-2026-001")
	if art.Priority != "high" {
		t.Errorf("priority = %q, want high", art.Priority)
	}
	if len(art.Sections) != 1 || art.Sections[0].Name != "notes" {
		t.Errorf("expected notes section, got %+v", art.Sections)
	}
}

func TestUpdate_FieldsOnly(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1", Priority: "low"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action":   "update",
		"id":       "TASK-2026-001",
		"priority": "high",
		"title":    "Updated Title",
	})

	if !strings.Contains(text, "priority = high") || !strings.Contains(text, "title = Updated Title") {
		t.Errorf("expected both field updates: %s", text)
	}

	art, _ := s.Get(ctx, "TASK-2026-001")
	if art.Priority != "high" || art.Title != "Updated Title" {
		t.Errorf("fields not updated: priority=%q title=%q", art.Priority, art.Title)
	}
}

// --- SCR-TSK-13: Enriched motd ---

func TestMotd_ShowsOpenBugs(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{ID: "BUG-2026-001", Kind: "bug", Scope: "test", Status: "open", Title: "Critical bug", Priority: "critical"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "admin", map[string]any{"action": "motd"})

	if !strings.Contains(text, "Open Bugs") {
		t.Errorf("expected Open Bugs section: %s", text)
	}
	if !strings.Contains(text, "BUG-2026-001") {
		t.Errorf("expected bug ID in motd: %s", text)
	}
	if !strings.Contains(text, "[critical]") {
		t.Errorf("expected priority in bug listing: %s", text)
	}
}

func TestMotd_ShowsChangedSince(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	since := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "active", Title: "Recent task"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "admin", map[string]any{"action": "motd", "since": since})

	if !strings.Contains(text, "Changed Since") {
		t.Errorf("expected Changed Since section: %s", text)
	}
	if !strings.Contains(text, "TASK-2026-001") {
		t.Errorf("expected recent task in changes: %s", text)
	}
}

func TestMotd_ShowsActiveSummary(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "active", Title: "Active"})
	s.Put(ctx, &model.Artifact{ID: "TASK-2026-002", Kind: "task", Scope: "test", Status: "draft", Title: "Draft"})

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "admin", map[string]any{"action": "motd"})

	if !strings.Contains(text, "Active Work:") {
		t.Errorf("expected Active Work summary: %s", text)
	}
}
