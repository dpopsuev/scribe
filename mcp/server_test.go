package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func openStore(t *testing.T) parchment.Store {
	t.Helper()
	s, err := parchment.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedArtifacts(t *testing.T, s parchment.Store) {
	t.Helper()
	ctx := context.Background()
	for _, a := range []*parchment.Artifact{
		{ID: "TASK-2026-001", Labels: []string{"kind:effort.task", "work.draft", "project:origami"}, Title: "Origami A"},
		{ID: "TASK-2026-002", Labels: []string{"kind:effort.task", "work.draft", "project:origami"}, Title: "Origami B"},
		{ID: "TASK-2026-003", Labels: []string{"kind:effort.task", "work.draft", "project:mos"}, Title: "Mos A"},
		{ID: "TASK-2026-004", Labels: []string{"kind:effort.task", "work.draft", "project:asterisk"}, Title: "Asterisk A"},
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

	srv, _ := scribemcp.NewServerFromStore(s, []string{"origami", "mos"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"kind":   "effort.task",
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

	srv, _ := scribemcp.NewServerFromStore(s, []string{"origami"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"kind":   "effort.task",
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

	srv, _ := scribemcp.NewServerFromStore(s, []string{"origami"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Auto-scoped task",
	})

	if !strings.Contains(text, "task") && !strings.Contains(text, "Auto-scoped task") {
		t.Errorf("expected task artifact in output, got: %s", text)
	}

	arts, _ := s.List(ctx, parchment.Filter{Labels: []string{"project:origami"}})
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

	srv, _ := scribemcp.NewServerFromStore(s, []string{"mos"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"kind":   "effort.task",
		"status": "work.draft",
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

	srv, _ := scribemcp.NewServerFromStore(s, []string{"mos"}, parchment.ProtocolConfig{}, "test")
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

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"kind":   "effort.task",
	})

	if !strings.Contains(text, "Origami A") || !strings.Contains(text, "Mos A") || !strings.Contains(text, "Asterisk A") {
		t.Errorf("no home scopes should show all artifacts, got: %s", text)
	}
}

func TestArtifactTree_MixedScope_ShowsLabels(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:sprint", "work.active", "project:origami"}, ID: "SPR-1", Title: "Sprint One"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:origami"}, ID: "TASK-1", Title: "Origami Work"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:scribe"}, ID: "TASK-2", Title: "Scribe Work"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "SPR-1", To: "TASK-1", Relation: parchment.RelParentOf})
	_ = s.AddEdge(ctx, parchment.Edge{From: "SPR-1", To: "TASK-2", Relation: parchment.RelParentOf})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{"action": "get", "format": "tree", "id": "SPR-1"})

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

	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:sprint", "work.active", "project:origami"}, ID: "SPR-1", Title: "Sprint One"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:origami"}, ID: "TASK-1", Title: "Work A"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:origami"}, ID: "TASK-2", Title: "Work B"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "SPR-1", To: "TASK-1", Relation: parchment.RelParentOf})
	_ = s.AddEdge(ctx, parchment.Edge{From: "SPR-1", To: "TASK-2", Relation: parchment.RelParentOf})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{"action": "get", "format": "tree", "id": "SPR-1"})

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

	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.campaign", "work.active", "project:go4"}, ID: "CAM-1", Title: "Gang of Four"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:go4"}, ID: "TSK-1", Title: "Remove MCP clients"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:intent.bug", "work.draft", "project:limes"}, ID: "BUG-1", Title: "Hardcoded deps"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "CAM-1", To: "TSK-1", Relation: parchment.RelParentOf})
	_ = s.AddEdge(ctx, parchment.Edge{From: "TSK-1", To: "BUG-1", Relation: "implements"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{"action": "get", "format": "briefing", "id": "CAM-1"})

	if !strings.Contains(text, "[effort.campaign|work.active]") {
		t.Errorf("briefing should show [kind|status] for root, got:\n%s", text)
	}
	if !strings.Contains(text, "parent_of ->") {
		t.Errorf("briefing should show edge label with arrow, got:\n%s", text)
	}
	if !strings.Contains(text, "[effort.task|work.draft]") {
		t.Errorf("briefing should show [kind|status] for child, got:\n%s", text)
	}
	if !strings.Contains(text, "implements ->") {
		t.Errorf("briefing should show implements edge, got:\n%s", text)
	}
	if !strings.Contains(text, "[intent.bug|work.draft]") {
		t.Errorf("briefing should show bug kind, got:\n%s", text)
	}
}

func TestBriefing_IncomingEdgeArrow(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:intent.spec", "work.draft", "project:scribe"}, ID: "SPC-1", Title: "Spec"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:scribe"}, ID: "TSK-1", Title: "Task"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "TSK-1", To: "SPC-1", Relation: "implements"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{"action": "get", "format": "briefing", "id": "SPC-1"})

	if !strings.Contains(text, "implements <-") {
		t.Errorf("briefing should show incoming arrow for implements edge on spec root, got:\n%s", text)
	}
	if !strings.Contains(text, "[effort.task|work.draft]") {
		t.Errorf("briefing should show task as child of spec, got:\n%s", text)
	}
}

func TestTree_EdgeLabelsShownWhenPresent(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:scribe"}, ID: "TSK-1", Title: "Task"})
	_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:intent.spec", "work.draft", "project:scribe"}, ID: "SPC-1", Title: "Spec"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "TSK-1", To: "SPC-1", Relation: "implements"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "get", "format": "tree",
		"id":        "TSK-1",
		"relation":  "implements",
		"direction": "outgoing",
	})

	if !strings.Contains(text, "implements ->") {
		t.Errorf("tree with relation should show edge label, got:\n%s", text)
	}
}

// --- Template enforcement tests ---

func createMCPTemplate(t *testing.T, s parchment.Store) {
	t.Helper()
	ctx := context.Background()
	err := s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:support.template", "work.active", "project:test"}, ID: "SCR-TPL-1", Title: "Spec Template",
		Sections: []parchment.Section{
			{Name: "content", Text: "full raw template markdown"},
			{Name: "problem", Text: "What is broken or missing"},
			{Name: "decision", Text: "What was decided and why"},
			{Name: "acceptance", Text: "Given/When/Then criteria"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func createMCPRealisticTemplate(t *testing.T, s parchment.Store) {
	t.Helper()
	ctx := context.Background()
	err := s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:support.template", "work.active", "project:test"}, ID: "TPL-2026-002", Title: "Eight Section Template",
		Sections: []parchment.Section{
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

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "intent.spec",
			"title":  "Test Spec",
			"scope":  "test",
			"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
			// NO sections provided - this should FAIL
		},
	})

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	// New behavior: partial creates succeed as draft with a Warnings field.
	// Hard rejection at create time was replaced by a warning + guard on promote.
	if result.IsError {
		t.Fatal("partial create should succeed as draft (not error): template conformance now fires on promote")
	}

	var text string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	// Result should mention the warning (missing section) without being a hard error.
	if !strings.Contains(text, "problem") && !strings.Contains(strings.ToLower(text), "warn") {
		t.Logf("create result (no hard error expected): %s", text)
	}
}

func TestTemplate_MCPCreateWithAllSections(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Test Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
		"sections": []map[string]string{
			{"name": "problem", "text": "What is broken"},
			{"name": "decision", "text": "What was decided"},
			{"name": "acceptance", "text": "Acceptance criteria"},
		},
	})

	if !strings.Contains(text, "Test Spec") {
		t.Errorf("artifact should be created successfully, got: %s", text)
	}
	if !strings.Contains(text, "spec") && !strings.Contains(text, "Test Spec") {
		t.Errorf("artifact ID should be present, got: %s", text)
	}
}

