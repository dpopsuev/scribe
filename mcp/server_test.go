package mcp_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func openStore(t *testing.T) *parchment.SQLiteStore {
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

	srv, _ := scribemcp.NewServer(s, []string{"origami", "mos"}, nil, parchment.ProtocolConfig{}, "test")
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

	srv, _ := scribemcp.NewServer(s, []string{"origami"}, nil, parchment.ProtocolConfig{}, "test")
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

	srv, _ := scribemcp.NewServer(s, []string{"origami"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "Auto-scoped task",
	})

	if !strings.Contains(text, "TASK-") {
		t.Errorf("expected TASK- ID in created artifact, got: %s", text)
	}

	arts, _ := s.List(ctx, parchment.Filter{Scope: "origami"})
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

	srv, _ := scribemcp.NewServer(s, []string{"mos"}, nil, parchment.ProtocolConfig{}, "test")
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

	srv, _ := scribemcp.NewServer(s, []string{"mos"}, nil, parchment.ProtocolConfig{}, "test")
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

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

	_ = s.Put(ctx, &parchment.Artifact{ID: "SPR-1", Kind: "sprint", Scope: "origami", Status: "active", Title: "Sprint One"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "TASK-1", Kind: "task", Scope: "origami", Status: "draft", Title: "Origami Work", Parent: "SPR-1"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "TASK-2", Kind: "task", Scope: "scribe", Status: "draft", Title: "Scribe Work", Parent: "SPR-1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

	_ = s.Put(ctx, &parchment.Artifact{ID: "SPR-1", Kind: "sprint", Scope: "origami", Status: "active", Title: "Sprint One"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "TASK-1", Kind: "task", Scope: "origami", Status: "draft", Title: "Work A", Parent: "SPR-1"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "TASK-2", Kind: "task", Scope: "origami", Status: "draft", Title: "Work B", Parent: "SPR-1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

	_ = s.Put(ctx, &parchment.Artifact{ID: "CAM-1", Kind: "campaign", Scope: "go4", Status: "active", Title: "Gang of Four"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "TSK-1", Kind: "task", Scope: "go4", Status: "draft", Title: "Remove MCP clients", Parent: "CAM-1"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "BUG-1", Kind: "bug", Scope: "limes", Status: "draft", Title: "Hardcoded deps"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "TSK-1", To: "BUG-1", Relation: "implements"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

	_ = s.Put(ctx, &parchment.Artifact{ID: "SPC-1", Kind: "spec", Scope: "scribe", Status: "draft", Title: "Spec"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "TSK-1", Kind: "task", Scope: "scribe", Status: "draft", Title: "Task"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "TSK-1", To: "SPC-1", Relation: "implements"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

	_ = s.Put(ctx, &parchment.Artifact{ID: "TSK-1", Kind: "task", Scope: "scribe", Status: "draft", Title: "Task"})
	_ = s.Put(ctx, &parchment.Artifact{ID: "SPC-1", Kind: "spec", Scope: "scribe", Status: "draft", Title: "Spec"})
	_ = s.AddEdge(ctx, parchment.Edge{From: "TSK-1", To: "SPC-1", Relation: "implements"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

func createMCPTemplate(t *testing.T, s parchment.Store) {
	t.Helper()
	ctx := context.Background()
	err := s.Put(ctx, &parchment.Artifact{
		ID: "SCR-TPL-1", Kind: "template", Status: "active", Title: "Spec Template", Scope: "test",
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
		ID: "TPL-2026-002", Kind: "template", Status: "active", Title: "Eight Section Template", Scope: "test",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
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
	if !strings.Contains(errMsg, "problem") {
		t.Errorf("error should mention missing section 'problem', got: %s", errMsg)
	}
}

func TestTemplate_MCPCreateWithAllSections(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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
	if !strings.Contains(text, "SPC-") && !strings.Contains(text, "SPEC-") {
		t.Errorf("artifact ID should be present, got: %s", text)
	}
}

// --- SCR-TSK-265: Section content round-trip ---

func TestSectionRoundTrip_SectionsArray(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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
			"action": "get_section",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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
			"action": "get_section",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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
			"action": "get_section",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	mdContent := "# Header\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n\n- bullet 1\n- bullet 2"

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "Markdown Fidelity Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "notes", "text": mdContent},
		},
	})

	id := extractID(t, createText)

	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get_section",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	createText := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
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
			"action": "get_section",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Provide only MustSection ("problem") but not ShouldSections ("decision", "acceptance").
	// With the TSK-28 fix, creation should SUCCEED — non-MustSections are deferred to completion.
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
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
	if !strings.Contains(text, "SPC-") && !strings.Contains(text, "SPEC-") {
		t.Errorf("artifact ID should be present, got: %s", text)
	}
}

func TestTemplate_MCPLinkSatisfiesBlocksMissingSections(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	createMCPTemplate(t, s)

	// Create artifact missing the MustSection ("problem") — only has non-required sections
	s.Put(ctx, &parchment.Artifact{
		ID: "SPEC-2026-001", Kind: "spec", Status: "draft", Title: "Incomplete Spec", Scope: "test",
		Sections: []parchment.Section{
			{Name: "decision", Text: "Some decision"},
			// Missing "problem" which is the MustSection for spec
		},
	})

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
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
		ID: "SPEC-2026-002", Kind: "spec", Status: "draft", Title: "Complete Spec", Scope: "test",
		Sections: []parchment.Section{
			{Name: "problem", Text: "What is broken"},
			{Name: "decision", Text: "What was decided"},
			{Name: "acceptance", Text: "Acceptance criteria"},
		},
	})

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
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
		s.Put(ctx, &parchment.Artifact{
			ID: fmt.Sprintf("TASK-2026-%03d", i), Kind: "task", Scope: "test",
			Status: "draft", Title: fmt.Sprintf("Task %d", i),
		})
	}

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "SPEC-2026-001", Kind: "spec", Scope: "test", Status: "draft", Title: "S1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "SPEC-2026-001", Kind: "spec", Scope: "test", Status: "draft", Title: "S1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

// --- SCR-BUG-22: attach_section body field silently dropped ---

func TestAttachSection_BodyFieldAlias(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{ID: "TSK-1", Kind: "task", Scope: "test", Status: "draft", Title: "Test"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Claude sends "body" not "text" — this is the bug.
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "attach_section",
		"id":     "TSK-1",
		"name":   "context",
		"body":   "This content should be stored, not silently dropped.",
	})
	if !strings.Contains(text, "section \"context\" added") {
		t.Errorf("attach with body field should succeed: %s", text)
	}

	// Verify content is actually persisted.
	art, _ := s.Get(ctx, "TSK-1")
	found := false
	for _, sec := range art.Sections {
		if sec.Name == "context" {
			if sec.Text == "" {
				t.Error("SCR-BUG-22: section created but body content silently dropped")
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
	s.Put(ctx, &parchment.Artifact{ID: "TSK-2", Kind: "task", Scope: "test", Status: "draft", Title: "Test2"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Batch with "body" field instead of "text".
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "attach_section",
		"id":     "TSK-2",
		"sections": []any{
			map[string]any{"name": "problem", "body": "Problem statement via body field"},
			map[string]any{"name": "fix", "body": "Fix description via body field"},
		},
	})
	if !strings.Contains(text, "2 sections") {
		t.Errorf("batch attach with body field should succeed: %s", text)
	}

	art, _ := s.Get(ctx, "TSK-2")
	for _, sec := range art.Sections {
		if sec.Text == "" {
			t.Errorf("SCR-BUG-22: batch section %q created but body content dropped", sec.Name)
		}
	}
}

// --- SCR-TSK-14: Batch create ---

func TestBatchCreate(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	arts, _ := s.List(ctx, parchment.Filter{Kind: "task"})
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
	s.Put(ctx, &parchment.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1", Priority: "low"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "BUG-2026-001", Kind: "bug", Scope: "test", Status: "open", Title: "Critical bug", Priority: "critical"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "active", Title: "Recent task"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "TASK-2026-001", Kind: "task", Scope: "test", Status: "active", Title: "Active"})
	s.Put(ctx, &parchment.Artifact{ID: "TASK-2026-002", Kind: "task", Scope: "test", Status: "draft", Title: "Draft"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "admin", map[string]any{"action": "motd"})

	if !strings.Contains(text, "Active Work:") {
		t.Errorf("expected Active Work summary: %s", text)
	}
}

// --- SCR-TSK-15: Count mode ---

func TestListCount(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		s.Put(ctx, &parchment.Artifact{
			ID: fmt.Sprintf("TASK-2026-%03d", i), Kind: "task", Scope: "test",
			Status: "draft", Title: fmt.Sprintf("Task %d", i),
		})
	}

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "list",
		"count":  true,
		"kind":   "task",
	})
	if text != "5" {
		t.Errorf("expected count '5', got %q", text)
	}
}

func TestListCount_GroupBy(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})
	s.Put(ctx, &parchment.Artifact{ID: "T-002", Kind: "task", Scope: "test", Status: "active", Title: "T2"})
	s.Put(ctx, &parchment.Artifact{ID: "T-003", Kind: "task", Scope: "test", Status: "draft", Title: "T3"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action":   "list",
		"count":    true,
		"group_by": "status",
	})
	if !strings.Contains(text, `"draft": 2`) || !strings.Contains(text, `"active": 1`) {
		t.Errorf("expected grouped counts, got: %s", text)
	}
}

// --- SCR-TSK-11: Changelog ---

func TestChangelog(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	since := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "test", Status: "active", Title: "Changed"})
	s.Put(ctx, &parchment.Artifact{ID: "T-002", Kind: "task", Scope: "test", Status: "draft", Title: "Also changed"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "admin", map[string]any{
		"action": "changelog",
		"since":  since,
	})
	if !strings.Contains(text, "2 artifacts") {
		t.Errorf("expected 2 artifacts in changelog, got: %s", text)
	}
	if !strings.Contains(text, "T-001") || !strings.Contains(text, "T-002") {
		t.Errorf("expected both artifact IDs in changelog: %s", text)
	}
}

func TestChangelog_ScopeFilter(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	since := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "alpha", Status: "active", Title: "Alpha"})
	s.Put(ctx, &parchment.Artifact{ID: "T-002", Kind: "task", Scope: "beta", Status: "draft", Title: "Beta"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "admin", map[string]any{
		"action": "changelog",
		"since":  since,
		"scope":  "alpha",
	})
	if !strings.Contains(text, "T-001") {
		t.Errorf("expected alpha artifact: %s", text)
	}
	if strings.Contains(text, "T-002") {
		t.Errorf("beta artifact should be filtered out: %s", text)
	}
}

func TestChangelog_NoChanges(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	future := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	text := callTool(t, cs, "admin", map[string]any{
		"action": "changelog",
		"since":  future,
	})
	if !strings.Contains(text, "no changes") {
		t.Errorf("expected 'no changes', got: %s", text)
	}
}

// --- SCR-TSK-10: Auto-link template ---

func TestAutoLinkTemplate(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Create a template for specs in scope "test"
	s.SetScopeKey(ctx, "test", "TST", false)
	s.Put(ctx, &parchment.Artifact{
		ID: "TST-TPL-1", Kind: "template", Scope: "test", Status: "active",
		Title: "Spec Template",
		Sections: []parchment.Section{
			{Name: "problem", Text: "Describe the problem"},
			{Name: "decision", Text: "What was decided"},
		},
	})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create a spec WITHOUT explicit satisfies link — should auto-link
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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
	arts, _ := s.List(ctx, parchment.Filter{Kind: "spec", Scope: "test"})
	found := false
	for _, a := range arts {
		if a.Title == "Auto-linked Spec" && len(a.Links["satisfies"]) == 1 && a.Links["satisfies"][0] == "TST-TPL-1" {
			found = true
		}
	}
	if !found {
		t.Error("expected satisfies link to TST-TPL-1 to be auto-added")
	}
}

func TestAutoLinkTemplate_ExplicitOverride(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	s.SetScopeKey(ctx, "test", "TST", false)
	s.Put(ctx, &parchment.Artifact{
		ID: "TST-TPL-1", Kind: "template", Scope: "test", Status: "active",
		Title:    "Spec Template",
		Sections: []parchment.Section{{Name: "problem", Text: "Describe"}},
	})
	s.Put(ctx, &parchment.Artifact{
		ID: "TST-TPL-2", Kind: "template", Scope: "test", Status: "active",
		Title:    "Custom Template",
		Sections: []parchment.Section{{Name: "overview", Text: "Describe"}},
	})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create with explicit satisfies — should NOT auto-link
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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

	arts, _ := s.List(ctx, parchment.Filter{Kind: "spec", Scope: "test"})
	for _, a := range arts {
		if a.Title == "Explicit Spec" {
			if len(a.Links["satisfies"]) != 1 || a.Links["satisfies"][0] != "TST-TPL-2" {
				t.Errorf("explicit satisfies should be TST-TPL-2, got: %v", a.Links["satisfies"])
			}
		}
	}
}

func TestAutoLinkTemplate_NoTemplateInScope(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create spec in scope with no templates — should succeed without error
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
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
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "alpha", Status: "draft", Title: "First"})
	s.Put(ctx, &parchment.Artifact{ID: "T-002", Kind: "spec", Scope: "beta", Status: "active", Title: "Second"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "list",
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

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "list",
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
		ID: "SPEC-001", Kind: "spec", Scope: "alpha", Status: "active",
		Title: "Original Spec", Goal: "Original goal",
		Sections: []parchment.Section{
			{Name: "problem", Text: "The problem"},
			{Name: "decision", Text: "The decision"},
		},
		Labels: []string{"backend", "api"},
	})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "clone",
		"id":     "SPEC-001",
		"title":  "Cloned Spec",
		"scope":  "beta",
	})
	if !strings.Contains(text, "cloned SPEC-001") {
		t.Fatalf("clone failed: %s", text)
	}
	if !strings.Contains(text, "Cloned Spec") {
		t.Errorf("expected cloned title: %s", text)
	}

	// Verify clone has sections
	arts, _ := s.List(ctx, parchment.Filter{Scope: "beta", Kind: "spec"})
	if len(arts) != 1 {
		t.Fatalf("expected 1 cloned spec, got %d", len(arts))
	}
	clone := arts[0]
	if clone.Title != "Cloned Spec" {
		t.Errorf("clone title = %q, want 'Cloned Spec'", clone.Title)
	}
	if clone.Goal != "Original goal" {
		t.Errorf("clone should inherit goal, got %q", clone.Goal)
	}
	if len(clone.Sections) != 2 {
		t.Errorf("clone should have 2 sections, got %d", len(clone.Sections))
	}
	if clone.Status != "draft" {
		t.Errorf("clone status should default to draft, got %q", clone.Status)
	}
	if clone.ID == "SPEC-001" {
		t.Error("clone should have a new ID")
	}
}

func TestClone_NonexistentSource(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "clone",
			"id":     "NOPE-999",
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
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})
	s.Put(ctx, &parchment.Artifact{ID: "T-002", Kind: "task", Scope: "test", Status: "draft", Title: "T2"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
		"action": "list",
		"fields": []any{"id", "title"},
	})
	if !strings.Contains(text, "T-001") {
		t.Errorf("fields array failed: %s", text)
	}

	// sections array (create)
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "With Sections",
		"scope":  "test",
		"sections": []any{
			map[string]any{"name": "context", "text": "Background"},
		},
	})
	if !strings.Contains(text, "With Sections") {
		t.Errorf("sections array failed: %s", text)
	}

	// artifacts array (batch_create)
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "batch_create",
		"artifacts": []any{
			map[string]any{"kind": "task", "title": "Batch 1", "scope": "test"},
			map[string]any{"kind": "task", "title": "Batch 2", "scope": "test"},
		},
	})
	if !strings.Contains(text, "created 2") {
		t.Errorf("artifacts array failed: %s", text)
	}
}

