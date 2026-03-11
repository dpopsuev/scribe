package mcp_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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

	srv, _ := scribemcp.NewServer(s, []string{"origami", "mos"}, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, []string{"origami"}, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, []string{"origami"}, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, []string{"mos"}, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, []string{"mos"}, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{})
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

	srv, _ := scribemcp.NewServer(s, nil, nil, protocol.IDConfig{})
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