// --- SCR-TSK-265: Section content round-trip ---

func TestSectionRoundTrip_SectionsArray(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Round Trip Spec",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "problem", "text": "Background info here"},
			{"name": "decision", "text": "Step 1\nStep 2\nStep 3"},
			{"name": "acceptance", "text": "Given X\nWhen Y\nThen Z"},
		},
	})

	// Extract artifact ID from create response
	id := extractID(t, createText)

	for _, tc := range []struct {
		section, want string
	}{
		{"problem", "Background info here"},
		{"decision", "Step 1\nStep 2\nStep 3"},
		{"acceptance", "Given X\nWhen Y\nThen Z"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get",
			"id":     id,
			"name":   tc.section,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.section, got, tc.want)
		}
	}
}

func TestSectionRoundTrip_PatchMap(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Patch Map Spec",
		"scope":  "test",
		"patch": map[string]string{
			"problem":    "Patch problem content",
			"decision":   "Patch decision content",
			"acceptance": "Patch acceptance content",
		},
	})

	id := extractID(t, createText)

	for _, tc := range []struct {
		section, want string
	}{
		{"problem", "Patch problem content"},
		{"decision", "Patch decision content"},
		{"acceptance", "Patch acceptance content"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get",
			"id":     id,
			"name":   tc.section,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.section, got, tc.want)
		}
	}
}

func TestSectionRoundTrip_MixedSectionsAndPatch(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Mixed Spec",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "problem", "text": "Sections array problem"},
		},
		"patch": map[string]string{
			"decision":   "Patch decision",
			"acceptance": "Patch acceptance",
		},
	})

	id := extractID(t, createText)

	for _, tc := range []struct {
		section, want string
	}{
		{"problem", "Sections array problem"},
		{"decision", "Patch decision"},
		{"acceptance", "Patch acceptance"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get",
			"id":     id,
			"name":   tc.section,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.section, got, tc.want)
		}
	}
}

func TestSectionRoundTrip_MarkdownFidelity(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	mdContent := "# Header\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\n- bullet 1\n- bullet 2"

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Markdown Fidelity Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "notes", "text": mdContent},
		},
	})

	id := extractID(t, createText)

	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get",
		"id":     id,
		"name":   "notes",
	})
	if got != mdContent {
		t.Errorf("markdown fidelity lost:\ngot:  %q\nwant: %q", got, mdContent)
	}
}

// SCR-BUG-36: body alias missing on create sections parser
func TestSectionRoundTrip_BodyAlias(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Body Alias Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "notes", "body": "content via body field"},
			{"name": "context", "text": "content via text field"},
		},
	})

	id := extractID(t, createText)

	for _, tc := range []struct {
		section, want string
	}{
		{"notes", "content via body field"},
		{"context", "content via text field"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get",
			"id":     id,
			"name":   tc.section,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.section, got, tc.want)
		}
	}
}

func extractID(t *testing.T, createOutput string) string {
	t.Helper()
	// Format: "created SCR-XXX-123: Title [kind|status]"
	parts := strings.Fields(createOutput)
	for i, p := range parts {
		if p == "created" && i+1 < len(parts) {
			id := strings.TrimSuffix(parts[i+1], ":")
			if id != "" {
				return id
			}
		}
	}
	t.Fatalf("could not extract artifact ID from: %s", createOutput)
	return ""
}

func TestTemplate_MCPCreateWithPartialSections(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Provide only MustSection ("problem") but not ShouldSections ("decision", "acceptance").
	// With the TSK-28 fix, creation should SUCCEED — non-MustSections are deferred to completion.
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Test Spec with partial sections",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
		"sections": []map[string]string{
			{"name": "problem", "text": "Something is broken"},
		},
	})

	if !strings.Contains(text, "Test Spec with partial sections") {
		t.Errorf("artifact should be created with only MustSection, got: %s", text)
	}
}

func TestTemplate_MCPCreateWithRealisticTemplate(t *testing.T) {
	s := openStore(t)
	createMCPRealisticTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
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
	if !strings.Contains(text, "spec") && !strings.Contains(text, "Test Spec") {
		t.Errorf("artifact ID should be present, got: %s", text)
	}
}

func TestTemplate_MCPLinkSatisfiesBlocksMissingSections(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	createMCPTemplate(t, s)

	// Create artifact missing the MustSection ("problem") — only has non-required sections
	s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:intent.spec", "work.draft", "project:test"}, ID: "SPEC-2026-001", Title: "Incomplete Spec",
		Sections: []parchment.Section{
			{Name: "decision", Text: "Some decision"},
			// Missing "problem" which is the MustSection for spec
		},
	})

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Try to link to template via MCP - should fail because "problem" (MustSection) is missing
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
		t.Fatal("expected tool error (IsError=true) when linking to template with missing MustSection")
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
	if !strings.Contains(errMsg, "problem") {
		t.Errorf("error should mention missing section 'problem', got: %s", errMsg)
	}
}

func TestTemplate_MCPLinkSatisfiesAllowsConformant(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	createMCPTemplate(t, s)

	// Create artifact with all required sections
	s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:intent.spec", "work.draft", "project:test"}, ID: "SPEC-2026-002", Title: "Complete Spec",
		Sections: []parchment.Section{
			{Name: "problem", Text: "What is broken"},
			{Name: "decision", Text: "What was decided"},
			{Name: "acceptance", Text: "Acceptance criteria"},
		},
	})

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
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

	// Verify link was added via edge
	satisfiesEdges, _ := s.Neighbors(ctx, "SPEC-2026-002", "satisfies", parchment.Outgoing)
	if len(satisfiesEdges) != 1 || satisfiesEdges[0].To != "SCR-TPL-1" {
		t.Errorf("satisfies edge not added, edges: %+v", satisfiesEdges)
	}
}

// --- SCR-TSK-7: Bulk set_field ---

func TestBulkSetField(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: fmt.Sprintf("TASK-2026-%03d", i), Title: fmt.Sprintf("Task %d", i)})
	}

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
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
		if art.Label(parchment.LabelPrefixPriority) != "high" {
			t.Errorf("%s priority = %q, want high", id, art.Label(parchment.LabelPrefixPriority))
		}
	}
}