func TestMCPSchema_BooleanTypes(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})
	s.Put(ctx, &parchment.Artifact{ID: "T-002", Kind: "task", Scope: "test", Status: "draft", Title: "T2"})
	s.AddEdge(ctx, parchment.Edge{From: "T-001", To: "T-002", Relation: "depends_on"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
		"action": "list",
		"count":  true,
		"kind":   "task",
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
		s.Put(ctx, &parchment.Artifact{
			ID: fmt.Sprintf("T-%03d", i), Kind: "task", Scope: "test",
			Status: "draft", Title: fmt.Sprintf("Task %d", i),
		})
	}

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
		if art.Priority != "high" || art.Sprint != "SPR-1" {
			t.Errorf("%s: priority=%q sprint=%q", id, art.Priority, art.Sprint)
		}
	}
}

func TestBatchUpdate_WithPatch(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})
	s.Put(ctx, &parchment.Artifact{ID: "T-002", Kind: "task", Scope: "test", Status: "draft", Title: "T2"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
		if art.Priority != "critical" || art.Sprint != "SPR-2" {
			t.Errorf("%s: priority=%q sprint=%q", id, art.Priority, art.Sprint)
		}
	}
}

func TestBatchUpdate_SingleIDBackwardCompat(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "T-001", Kind: "task", Scope: "test", Status: "draft", Title: "T1"})

	srv, _ := scribemcp.NewServer(s, nil, nil, parchment.ProtocolConfig{}, "test")
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
	s.Put(ctx, &parchment.Artifact{ID: "TPL-1", Kind: "template", Status: "active", Title: "Task Template",
		Sections: []parchment.Section{{Name: "content", Text: "body"}, {Name: "context", Text: "ctx"}},
	})
	text = callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
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
		{ID: "GOL-1", Kind: "goal", Scope: "test", Status: "active", Title: "Target Goal"},
		{ID: "TASK-1", Kind: "task", Scope: "test", Status: "active", Title: "Unrelated Task A"},
		{ID: "TASK-2", Kind: "task", Scope: "test", Status: "active", Title: "Unrelated Task B"},
	} {
		if err := s.Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Archive with singular "id" (not "ids") + scope — should only archive GOL-1
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "archive",
		"id":     "GOL-1",
		"scope":  "test",
	})

	// BUG: without fix, this takes the bulk path and archives ALL 3 artifacts in scope
	if strings.Contains(text, "archived 3") {
		t.Fatalf("singular id with scope should not bulk-archive entire scope: %s", text)
	}

	// Verify only GOL-1 is archived
	goal, _ := s.Get(ctx, "GOL-1")
	task1, _ := s.Get(ctx, "TASK-1")
	task2, _ := s.Get(ctx, "TASK-2")

	if goal.Status != "archived" {
		t.Errorf("GOL-1 should be archived, got %s", goal.Status)
	}
	if task1.Status != "active" {
		t.Errorf("TASK-1 should remain active, got %s", task1.Status)
	}
	if task2.Status != "active" {
		t.Errorf("TASK-2 should remain active, got %s", task2.Status)
	}
}

