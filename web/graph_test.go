package web_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/web"
)

// ── builder tests (pure functions via HTTP harness) ────────────────────────

func setupGraph(t *testing.T) *web.Server {
	t.Helper()
	dir := t.TempDir()
	s, err := parchment.OpenSQLite(dir + "/graph_test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()

	// Two scopes: alpha (2 tasks) and beta (1 spec).
	for _, art := range []*parchment.Artifact{
		{ID: "TSK-A1", Kind: "task", Scope: "alpha", Status: "active", Title: "Alpha task 1"},
		{ID: "TSK-A2", Kind: "task", Scope: "alpha", Status: "draft", Title: "Alpha task 2"},
		{ID: "SPC-B1", Kind: "spec", Scope: "beta", Status: "active", Title: "Beta spec 1"},
	} {
		if err := s.Put(ctx, art); err != nil {
			t.Fatal(err)
		}
	}
	// Cross-scope edge: TSK-A1 implements SPC-B1.
	if err := s.AddEdge(ctx, parchment.Edge{
		From: "TSK-A1", To: "SPC-B1", Relation: "implements",
	}); err != nil {
		t.Fatal(err)
	}
	// Intra-scope edge: TSK-A1 depends_on TSK-A2.
	if err := s.AddEdge(ctx, parchment.Edge{
		From: "TSK-A1", To: "TSK-A2", Relation: "depends_on",
	}); err != nil {
		t.Fatal(err)
	}

	proto := parchment.New(s, nil, []string{"alpha", "beta"}, nil, parchment.ProtocolConfig{})
	return web.NewServer(proto)
}

// ── /api/graph/scopes ─────────────────────────────────────────────────────

func TestAPIGraphScopes_ReturnsScopeNodes(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/graph/scopes", http.NoBody))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var data struct {
		Nodes []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"nodes"`
		Links []struct {
			Relation string `json:"relation"`
		} `json:"links"`
	}
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if len(data.Nodes) != 2 {
		t.Errorf("expected 2 scope nodes, got %d", len(data.Nodes))
	}
	for _, n := range data.Nodes {
		if n.Kind != "scope" {
			t.Errorf("node %s has kind %q, want scope", n.ID, n.Kind)
		}
		if !strings.HasPrefix(n.ID, "scope:") {
			t.Errorf("scope node ID should start with scope:, got %q", n.ID)
		}
	}
	if len(data.Links) != 1 {
		t.Errorf("expected 1 cross-scope link, got %d", len(data.Links))
	}
	if data.Links[0].Relation != "cross-scope" {
		t.Errorf("expected relation cross-scope, got %q", data.Links[0].Relation)
	}
}

func TestAPIGraphScopes_ExcludesSchemaScope(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/graph/scopes", http.NoBody))

	var data struct {
		Nodes []struct {
			Scope string `json:"scope"`
		} `json:"nodes"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures
	for _, n := range data.Nodes {
		if n.Scope == "_schema" {
			t.Error("_schema scope must not appear in scope graph")
		}
	}
}

// ── /api/graph/kinds ──────────────────────────────────────────────────────

func TestAPIGraphKinds_ReturnsKindNodes(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/graph/kinds?scope=alpha&status=active,draft", http.NoBody))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var data struct {
		Nodes []struct {
			Kind  string `json:"kind"`
			Group string `json:"group"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	// alpha scope has only tasks → 1 kind node
	if len(data.Nodes) != 1 {
		t.Errorf("expected 1 kind node (task), got %d", len(data.Nodes))
	}
	if data.Nodes[0].Kind != "kind-group" {
		t.Errorf("expected kind-group, got %q", data.Nodes[0].Kind)
	}
	if data.Nodes[0].Group != "task" {
		t.Errorf("expected group=task, got %q", data.Nodes[0].Group)
	}
}

func TestAPIGraphKinds_CrossKindLinks(t *testing.T) {
	srv := setupGraph(t)
	// beta scope only has spec — no cross-kind links within it.
	// Use "all" statuses to pick up both scopes separately.
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/graph/kinds?scope=alpha&status=active,draft&relations=depends_on", http.NoBody))

	var data struct {
		Links []any `json:"links"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures
	// TSK-A1 and TSK-A2 are both tasks — no cross-kind edge, so links should be empty.
	if len(data.Links) != 0 {
		t.Errorf("intra-kind edge should not appear in kind graph, got %d links", len(data.Links))
	}
}

// ── /api/graph ────────────────────────────────────────────────────────────

func TestAPIGraph_ReturnsArtifactNodes(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/graph?scope=alpha&status=active,draft", http.NoBody))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var data struct {
		Nodes []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"nodes"`
		Links []struct {
			Relation string `json:"relation"`
		} `json:"links"`
	}
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	if len(data.Nodes) != 2 {
		t.Errorf("expected 2 alpha artifacts, got %d", len(data.Nodes))
	}
	// depends_on is an intra-scope edge so it should appear.
	if len(data.Links) != 1 {
		t.Errorf("expected 1 intra-scope link, got %d", len(data.Links))
	}
}

func TestAPIGraph_DefaultStatusFilter(t *testing.T) {
	srv := setupGraph(t)
	// No status param — should use default active-work statuses.
	// TSK-A1 is active, TSK-A2 is draft — both should appear.
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/graph?scope=alpha", http.NoBody))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var data struct {
		Nodes []any `json:"nodes"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures
	if len(data.Nodes) < 1 {
		t.Error("default status filter should return active/draft artifacts")
	}
}

func TestAPIGraph_RelationFilter(t *testing.T) {
	srv := setupGraph(t)
	// Request only parent_of edges — depends_on edge should not appear.
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/graph?scope=alpha&status=active,draft&relations=parent_of", http.NoBody))

	var data struct {
		Links []any `json:"links"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures
	if len(data.Links) != 0 {
		t.Errorf("parent_of filter: expected 0 links (no parent_of edges exist), got %d", len(data.Links))
	}
}

// ── /api/scopes ───────────────────────────────────────────────────────────

func TestAPIScopes_ReturnsScopeList(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/scopes", http.NoBody))

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	var scopes []string
	if err := json.NewDecoder(w.Body).Decode(&scopes); err != nil {
		t.Fatal(err)
	}
	// alpha and beta exist; _schema must be absent.
	for _, s := range scopes {
		if s == "_schema" {
			t.Error("_schema must not appear in /api/scopes")
		}
	}
}

// ── write API ─────────────────────────────────────────────────────────────

func TestAPICreateArtifact_CreatesAndReturns(t *testing.T) {
	srv := setupGraph(t)
	body := `{"kind":"note","title":"new note","scope":"alpha"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var art struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(w.Body).Decode(&art); err != nil {
		t.Fatal(err)
	}
	if art.Title != "new note" {
		t.Errorf("expected title 'new note', got %q", art.Title)
	}
}

func TestAPICreateArtifact_BadJSON_Returns400(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/artifacts", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAPIPatchArtifact_SetsField(t *testing.T) {
	srv := setupGraph(t)
	body := `{"field":"title","value":"updated title"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/artifacts/TSK-A1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPICreateEdge_CreatesEdge(t *testing.T) {
	srv := setupGraph(t)
	body := `{"from":"TSK-A2","to":"SPC-B1","relation":"implements","weight":0}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/edges", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIDeleteEdge_DeletesEdge(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodDelete,
		"/api/edges/TSK-A1/implements/SPC-B1", http.NoBody))

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

// ── /fragments/artifacts/{id} ─────────────────────────────────────────────

func TestFragmentArtifact_ReturnsHTML(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fragments/artifacts/TSK-A1", http.NoBody))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content-type, got %q", ct)
	}
	if !strings.Contains(w.Body.String(), "Alpha task 1") {
		t.Error("fragment should contain artifact title")
	}
}

func TestFragmentArtifact_NotFound_Returns404(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/fragments/artifacts/NONEXISTENT", http.NoBody))

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