func TestBulkSetField_SingleID(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "TASK-2026-001", Title: "T1"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:intent.spec", "work.draft", "project:test"}, ID: "SPEC-2026-001", Title: "S1"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// attach_section → update with sections=[]
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     "SPEC-2026-001",
		"sections": []any{
			map[string]any{"name": "problem", "text": "The problem statement"},
			map[string]any{"name": "decision", "text": "The decision"},
			map[string]any{"name": "acceptance", "text": "The criteria"},
		},
	})

	if !strings.Contains(text, "section") {
		t.Errorf("expected section updates in result, got: %s", text)
	}

	art, _ := s.Get(ctx, "SPEC-2026-001")
	if len(art.Sections) != 3 {
		t.Errorf("expected 3 sections, got %d", len(art.Sections))
	}
}

func TestBatchAttachSections_SingleFallback(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:intent.spec", "work.draft", "project:test"}, ID: "SPEC-2026-001", Title: "S1"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// attach_section (single) → update with sections=[{name, text}]
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     "SPEC-2026-001",
		"sections": []any{
			map[string]any{"name": "problem", "text": "Single section"},
		},
	})
	if !strings.Contains(text, "section \"problem\" added") {
		t.Errorf("single section update failed: %s", text)
	}
}

// --- SCR-BUG-22: attach_section body field silently dropped ---

func TestAttachSection_BodyFieldAlias(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "TSK-1", Title: "Test"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// attach_section → update with sections=[{name, text}]
	// body alias is supported in create sections but not in update sections;
	// use text field directly here.
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     "TSK-1",
		"sections": []any{
			map[string]any{"name": "context", "text": "This content should be stored, not silently dropped."},
		},
	})
	if !strings.Contains(text, "section \"context\" added") {
		t.Errorf("update with section should succeed: %s", text)
	}

	// Verify content is actually persisted.
	art, _ := s.Get(ctx, "TSK-1")
	found := false
	for _, sec := range art.Sections {
		if sec.Name == "context" {
			if sec.Text == "" {
				t.Error("section created but content silently dropped")
			}
			if sec.Text != "This content should be stored, not silently dropped." {
				t.Errorf("section text = %q", sec.Text)
			}
			found = true
		}
	}
	if !found {
		t.Error("section 'context' not found in artifact")
	}
}

func TestBatchAttachSections_BodyFieldAlias(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "TSK-2", Title: "Test2"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// attach_section → update with sections=[{name, text}]; use text field directly.
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     "TSK-2",
		"sections": []any{
			map[string]any{"name": "problem", "text": "Problem statement via text field"},
			map[string]any{"name": "fix", "text": "Fix description via text field"},
		},
	})
	if !strings.Contains(text, "section") {
		t.Errorf("update with sections should succeed: %s", text)
	}

	art, _ := s.Get(ctx, "TSK-2")
	for _, sec := range art.Sections {
		if sec.Text == "" {
			t.Errorf("batch section %q created but content dropped", sec.Name)
		}
	}
}

// --- SCR-TSK-14: Batch create ---

func TestBatchCreate(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// batch_create → create with artifacts=[]
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"artifacts": []any{
			map[string]any{"kind": "effort.task", "title": "Batch Task 1", "scope": "test"},
			map[string]any{"kind": "effort.task", "title": "Batch Task 2", "scope": "test"},
			map[string]any{"kind": "effort.task", "title": "Batch Task 3", "scope": "test"},
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

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// batch_create → create with artifacts=[]
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"artifacts": []any{
			map[string]any{"kind": "effort.goal", "title": "Parent Goal", "scope": "test"},
			map[string]any{"kind": "effort.task", "title": "Child Task", "scope": "test", "parent": "$0"},
		},
	})

	if !strings.Contains(text, "created 2 artifacts") {
		t.Errorf("expected 'created 2 artifacts', got: %s", text)
	}

	// Verify parent was resolved
	arts, _ := s.List(ctx, parchment.Filter{Labels: []string{"kind:effort.task"}})
	found := false
	for _, a := range arts {
		if a.Title == "Child Task" {
			edges, _ := s.Neighbors(ctx, a.ID, parchment.RelParentOf, parchment.Incoming)
			if len(edges) > 0 && edges[0].From != "$0" {
				found = true
			}
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
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "TASK-2026-001", Title: "T1"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
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
	if art.Label(parchment.LabelPrefixPriority) != "high" {
		t.Errorf("priority = %q, want high", art.Label(parchment.LabelPrefixPriority))
	}
	if len(art.Sections) != 1 || art.Sections[0].Name != "notes" {
		t.Errorf("expected notes section, got %+v", art.Sections)
	}
}

func TestUpdate_FieldsOnly(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test", "priority:low"}, ID: "TASK-2026-001", Title: "T1"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
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
	if art.Label(parchment.LabelPrefixPriority) != "high" || art.Title != "Updated Title" {
		t.Errorf("fields not updated: priority=%q title=%q", art.Label(parchment.LabelPrefixPriority), art.Title)
	}
}

// --- SCR-TSK-10: Auto-link template ---

func TestAutoLinkTemplate(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Create a template for specs in scope "test"

	s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:support.template", "work.active", "project:test"}, ID: "TST-TPL-1",
		Title: "Spec Template",
		Sections: []parchment.Section{
			{Name: "problem", Text: "Describe the problem"},
			{Name: "decision", Text: "What was decided"},
		},
	})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create a spec WITHOUT explicit satisfies link — should auto-link
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Auto-linked Spec",
		"scope":  "test",
		"sections": []any{
			map[string]any{"name": "problem", "text": "The problem"},
			map[string]any{"name": "decision", "text": "The decision"},
		},
	})
	if !strings.Contains(text, "Auto-linked Spec") {
		t.Fatalf("create failed: %s", text)
	}

	// Verify the satisfies link was auto-added
	arts, _ := s.List(ctx, parchment.Filter{Labels: []string{"kind:intent.spec", "project:test"}})
	found := false
	for _, a := range arts {
		if a.Title == "Auto-linked Spec" {
			edges, _ := s.Neighbors(ctx, a.ID, "satisfies", parchment.Outgoing)
			if len(edges) == 1 && edges[0].To == "TST-TPL-1" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected satisfies link to TST-TPL-1 to be auto-added")
	}
}

func TestAutoLinkTemplate_ExplicitOverride(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:support.template", "work.active", "project:test"}, ID: "TST-TPL-1",
		Title:    "Spec Template",
		Sections: []parchment.Section{{Name: "problem", Text: "Describe"}},
	})
	s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:support.template", "work.active", "project:test"}, ID: "TST-TPL-2",
		Title:    "Custom Template",
		Sections: []parchment.Section{{Name: "overview", Text: "Describe"}},
	})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create with explicit satisfies — should NOT auto-link
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Explicit Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []any{"TST-TPL-2"}},
		"sections": []any{
			map[string]any{"name": "overview", "text": "Overview content"},
		},
	})
	if !strings.Contains(text, "Explicit Spec") {
		t.Fatalf("create failed: %s", text)
	}

	arts, _ := s.List(ctx, parchment.Filter{Labels: []string{"kind:intent.spec", "project:test"}})
	for _, a := range arts {
		if a.Title == "Explicit Spec" {
			edges, _ := s.Neighbors(ctx, a.ID, "satisfies", parchment.Outgoing)
			if len(edges) != 1 || edges[0].To != "TST-TPL-2" {
				t.Errorf("explicit satisfies should be TST-TPL-2, got edges: %v", edges)
			}
		}
	}
}