// --- PRC-BUG-12: archive single ID should support dry_run ---

func TestArchive_SingleIDDryRun(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	if err := s.Put(ctx, &parchment.Artifact{
		ID: "TASK-1", Kind: "task", Scope: "test", Status: "active", Title: "Dry Run Target",
	}); err != nil {
		t.Fatal(err)
	}

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Archive with dry_run — should preview, not commit
	text := callTool(t, cs, "artifact", map[string]any{
		"action":  "archive",
		"id":      "TASK-1",
		"dry_run": true,
	})

	// Should mention dry run and the artifact
	if !strings.Contains(text, "dry") && !strings.Contains(text, "would") {
		t.Errorf("dry_run should preview, got: %s", text)
	}

	// Artifact should NOT be archived
	art, _ := s.Get(ctx, "TASK-1")
	if art.Status == "archived" {
		t.Fatal("dry_run should not actually archive the artifact")
	}
}

// --- PRC-BUG-12: cascade archive should support dry_run showing full subtree ---

func TestArchive_CascadeDryRun(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Create parent + 3 children
	if err := s.Put(ctx, &parchment.Artifact{
		ID: "GOL-1", Kind: "goal", Scope: "test", Status: "active", Title: "Parent Goal",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		id := fmt.Sprintf("TASK-%d", i)
		if err := s.Put(ctx, &parchment.Artifact{
			ID: id, Kind: "task", Scope: "test", Status: "active", Title: fmt.Sprintf("Child %d", i), Parent: "GOL-1",
		}); err != nil {
			t.Fatal(err)
		}
		if err := s.AddEdge(ctx, parchment.Edge{From: "GOL-1", To: id, Relation: parchment.RelParentOf}); err != nil {
			t.Fatal(err)
		}
	}

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action":  "archive",
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
		if art.Status == "archived" {
			t.Errorf("%s should not be archived during dry_run", id)
		}
	}
}