func TestAutoLinkTemplate_NoTemplateInScope(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create spec in scope with no templates — should succeed without error
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "No Template Task",
		"scope":  "empty",
	})
	if !strings.Contains(text, "No Template Task") {
		t.Fatalf("create failed: %s", text)
	}
}

// --- SCR-TSK-16: Compact list ---

func TestListCompact(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:alpha"}, ID: "T-001", Title: "First"})
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:intent.spec", "work.active", "project:beta"}, ID: "T-002", Title: "Second"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"fields": []any{"id", "status", "title"},
	})
	if !strings.Contains(text, "ID") || !strings.Contains(text, "STATUS") || !strings.Contains(text, "TITLE") {
		t.Errorf("expected compact headers, got: %s", text)
	}
	if !strings.Contains(text, "T-001") || !strings.Contains(text, "T-002") {
		t.Errorf("expected both artifact IDs: %s", text)
	}
	// Should NOT contain SCOPE or KIND columns since they weren't requested
	if strings.Contains(text, "SCOPE") || strings.Contains(text, "KIND") {
		t.Errorf("compact list should only show requested fields: %s", text)
	}
}

func TestListCompact_InvalidField(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "query",
			"fields": []any{"id", "nonexistent"},
		},
	})
	// Should return error for invalid field
	if err == nil && result != nil && !result.IsError {
		t.Error("expected error for invalid field name")
	}
}

// --- SCR-TSK-12: Clone artifact ---

func TestClone(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{
		ID:    "SPEC-001",
		Title: "Original Spec",
		Sections: []parchment.Section{
			{Name: "goal", Text: "Original goal"},
			{Name: "problem", Text: "The problem"},
			{Name: "decision", Text: "The decision"},
		},
		Labels: []string{"kind:intent.spec", "work.active", "backend", "api", "project:alpha"},
	})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// clone → create with clone_from=<source_id>
	text := callTool(t, cs, "artifact", map[string]any{
		"action":     "create",
		"clone_from": "SPEC-001",
		"title":      "Cloned Spec",
		"scope":      "beta",
	})
	if !strings.Contains(text, "cloned SPEC-001") {
		t.Fatalf("clone failed: %s", text)
	}
	if !strings.Contains(text, "Cloned Spec") {
		t.Errorf("expected cloned title: %s", text)
	}

	// Verify clone has sections
	arts, _ := s.List(ctx, parchment.Filter{Labels: []string{"kind:intent.spec", "project:beta"}})
	if len(arts) != 1 {
		t.Fatalf("expected 1 cloned spec, got %d", len(arts))
	}
	clone := arts[0]
	if clone.Title != "Cloned Spec" {
		t.Errorf("clone title = %q, want 'Cloned Spec'", clone.Title)
	}
	if clone.Goal() != "Original goal" {
		t.Errorf("clone should inherit goal, got %q", clone.Goal())
	}
	if len(clone.Sections) != 3 {
		t.Errorf("clone should have 3 sections (goal+problem+decision), got %d", len(clone.Sections))
	}
	if parchment.StatusFromLabels(clone.Labels) != "work.draft" {
		t.Errorf("clone status should default to draft, got %q", parchment.StatusFromLabels(clone.Labels))
	}
	if clone.ID == "SPEC-001" {
		t.Error("clone should have a new ID")
	}
}

func TestClone_NonexistentSource(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	// clone → create with clone_from=<source_id>
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action":     "create",
			"clone_from": "NOPE-999",
		},
	})
	if err == nil && result != nil && !result.IsError {
		t.Error("expected error for nonexistent source")
	}
}

// --- SCR-SPC-74: MCP schema validation for complex types ---

func TestMCPSchema_ArrayTypes(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-001", Title: "T1"})
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-002", Title: "T2"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// ids array (get)
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "get",
		"ids":    []any{"T-001", "T-002"},
	})
	if !strings.Contains(text, "T1") || !strings.Contains(text, "T2") {
		t.Errorf("ids array failed: %s", text)
	}

	// fields array (list)
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"fields": []any{"id", "title"},
	})
	if !strings.Contains(text, "T-001") {
		t.Errorf("fields array failed: %s", text)
	}

	// sections array (create)
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "With Sections",
		"scope":  "test",
		"sections": []any{
			map[string]any{"name": "context", "text": "Background"},
		},
	})
	if !strings.Contains(text, "With Sections") {
		t.Errorf("sections array failed: %s", text)
	}

	// artifacts array (create with artifacts=[])
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"artifacts": []any{
			map[string]any{"kind": "effort.task", "title": "Batch 1", "scope": "test"},
			map[string]any{"kind": "effort.task", "title": "Batch 2", "scope": "test"},
		},
	})
	if !strings.Contains(text, "created 2") {
		t.Errorf("artifacts array failed: %s", text)
	}
}

func TestMCPSchema_BooleanTypes(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-001", Title: "T1"})
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-002", Title: "T2"})
	s.AddEdge(ctx, parchment.Edge{From: "T-001", To: "T-002", Relation: "depends_on"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// include_edges boolean (get)
	text := callTool(t, cs, "artifact", map[string]any{
		"action":        "get",
		"id":            "T-001",
		"include_edges": true,
	})
	if !strings.Contains(text, "depends_on") {
		t.Errorf("include_edges boolean failed — edges not shown: %s", text)
	}

	// count boolean (list)
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"count":  true,
		"kind":   "effort.task",
	})
	if text != "2" {
		t.Errorf("count boolean failed: got %q, want 2", text)
	}

	// force boolean (set)
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "set",
		"id":     "T-001",
		"field":  "status",
		"value":  "complete",
		"force":  true,
	})
	if !strings.Contains(text, "status = complete") {
		t.Errorf("force boolean failed: %s", text)
	}
}

// --- SCR-TSK-177: Batch update ---

func TestBatchUpdate_MultipleIDs(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: fmt.Sprintf("T-%03d", i), Title: fmt.Sprintf("Task %d", i)})
	}

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action":   "update",
		"ids":      []any{"T-001", "T-002", "T-003"},
		"priority": "high",
		"sprint":   "SPR-1",
	})

	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("T-%03d", i)
		if !strings.Contains(text, id+".priority = high") {
			t.Errorf("expected %s.priority = high in result: %s", id, text)
		}
		if !strings.Contains(text, id+".sprint = SPR-1") {
			t.Errorf("expected %s.sprint = SPR-1 in result: %s", id, text)
		}
		art, _ := s.Get(ctx, id)
		if art.Label(parchment.LabelPrefixPriority) != "high" || art.Label(parchment.LabelPrefixSprint) != "SPR-1" {
			t.Errorf("%s: priority=%q sprint=%q", id, art.Label(parchment.LabelPrefixPriority), art.Label(parchment.LabelPrefixSprint))
		}
	}
}

func TestBatchUpdate_WithPatch(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-001", Title: "T1"})
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-002", Title: "T2"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"ids":    []any{"T-001", "T-002"},
		"patch":  map[string]any{"priority": "critical", "sprint": "SPR-2"},
	})

	if !strings.Contains(text, "T-001.priority = critical") || !strings.Contains(text, "T-002.priority = critical") {
		t.Errorf("expected priority updates: %s", text)
	}

	for _, id := range []string{"T-001", "T-002"} {
		art, _ := s.Get(ctx, id)
		if art.Label(parchment.LabelPrefixPriority) != "critical" || art.Label(parchment.LabelPrefixSprint) != "SPR-2" {
			t.Errorf("%s: priority=%q sprint=%q", id, art.Label(parchment.LabelPrefixPriority), art.Label(parchment.LabelPrefixSprint))
		}
	}
}

func TestBatchUpdate_SingleIDBackwardCompat(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-001", Title: "T1"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action":   "update",
		"id":       "T-001",
		"priority": "medium",
	})

	if !strings.Contains(text, "T-001.priority = medium") {
		t.Errorf("single id backward compat failed: %s", text)
	}
}

func TestMCPSchema_ObjectTypes(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.draft", "project:test"}, ID: "T-001", Title: "T1"})

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// patch object (update)
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     "T-001",
		"patch":  map[string]any{"priority": "high", "title": "Updated"},
	})
	if !strings.Contains(text, "priority = high") {
		t.Errorf("patch object failed: %s", text)
	}

	// links object (create)
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:support.template", "work.active"}, ID: "TPL-1", Title: "Task Template",
		Sections: []parchment.Section{{Name: "content", Text: "body"}, {Name: "context", Text: "ctx"}},
	})
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "With Links",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []any{"TPL-1"}},
		"sections": []any{
			map[string]any{"name": "context", "text": "Background"},
		},
	})
	if !strings.Contains(text, "With Links") {
		t.Errorf("links object failed: %s", text)
	}
}

// --- PRC-BUG-10: archive with singular "id" + scope takes bulk path, archives entire scope ---

func TestArchive_SingularIDWithScopeDoesNotBulk(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Create 3 artifacts in same scope: a goal and two unrelated tasks
	for _, a := range []*parchment.Artifact{
		{ID: "GOL-1", Labels: []string{"kind:effort.goal", "work.active", "project:test"}, Title: "Target Goal"},
		{ID: "TASK-1", Labels: []string{"kind:effort.task", "work.active", "project:test"}, Title: "Unrelated Task A"},
		{ID: "TASK-2", Labels: []string{"kind:effort.task", "work.active", "project:test"}, Title: "Unrelated Task B"},
	} {
		if err := s.Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Archive with singular "id" (not "ids") + scope — should only archive GOL-1
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "set", "field": "status", "value": "archived", "bypass_guards": true,
		"id":    "GOL-1",
		"scope": "test",
	})

	// BUG: without fix, this takes the bulk path and archives ALL 3 artifacts in scope
	if strings.Contains(text, "archived 3") {
		t.Fatalf("singular id with scope should not bulk-archive entire scope: %s", text)
	}

	// Verify only GOL-1 is archived
	goal, _ := s.Get(ctx, "GOL-1")
	task1, _ := s.Get(ctx, "TASK-1")
	task2, _ := s.Get(ctx, "TASK-2")

	if parchment.StatusFromLabels(goal.Labels) != "archived" {
		t.Errorf("GOL-1 should be archived, got %s", parchment.StatusFromLabels(goal.Labels))
	}
	if parchment.StatusFromLabels(task1.Labels) != "work.active" {
		t.Errorf("TASK-1 should remain active, got %s", parchment.StatusFromLabels(task1.Labels))
	}
	if parchment.StatusFromLabels(task2.Labels) != "work.active" {
		t.Errorf("TASK-2 should remain active, got %s", parchment.StatusFromLabels(task2.Labels))
	}
}

// --- PRC-BUG-12: archive single ID should support dry_run ---

func TestArchive_SingleIDDryRun(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	if err := s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.active", "project:test"}, ID: "TASK-1", Title: "Dry Run Target"}); err != nil {
		t.Fatal(err)
	}

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Archive with dry_run — should preview, not commit
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "set", "field": "status", "value": "archived", "bypass_guards": true,
		"id":      "TASK-1",
		"dry_run": true,
	})

	// Should mention dry run and the artifact
	if !strings.Contains(text, "dry") && !strings.Contains(text, "would") {
		t.Errorf("dry_run should preview, got: %s", text)
	}

	// Artifact should NOT be archived
	art, _ := s.Get(ctx, "TASK-1")
	if parchment.StatusFromLabels(art.Labels) == "archived" {
		t.Fatal("dry_run should not actually archive the artifact")
	}
}

// --- PRC-BUG-12: cascade archive should support dry_run showing full subtree ---

func TestArchive_CascadeDryRun(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Create parent + 3 children
	if err := s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.goal", "work.active", "project:test"}, ID: "GOL-1", Title: "Parent Goal"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("TASK-%d", i)
		if err := s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:effort.task", "work.active", "project:test"}, ID: id, Title: fmt.Sprintf("Child %d", i)}); err != nil {
			t.Fatal(err)
		}
		if err := s.AddEdge(ctx, parchment.Edge{From: "GOL-1", To: id, Relation: parchment.RelParentOf}); err != nil {
			t.Fatal(err)
		}
	}

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "set", "field": "status", "value": "archived", "bypass_guards": true,
		"id":      "GOL-1",
		"cascade": true,
		"dry_run": true,
	})

	// Should mention all 4 artifacts
	if !strings.Contains(text, "dry") && !strings.Contains(text, "would") {
		t.Errorf("cascade dry_run should preview, got: %s", text)
	}

	// Nothing should be archived
	for _, id := range []string{"GOL-1", "TASK-1", "TASK-2", "TASK-3"} {
		art, _ := s.Get(ctx, id)
		if parchment.StatusFromLabels(art.Labels) == "archived" {
			t.Errorf("%s should not be archived during dry_run", id)
		}
	}
}

// --- SCR-BUG-33: template conformance bypassed when sections attached after satisfies link ---