// --- SCR-BUG-33: template conformance bypassed when sections attached after satisfies link ---

func TestTemplate_SectionsAttachedAfterCreationValidatedOnLink(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create spec BEFORE template exists so auto-link doesn't kick in
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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

	// Now attach sections
	for _, sec := range []struct{ name, text string }{
		{"problem", "Background"},
		{"decision", "Steps"},
		{"acceptance", "Given/When/Then"},
	} {
		callTool(t, cs, "artifact", map[string]any{
			"action": "attach_section",
			"id":     id,
			"name":   sec.name,
			"text":   sec.text,
		})
	}

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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// LLMs commonly send "slug" instead of "name" and "body" instead of "text".
	// This should succeed but currently fails with "missing sections".
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "spec",
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
			"action": "get_section",
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
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// First create without sections but with template link — will fail and stash
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "spec",
			"title":  "Stash Test Spec",
			"scope":  "test",
			"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected create to fail without sections when template is linked")
	}

	// Extract stash_id from error message
	var errMsg string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			errMsg = tc.Text
			break
		}
	}
	stashIdx := strings.Index(errMsg, "stash_id=")
	if stashIdx < 0 {
		t.Fatalf("expected stash_id in error, got: %s", errMsg)
	}
	stashID := errMsg[stashIdx+len("stash_id="):]
	stashID = strings.TrimSuffix(stashID, "]")

	// Now promote with slug+body — should work
	promoteResult, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action":   "promote_stash",
			"stash_id": stashID,
			"sections": []map[string]string{
				{"slug": "problem", "body": "Background via promote"},
				{"slug": "decision", "body": "Steps via promote"},
				{"slug": "acceptance", "body": "Criteria via promote"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if promoteResult.IsError {
		var promoteErr string
		for _, c := range promoteResult.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				promoteErr = tc.Text
				break
			}
		}
		t.Fatalf("promote_stash with slug+body should succeed, got: %s", promoteErr)
	}
}