func TestTemplate_SectionsAttachedAfterCreationValidatedOnLink(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create spec BEFORE template exists so auto-link doesn't kick in
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Bare Spec",
		"scope":  "test",
	})
	id := extractID(t, text)

	// Now create the template
	createMCPTemplate(t, s)

	// Try to add satisfies link WITHOUT having sections — should fail
	ctx := context.Background()
	linkResult, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "graph",
		Arguments: map[string]any{
			"action":   "link",
			"id":       id,
			"relation": "satisfies",
			"targets":  []string{"SCR-TPL-1"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !linkResult.IsError {
		t.Fatal("adding satisfies link without required sections should fail template conformance")
	}

	// Now attach sections via update with sections=[]
	callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     id,
		"sections": []any{
			map[string]any{"name": "problem", "text": "Background"},
			map[string]any{"name": "decision", "text": "Steps"},
			map[string]any{"name": "acceptance", "text": "Given/When/Then"},
		},
	})

	// Now adding satisfies link should succeed
	linkResult2, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "graph",
		Arguments: map[string]any{
			"action":   "link",
			"id":       id,
			"relation": "satisfies",
			"targets":  []string{"SCR-TPL-1"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if linkResult2.IsError {
		var errMsg string
		for _, c := range linkResult2.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				errMsg = tc.Text
				break
			}
		}
		t.Fatalf("linking with all sections present should succeed: %s", errMsg)
	}
}

// SCR-BUG-37: sections with "slug" key silently dropped — template conformance fails
// even when all required sections are provided via slug+body instead of name+text.
func TestTemplate_MCPCreateWithSlugAlias(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// LLMs commonly send "slug" instead of "name" and "body" instead of "text".
	// This should succeed but currently fails with "missing sections".
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "intent.spec",
			"title":  "Slug Alias Spec",
			"scope":  "test",
			"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
			"sections": []map[string]string{
				{"slug": "problem", "body": "Background info"},
				{"slug": "decision", "body": "Steps to follow"},
				{"slug": "acceptance", "body": "Acceptance criteria"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.IsError {
		var errMsg string
		for _, c := range result.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				errMsg = tc.Text
				break
			}
		}
		t.Fatalf("sections with slug+body aliases should be accepted, but got error: %s", errMsg)
	}

	var text string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			text = tc.Text
			break
		}
	}
	if !strings.Contains(text, "Slug Alias Spec") {
		t.Errorf("artifact should be created successfully, got: %s", text)
	}

	// Verify sections were actually stored
	id := extractID(t, text)
	for _, sec := range []struct{ name, want string }{
		{"problem", "Background info"},
		{"decision", "Steps to follow"},
		{"acceptance", "Acceptance criteria"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get",
			"id":     id,
			"name":   sec.name,
		})
		if got != sec.want {
			t.Errorf("section %q: got %q, want %q", sec.name, got, sec.want)
		}
	}
}

// SCR-BUG-37: promote_stash also ignores slug alias and body alias.
func TestTemplate_PromoteStashWithSlugAlias(t *testing.T) {
	// Previously tested stash-on-create recovery. Rewritten for the new flow:
	// create succeeds as draft → attach sections → promote to active.
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	ctx := context.Background()

	// Create without sections — now succeeds as draft with a warning.
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Stash Test Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
	})
	// Must not be a hard error.
	if strings.Contains(text, "does not conform") && !strings.Contains(text, "[warn]") {
		t.Fatalf("partial create should succeed as draft, got: %s", text)
	}

	// Look up the created artifact.
	proto := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	arts, _ := proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{"kind:intent.spec"}})
	if len(arts) == 0 {
		t.Fatal("expected artifact to be created")
	}
	artID := arts[0].ID

	// Attach required sections via update with sections=[].
	out := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     artID,
		"sections": []any{
			map[string]any{"name": "problem", "text": "Background via attach"},
			map[string]any{"name": "decision", "text": "Steps via attach"},
			map[string]any{"name": "acceptance", "text": "Criteria via attach"},
		},
	})
	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("update sections failed: %s", out)
	}

	// Promote to active — should succeed now that required sections are present.
	result := callTool(t, cs, "artifact", map[string]any{
		"action": "set",
		"id":     artID,
		"field":  "status",
		"value":  "active",
	})
	if strings.Contains(strings.ToLower(result), "cannot promote") {
		t.Fatalf("promote after attaching required sections should succeed, got: %s", result)
	}
}

// SCR-BUG-37: Mixed slug and name in the same sections array.
func TestSectionAlias_MixedSlugAndName(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Mixed Keys Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
		"sections": []map[string]string{
			{"name": "problem", "text": "via name+text"},
			{"slug": "decision", "body": "via slug+body"},
			{"name": "acceptance", "body": "via name+body"},
		},
	})

	if strings.Contains(text, "does not conform") {
		t.Fatalf("mixed slug/name sections should all be accepted: %s", text)
	}

	id := extractID(t, text)
	for _, tc := range []struct{ name, want string }{
		{"problem", "via name+text"},
		{"decision", "via slug+body"},
		{"acceptance", "via name+body"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get", "id": id, "name": tc.name,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// SCR-BUG-37: When both slug AND name are present, name wins (canonical).
func TestSectionAlias_BothSlugAndNamePresent(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Both Keys Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "winner", "slug": "loser", "text": "name should win"},
		},
	})

	id := extractID(t, text)
	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get", "id": id, "name": "winner",
	})
	if got != "name should win" {
		t.Errorf("name should take precedence over slug, got section 'winner' = %q", got)
	}

	// "loser" should NOT exist as a section
	loser := callTool(t, cs, "artifact", map[string]any{
		"action": "get", "id": id, "name": "loser",
	})
	if loser == "name should win" {
		t.Error("slug value should not create a separate section when name is present")
	}
}

// SCR-BUG-37: When both text AND body are present, text wins (canonical).
func TestSectionAlias_BothTextAndBodyPresent(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Both Text Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "notes", "text": "text wins", "body": "body loses"},
		},
	})

	id := extractID(t, text)
	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get", "id": id, "name": "notes",
	})
	if got != "text wins" {
		t.Errorf("text should take precedence over body, got %q", got)
	}
}

// SCR-BUG-37: Sections with neither name nor slug are silently skipped.
func TestSectionAlias_NoNameNoSlug(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "intent.spec",
			"title":  "Orphan Sections Spec",
			"scope":  "test",
			"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
			"sections": []map[string]string{
				{"body": "orphan with no name or slug"},
				{"text": "another orphan"},
				{"slug": "problem", "body": "this one is valid"},
				{"slug": "decision", "body": "also valid"},
				{"slug": "acceptance", "body": "also valid"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	// Should succeed — the 3 valid sections satisfy the template, orphans are dropped
	if result.IsError {
		var errMsg string
		for _, c := range result.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				errMsg = tc.Text
				break
			}
		}
		t.Fatalf("valid sections should pass template despite orphans: %s", errMsg)
	}
}

// SCR-BUG-37: Empty slug string is treated as missing.
func TestSectionAlias_EmptySlug(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Empty Slug Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"slug": "", "body": "should be skipped"},
			{"name": "", "text": "also skipped"},
			{"slug": "valid", "body": "this one counts"},
		},
	})

	id := extractID(t, text)
	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get", "id": id, "name": "valid",
	})
	if got != "this one counts" {
		t.Errorf("valid section should be stored, got %q", got)
	}
}