// SCR-BUG-37: Mixed slug and name in the same sections array.
func TestSectionAlias_MixedSlugAndName(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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
			"action": "get_section", "id": id, "name": tc.name,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// SCR-BUG-37: When both slug AND name are present, name wins (canonical).
func TestSectionAlias_BothSlugAndNamePresent(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "Both Keys Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "winner", "slug": "loser", "text": "name should win"},
		},
	})

	id := extractID(t, text)
	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get_section", "id": id, "name": "winner",
	})
	if got != "name should win" {
		t.Errorf("name should take precedence over slug, got section 'winner' = %q", got)
	}

	// "loser" should NOT exist as a section
	loser := callTool(t, cs, "artifact", map[string]any{
		"action": "get_section", "id": id, "name": "loser",
	})
	if loser == "name should win" {
		t.Error("slug value should not create a separate section when name is present")
	}
}

// SCR-BUG-37: When both text AND body are present, text wins (canonical).
func TestSectionAlias_BothTextAndBodyPresent(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "Both Text Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "notes", "text": "text wins", "body": "body loses"},
		},
	})

	id := extractID(t, text)
	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get_section", "id": id, "name": "notes",
	})
	if got != "text wins" {
		t.Errorf("text should take precedence over body, got %q", got)
	}
}

// SCR-BUG-37: Sections with neither name nor slug are silently skipped.
func TestSectionAlias_NoNameNoSlug(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "spec",
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

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
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
		"action": "get_section", "id": id, "name": "valid",
	})
	if got != "this one counts" {
		t.Errorf("valid section should be stored, got %q", got)
	}
}