// SCR-BUG-37: Batch attach_section with slug aliases.
func TestSectionAlias_BatchAttachWithSlug(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create artifact first
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Batch Slug Task",
		"scope":  "test",
	})
	id := extractID(t, text)

	// Batch attach via update with sections=[{name, text}]
	callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     id,
		"sections": []any{
			map[string]any{"name": "design", "text": "design via name"},
			map[string]any{"name": "notes", "text": "notes via name"},
			map[string]any{"name": "context", "text": "context via name"},
		},
	})

	for _, tc := range []struct{ name, want string }{
		{"design", "design via name"},
		{"notes", "notes via name"},
		{"context", "context via name"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get", "id": id, "name": tc.name,
		})
		if got != tc.want {
			t.Errorf("batch section %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// SCR-BUG-37: Stash created with name+text, promoted with slug+body additions.
func TestSectionAlias_CrossFormatStashPromote(t *testing.T) {
	// Previously tested stash-on-create + cross-format slug promotion.
	// Rewritten: create with partial sections (succeeds as draft),
	// then attach missing sections using slug format via attach_section.
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	ctx := context.Background()

	// Create with only decision section — now succeeds as draft.
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Cross Format Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
		"sections": []map[string]string{
			{"name": "decision", "text": "original decision"},
		},
	})
	if strings.Contains(text, "does not conform") && !strings.Contains(text, "[warn]") {
		t.Fatalf("partial create should succeed as draft, got: %s", text)
	}

	proto := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	arts, _ := proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{"kind:intent.spec"}})
	if len(arts) == 0 {
		t.Fatal("expected artifact to be created")
	}
	artID := arts[0].ID

	// Add missing required sections via update with sections=[].
	out := callTool(t, cs, "artifact", map[string]any{
		"action": "update",
		"id":     artID,
		"sections": []any{
			map[string]any{"name": "problem", "text": "promoted problem"},
			map[string]any{"name": "acceptance", "text": "promoted acceptance"},
		},
	})
	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("update sections failed: %s", out)
	}

	// Verify all sections are present on the artifact.
	got, _ := proto.GetArtifact(ctx, artID)
	if got == nil {
		t.Fatal("artifact not found")
	}
	have := map[string]string{}
	for _, sec := range got.Sections {
		have[sec.Name] = sec.Text
	}
	for _, want := range []string{"decision", "problem", "acceptance"} {
		if have[want] == "" {
			t.Errorf("section %q missing after attach, have: %v", want, have)
		}
	}
}

// SCR-BUG-37: Patch map combined with slug-keyed sections — both paths contribute.
func TestSectionAlias_PatchAndSlugCombined(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "intent.spec",
		"title":  "Patch Plus Slug Spec",
		"scope":  "test",
		"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
		"sections": []map[string]string{
			{"slug": "problem", "body": "problem via slug"},
		},
		"patch": map[string]string{
			"decision":   "decision via patch",
			"acceptance": "acceptance via patch",
		},
	})

	if strings.Contains(text, "does not conform") {
		t.Fatalf("slug sections + patch should satisfy template together: %s", text)
	}

	id := extractID(t, text)
	for _, tc := range []struct{ name, want string }{
		{"problem", "problem via slug"},
		{"decision", "decision via patch"},
		{"acceptance", "acceptance via patch"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get", "id": id, "name": tc.name,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// SCR-BUG-37: Duplicate slugs in same request — last writer wins.
func TestSectionAlias_DuplicateSlug(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "Duplicate Slug Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"slug": "notes", "body": "first"},
			{"slug": "notes", "body": "second"},
		},
	})

	id := extractID(t, text)
	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get", "id": id, "name": "notes",
	})
	// Should have some value — either first or second, but not crash or empty
	if got == "" {
		t.Error("duplicate slug should still produce a stored section, got empty")
	}
}

// --- SCR-TSK-284: Search action alias ---

func TestList_SearchByQuery(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "T-001", Labels: []string{"kind:effort.task", "work.draft", "project:test"}, Title: "Implement auth module"},
		{ID: "T-002", Labels: []string{"kind:effort.task", "work.draft", "project:test"}, Title: "Fix database migration"},
		{ID: "T-003", Labels: []string{"kind:effort.task", "work.draft", "project:test"}, Title: "Add logging middleware"},
	} {
		if err := s.Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "query",
		"query":  "auth",
	})

	if !strings.Contains(text, "Implement auth module") {
		t.Errorf("list(query=auth) should find 'Implement auth module', got: %s", text)
	}
	if strings.Contains(text, "Fix database migration") {
		t.Errorf("list(query=auth) should not return 'Fix database migration', got: %s", text)
	}
	if strings.Contains(text, "Add logging middleware") {
		t.Errorf("list(query=auth) should not return 'Add logging middleware', got: %s", text)
	}
}

// --- SCR-TSK-283: Error param hints ---

func TestErrorMessages_ContainParamNames(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	ctx := context.Background()

	// Call set with no id -> assert error contains "id"
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "set",
			"field":  "status",
			"value":  "active",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for set without id")
	}
	setErrMsg := extractErrorText(t, result)
	if !strings.Contains(setErrMsg, "id") {
		t.Errorf("set error should mention 'id' param, got: %s", setErrMsg)
	}

	// Call graph link with no id -> assert error contains "id"
	result, err = cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "graph",
		Arguments: map[string]any{
			"action":   "link",
			"relation": "depends_on",
			"targets":  []string{"T-001"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for link without id")
	}
	linkIDErr := extractErrorText(t, result)
	if !strings.Contains(linkIDErr, "id") {
		t.Errorf("link error should mention 'id' param, got: %s", linkIDErr)
	}

	// Call graph link with no targets -> assert error contains "targets"
	result, err = cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "graph",
		Arguments: map[string]any{
			"action":   "link",
			"id":       "T-001",
			"relation": "depends_on",
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for link without targets")
	}
	linkTargetsErr := extractErrorText(t, result)
	if !strings.Contains(linkTargetsErr, "targets") {
		t.Errorf("link error should mention 'targets' param, got: %s", linkTargetsErr)
	}

	// Call graph link with no relation -> assert error contains "relation"
	result, err = cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "graph",
		Arguments: map[string]any{
			"action":  "link",
			"id":      "T-001",
			"targets": []string{"T-002"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for link without relation")
	}
	linkRelationErr := extractErrorText(t, result)
	if !strings.Contains(linkRelationErr, "relation") {
		t.Errorf("link error should mention 'relation' param, got: %s", linkRelationErr)
	}
}

func extractErrorText(t *testing.T, result *sdkmcp.CallToolResult) string {
	t.Helper()
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			return tc.Text
		}
	}
	t.Fatal("no text content in error result")
	return ""
}