// SCR-BUG-37: Batch attach_section with slug aliases.
func TestSectionAlias_BatchAttachWithSlug(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create artifact first
	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "Batch Slug Task",
		"scope":  "test",
	})
	id := extractID(t, text)

	// Batch attach using slug+body
	callTool(t, cs, "artifact", map[string]any{
		"action": "attach_section",
		"id":     id,
		"sections": []map[string]string{
			{"slug": "design", "body": "design via slug"},
			{"slug": "notes", "body": "notes via slug"},
			{"name": "context", "text": "context via name"},
		},
	})

	for _, tc := range []struct{ name, want string }{
		{"design", "design via slug"},
		{"notes", "notes via slug"},
		{"context", "context via name"},
	} {
		got := callTool(t, cs, "artifact", map[string]any{
			"action": "get_section", "id": id, "name": tc.name,
		})
		if got != tc.want {
			t.Errorf("batch section %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// SCR-BUG-37: Stash created with name+text, promoted with slug+body additions.
func TestSectionAlias_CrossFormatStashPromote(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	// Create with only decision section using name+text — will fail template (missing MustSection "problem")
	ctx := context.Background()
	result, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action": "create",
			"kind":   "spec",
			"title":  "Cross Format Spec",
			"scope":  "test",
			"links":  map[string]any{"satisfies": []string{"SCR-TPL-1"}},
			"sections": []map[string]string{
				{"name": "decision", "text": "original decision"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected partial sections to fail template conformance")
	}

	var errMsg string
	for _, c := range result.Content {
		if tc, ok := c.(*sdkmcp.TextContent); ok {
			errMsg = tc.Text
			break
		}
	}
	stashIdx := strings.Index(errMsg, "stash_id=")
	if stashIdx < 0 {
		t.Fatalf("expected stash_id in error: %s", errMsg)
	}
	stashID := errMsg[stashIdx+len("stash_id="):]
	stashID = strings.TrimSuffix(stashID, "]")

	// Promote with the missing sections using slug+body format
	promoteResult, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "artifact",
		Arguments: map[string]any{
			"action":   "promote_stash",
			"stash_id": stashID,
			"sections": []map[string]string{
				{"slug": "problem", "body": "promoted problem"},
				{"slug": "acceptance", "body": "promoted acceptance"},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if promoteResult.IsError {
		var promoteErr string
		for _, c := range promoteResult.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				promoteErr = tc.Text
				break
			}
		}
		t.Fatalf("cross-format promote should merge name+slug sections: %s", promoteErr)
	}
}

// SCR-BUG-37: Patch map combined with slug-keyed sections — both paths contribute.
func TestSectionAlias_PatchAndSlugCombined(t *testing.T) {
	s := openStore(t)
	createMCPTemplate(t, s)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "spec",
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
			"action": "get_section", "id": id, "name": tc.name,
		})
		if got != tc.want {
			t.Errorf("section %q: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// SCR-BUG-37: Duplicate slugs in same request — last writer wins.
func TestSectionAlias_DuplicateSlug(t *testing.T) {
	s := openStore(t)

	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)

	text := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "Duplicate Slug Task",
		"scope":  "test",
		"sections": []map[string]string{
			{"slug": "notes", "body": "first"},
			{"slug": "notes", "body": "second"},
		},
	})

	id := extractID(t, text)
	got := callTool(t, cs, "artifact", map[string]any{
		"action": "get_section", "id": id, "name": "notes",
	})
	// Should have some value — either first or second, but not crash or empty
	if got == "" {
		t.Error("duplicate slug should still produce a stored section, got empty")
	}
}