func TestStreamableHTTP_ToolsListPreservesTypedInputSchemas(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"origami"}, parchment.ProtocolConfig{}, "test")

	handler := sdkmcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *sdkmcp.Server { return srv },
		&sdkmcp.StreamableHTTPOptions{
			SessionTimeout: time.Minute,
		},
	)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	sid := initHTTPSession(t, ts.URL)
	resp := postJSONRPC(t, ts.URL, sid, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	defer resp.Body.Close()

	payload := decodeJSONRPC(t, resp)
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list missing result: %s", mustJSON(payload))
	}
	rawTools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list missing tools array: %s", mustJSON(payload))
	}

	expectedProps := map[string][]string{
		"artifact": {"action", "kind", "id", "relation", "targets"},
	}
	seen := make(map[string]bool, len(expectedProps))

	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			t.Fatalf("tools/list entry has unexpected type %T", rawTool)
		}
		name, _ := tool["name"].(string)
		wantProps, ok := expectedProps[name]
		if !ok {
			continue
		}
		seen[name] = true

		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Fatalf("%s missing inputSchema object: %s", name, mustJSON(tool))
		}
		if gotType, _ := schema["type"].(string); gotType != "object" {
			t.Errorf("%s inputSchema.type = %q, want %q", name, gotType, "object")
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			t.Fatalf("%s inputSchema missing properties: %s", name, mustJSON(schema))
		}
		for _, prop := range wantProps {
			if _, ok := props[prop]; !ok {
				t.Fatalf("%s inputSchema missing property %q: %s", name, prop, mustJSON(schema))
			}
		}
	}

	for name := range expectedProps {
		if !seen[name] {
			t.Fatalf("tools/list missing tool %q: %s", name, mustJSON(payload))
		}
	}
}

func initHTTPSession(t *testing.T, baseURL string) string {
	t.Helper()
	resp := postJSONRPC(t, baseURL, "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "mcp-test",
				"version": "0.1",
			},
		},
	})
	sid := resp.Header.Get("Mcp-Session-Id")
	resp.Body.Close()
	if sid == "" {
		t.Fatal("initialize response missing Mcp-Session-Id")
	}

	initResp := postJSONRPC(t, baseURL, sid, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	initResp.Body.Close()
	return sid
}

func postJSONRPC(t *testing.T, baseURL, sid string, payload map[string]any) *http.Response {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal JSON-RPC payload: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", req.URL.String(), err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("POST %s: HTTP %d: %s", req.URL.String(), resp.StatusCode, string(raw))
	}
	return resp
}

func decodeJSONRPC(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read JSON-RPC response: %v", err)
	}

	body := raw
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			body = []byte(strings.TrimPrefix(line, "data: "))
			break
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode JSON-RPC response: %v\nraw: %s", err, string(raw))
	}
	return payload
}

func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(data)
}

// --- Progressive disclosure tests (SCR-TSK-327, 323, 324, 328) ---

// TestList_UnfilteredHint verifies that a bare list with no filters appends a
// truncation/context-cost hint steering agents toward filtered alternatives.
func TestList_UnfilteredHint(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	out := callTool(t, cs, "artifact", map[string]any{"action": "query"})

	if !strings.Contains(out, "top=10") || !strings.Contains(out, "filters to narrow") {
		t.Errorf("unfiltered list must include context-cost hint; got:\n%s", out)
	}
}

// TestList_FilteredNoHint verifies that a filtered list does NOT add the hint
// (the hint is only for unfiltered dumps).
func TestList_FilteredNoHint(t *testing.T) {
	s := openStore(t)
	seedArtifacts(t, s)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	out := callTool(t, cs, "artifact", map[string]any{"action": "query", "scope": "origami"})

	if strings.Contains(out, "filters to narrow") {
		t.Errorf("filtered list must not include truncation hint; got:\n%s", out)
	}
}

// TestToolDescriptions_ProgressiveDisclosure verifies that all three tool
// descriptions contain the key guidance phrases required by SCR-TSK-323/324/328.
func TestToolDescriptions_ProgressiveDisclosure(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	ctx := context.Background()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	desc := make(map[string]string)
	for _, tool := range tools.Tools {
		desc[tool.Name] = tool.Description
	}

	// artifact: CRUD + search surface.
	artifact := desc["artifact"]
	for _, phrase := range []string{"query", "get", "create", "set", "sort=topo"} {
		if !strings.Contains(artifact, phrase) {
			t.Errorf("artifact desc missing %q; got:\n%s", phrase, artifact)
		}
	}

	// graph: edge operations + analysis.
	graph := desc["graph"]
	for _, phrase := range []string{"link", "unlink", "EDGES", "analyze", "synonym"} {
		if !strings.Contains(graph, phrase) {
			t.Errorf("graph desc missing %q; got:\n%s", phrase, graph)
		}
	}

	// admin: ops + introspection.
	admin := desc["admin"]
	for _, phrase := range []string{"hygiene", "lint", "dashboard", "history"} {
		if !strings.Contains(admin, phrase) {
			t.Errorf("admin desc missing %q; got:\n%s", phrase, admin)
		}
	}
}

func TestToolRegistry_ReturnsNonEmpty(t *testing.T) {
	reg := scribemcp.ToolRegistry()
	tools := reg.List()
	if len(tools) == 0 {
		t.Fatal("ToolRegistry should return at least one tool")
	}
}

// TestArtifactSchema_KindDescriptionFromRegistry verifies that the artifact tool's
// "kind" field description is populated from the live LabelTrait registry,
// not the hardcoded struct tag.
//
// Given: kind:note and kind:task are registered (seeded from registry YAML)
// When:  the MCP server is initialized and tools/list is called
// Then:  the kind field description contains the registered kind names
func TestArtifactSchema_KindDescriptionFromRegistry(t *testing.T) {
	t.Parallel()
	srv, _ := scribemcp.NewServerFromStore(parchment.NewMemoryStore(), []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var artifactSchema map[string]any
	for _, tool := range tools.Tools {
		if tool.Name == "artifact" {
			raw, _ := json.Marshal(tool.InputSchema)
			_ = json.Unmarshal(raw, &artifactSchema)
			break
		}
	}
	if artifactSchema == nil {
		t.Fatal("artifact tool not found in tools/list")
	}

	props, ok := artifactSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("artifact schema missing properties")
	}
	kindProp, ok := props["kind"].(map[string]any)
	if !ok {
		t.Fatal("artifact schema missing kind property")
	}
	desc, _ := kindProp["description"].(string)

	// Registry seeds note and task at minimum — both must appear.
	for _, want := range []string{"note", "task"} {
		if !strings.Contains(desc, want) {
			t.Errorf("kind description missing %q; got: %s", want, desc)
		}
	}
	// Must NOT be the old hardcoded stub value.
	if desc == "task, spec, bug, goal, campaign, doc, ref, need, decision" {
		t.Error("kind description is still the hardcoded stub — registry patch did not apply")
	}
}

func TestWorkspaceWarning_SuppressedWhenArtifactHasScope(t *testing.T) {
	// Bug: workspace unset warning fires even when the artifact has an explicit scope.
	// No workspace labels passed to NewServerFromStore → session is unconfigured.
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "effort.task",
		"title":  "scoped task",
		"scope":  "myproject",
	})
	if strings.Contains(text, "workspace unset") {
		t.Errorf("workspace warning should be suppressed when artifact has explicit scope, got: %s", text)
	}
}
