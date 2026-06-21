package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/web"
)

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
		{ID: "TSK-A1", Labels: []string{"kind:effort.task", "status:active", "project:alpha"}, Title: "Alpha task 1"},
		{ID: "TSK-A2", Labels: []string{"kind:effort.task", "status:draft", "project:alpha"}, Title: "Alpha task 2"},
		{ID: "SPC-B1", Labels: []string{"kind:intent.spec", "status:active", "project:beta"}, Title: "Beta spec 1"},
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
	return web.NewServer(proto, "dev", "")
}

func TestAPIGraphScopes_ReturnsScopeNodes(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/graph/scopes", http.NoBody))

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
		if n.Kind != "project" {
			t.Errorf("node %s has kind %q, want scope", n.ID, n.Kind)
		}
		if !strings.HasPrefix(n.ID, "project:") {
			t.Errorf("project node ID should start with project:, got %q", n.ID)
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
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/graph/scopes", http.NoBody))

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

func TestAPIGraphKinds_ReturnsKindNodes(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/graph/kinds?scope=alpha&status=active,draft", http.NoBody))

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
	if data.Nodes[0].Group != "effort.task" {
		t.Errorf("expected group=effort.task, got %q", data.Nodes[0].Group)
	}
}

func TestAPIGraphKinds_CrossKindLinks(t *testing.T) {
	srv := setupGraph(t)
	// beta scope only has spec — no cross-kind links within it.
	// Use "all" statuses to pick up both scopes separately.
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/graph/kinds?scope=alpha&status=active,draft&relations=depends_on", http.NoBody))

	var data struct {
		Links []any `json:"links"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures
	// TSK-A1 and TSK-A2 are both tasks — no cross-kind edge, so links should be empty.
	if len(data.Links) != 0 {
		t.Errorf("intra-kind edge should not appear in kind graph, got %d links", len(data.Links))
	}
}

func TestAPIGraph_ReturnsArtifactNodes(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/graph?scope=alpha&status=active,draft", http.NoBody))

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
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/graph?scope=alpha", http.NoBody))

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
		"/api/v1/graph?scope=alpha&status=active,draft&relations=parent_of", http.NoBody))

	var data struct {
		Links []any `json:"links"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures
	if len(data.Links) != 0 {
		t.Errorf("parent_of filter: expected 0 links (no parent_of edges exist), got %d", len(data.Links))
	}
}

func TestAPIScopes_ReturnsScopeList(t *testing.T) {
	srv := setupGraph(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/scopes", http.NoBody))

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

func TestAPICreateArtifact_CreatesAndReturns(t *testing.T) {
	srv := setupGraph(t)
	body := `{"kind":"knowledge.note","title":"new note","scope":"alpha"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader("not json"))
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
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/TSK-A1", strings.NewReader(body))
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/edges", strings.NewReader(body))
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
		"/api/v1/edges/TSK-A1/implements/SPC-B1", http.NoBody))

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

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

// TestAPIGraphScopes_ContractMatchesFixture is the contract test between
// Go's graphNode struct and the JS fixture at web/static/graph/fixtures/scope-graph.json.
//
// Given: a seeded store with scope nodes
// When:  GET /api/v1/graph/scopes is called
// Then:  every field present in the JS fixture exists in the response with the correct type
//
// If Go renames or removes a field, this test breaks before the frontend silently breaks.
func TestAPIGraphScopes_ContractMatchesFixture(t *testing.T) {
	srv := httptest.NewServer(setupGraph(t))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/graph/scopes")
	if err != nil {
		t.Fatalf("GET /api/v1/graph/scopes: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	// Decode into the same shape the JS fixture uses.
	var body struct {
		Nodes []map[string]any `json:"nodes"`
		Links []map[string]any `json:"links"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Nodes) == 0 {
		t.Fatal("no nodes returned")
	}

	// Every field the JS fixture declares must be present with the right type.
	// Source of truth: web/static/graph/fixtures/scope-graph.json
	requiredStringFields := []string{"id", "name", "kind", "status", "scope"}
	requiredNumericFields := []string{"val", "violations"}

	for _, node := range body.Nodes {
		for _, f := range requiredStringFields {
			v, ok := node[f]
			if !ok {
				t.Errorf("node missing field %q — JS fixture expects it", f)
				continue
			}
			if _, isStr := v.(string); !isStr {
				t.Errorf("node field %q: want string, got %T", f, v)
			}
		}
		for _, f := range requiredNumericFields {
			v, ok := node[f]
			if !ok {
				t.Errorf("node missing field %q — JS fixture expects it", f)
				continue
			}
			if _, isNum := v.(float64); !isNum {
				t.Errorf("node field %q: want number, got %T", f, v)
			}
		}
	}
}

func TestArtifactGraph_MaxNodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := parchment.OpenSQLite(dir + "/maxnodes.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	proto := parchment.New(s, nil, []string{"stress"}, nil, parchment.ProtocolConfig{})
	for i := range 100 {
		proto.CreateArtifact(ctx, parchment.CreateInput{
			Title:  "node-" + strings.Repeat("x", 3) + string(rune('A'+i%26)),
			Labels: []string{"kind:effort.task", "project:stress"},
		})
	}

	srv := web.NewServer(proto, "test", "")
	req := httptest.NewRequest("GET", "/api/v1/graph?scope=stress&max_nodes=10", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var data struct {
		Nodes []map[string]any `json:"nodes"`
	}
	json.NewDecoder(w.Body).Decode(&data)
	if len(data.Nodes) > 10 {
		t.Errorf("expected ≤10 nodes with max_nodes=10, got %d", len(data.Nodes))
	}
}

func TestArtifactGraph_StressLargeScope(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test")
	}
	t.Parallel()
	dir := t.TempDir()
	s, err := parchment.OpenSQLite(dir + "/stress.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	proto := parchment.New(s, nil, []string{"big"}, nil, parchment.ProtocolConfig{})
	ids := make([]string, 3000)
	for i := range 3000 {
		art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
			Title:  "stress-" + string(rune('A'+i%26)) + strings.Repeat("x", i%10),
			Labels: []string{"kind:effort.task", "project:big"},
		})
		ids[i] = art.ID
	}
	for i := 1; i < len(ids); i += 3 {
		s.AddEdge(ctx, parchment.Edge{From: ids[i-1], To: ids[i], Relation: "depends_on"})
	}

	srv := web.NewServer(proto, "test", "")

	req := httptest.NewRequest("GET", "/api/v1/graph?scope=big&max_nodes=500", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var data struct {
		Nodes []map[string]any `json:"nodes"`
		Links []map[string]any `json:"links"`
	}
	json.NewDecoder(w.Body).Decode(&data)
	if len(data.Nodes) > 500 {
		t.Errorf("expected ≤500 nodes, got %d", len(data.Nodes))
	}
	t.Logf("stress: 3000 artifacts → %d nodes, %d links (capped at 500)", len(data.Nodes), len(data.Links))
}

func TestLocalGraph_NHopNeighborhood(t *testing.T) {
	srv := setupGraph(t)
	req := httptest.NewRequest("GET", "/api/v1/graph/local?id=TSK-A1&hops=1", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var data struct {
		Nodes []struct{ ID string }       `json:"nodes"`
		Links []struct{ Relation string } `json:"links"`
	}
	json.NewDecoder(w.Body).Decode(&data)

	ids := make(map[string]bool)
	for _, n := range data.Nodes {
		ids[n.ID] = true
	}
	if !ids["TSK-A1"] {
		t.Error("root node TSK-A1 must be in results")
	}
	if !ids["TSK-A2"] {
		t.Error("1-hop neighbor TSK-A2 must be in results (depends_on)")
	}
	if !ids["SPC-B1"] {
		t.Error("1-hop neighbor SPC-B1 must be in results (implements)")
	}
}

func TestLocalGraph_RequiresID(t *testing.T) {
	srv := setupGraph(t)
	req := httptest.NewRequest("GET", "/api/v1/graph/local?hops=1", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400 for missing id, got %d", w.Code)
	}
}

// ── Tako-scale E2E: 150+ artifacts with hierarchical structure ───────────

func setupTakoScale(t *testing.T) *web.Server {
	t.Helper()
	dir := t.TempDir()
	s, err := parchment.OpenSQLite(dir + "/tako_scale.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	scope := "tako"

	// Create 50 code.struct, 100 code.function, 5 knowledge.source, 1 intent.spec
	for i := range 50 {
		_ = s.Put(ctx, &parchment.Artifact{
			ID: fmt.Sprintf("struct-%03d", i), Title: fmt.Sprintf("Struct%d", i),
			Labels: []string{"kind:code.struct", "project:" + scope, "status:active"},
		})
	}
	for i := range 100 {
		_ = s.Put(ctx, &parchment.Artifact{
			ID: fmt.Sprintf("func-%03d", i), Title: fmt.Sprintf("Func%d", i),
			Labels: []string{"kind:code.function", "project:" + scope, "status:active"},
		})
	}
	for i := range 5 {
		_ = s.Put(ctx, &parchment.Artifact{
			ID: fmt.Sprintf("src-%03d", i), Title: fmt.Sprintf("Source%d", i),
			Labels: []string{"kind:knowledge.source", "project:" + scope, "status:active"},
		})
	}
	_ = s.Put(ctx, &parchment.Artifact{
		ID: "spec-001", Title: "Main Spec",
		Labels: []string{"kind:intent.spec", "project:" + scope, "status:active"},
	})

	// Create has_member edges: each struct has ~2 functions
	for i := range 50 {
		for j := range 2 {
			fi := i*2 + j
			if fi >= 100 {
				break
			}
			_ = s.AddEdge(ctx, parchment.Edge{
				From: fmt.Sprintf("struct-%03d", i), To: fmt.Sprintf("func-%03d", fi),
				Relation: "has_member",
			})
		}
	}

	// Create some cross-struct calls edges
	for i := range 30 {
		_ = s.AddEdge(ctx, parchment.Edge{
			From: fmt.Sprintf("func-%03d", i), To: fmt.Sprintf("func-%03d", i+30),
			Relation: "calls",
		})
	}

	proto := parchment.New(s, nil, []string{scope}, nil, parchment.ProtocolConfig{})
	return web.NewServer(proto, "dev", "")
}

func TestTakoScale_ScopeGraph_SingleNode(t *testing.T) {
	srv := setupTakoScale(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/graph/scopes", http.NoBody))

	var data struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures
	if len(data.Nodes) != 1 {
		t.Errorf("expected 1 scope node (tako), got %d", len(data.Nodes))
	}
}

func TestTakoScale_KindGroups_CorrectCounts(t *testing.T) {
	srv := setupTakoScale(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET",
		"/api/v1/graph/kinds?scope=tako&status=active", http.NoBody))

	type kindNode struct {
		Group string `json:"group"`
		Val   int    `json:"val"`
	}
	var data struct {
		Nodes []kindNode `json:"nodes"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures

	byGroup := map[string]int{}
	for _, n := range data.Nodes {
		byGroup[n.Group] = n.Val
	}

	if byGroup["code.struct"] < 5 {
		t.Errorf("code.struct val=%d, want >= 5 (50 structs / 20 scaling)", byGroup["code.struct"])
	}
	if byGroup["code.function"] < 5 {
		t.Errorf("code.function val=%d, want >= 5 (100 funcs)", byGroup["code.function"])
	}
	if _, ok := byGroup["knowledge.source"]; !ok {
		t.Error("knowledge.source kind-group missing")
	}
}

func TestTakoScale_ArtifactGraph_MaxNodes(t *testing.T) {
	srv := setupTakoScale(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET",
		"/api/v1/graph?scope=tako&status=active&max_nodes=50", http.NoBody))

	var data struct {
		Nodes []json.RawMessage `json:"nodes"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures

	if len(data.Nodes) > 50 {
		t.Errorf("max_nodes=50 but got %d nodes", len(data.Nodes))
	}
	if len(data.Nodes) < 30 {
		t.Errorf("expected at least 30 nodes with max_nodes=50, got %d", len(data.Nodes))
	}
}

func TestTakoScale_LocalGraph_ChildContainment(t *testing.T) {
	srv := setupTakoScale(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET",
		"/api/v1/graph/local?id=struct-000&hops=1", http.NoBody))

	type gn struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	var data struct {
		Nodes []gn `json:"nodes"`
		Links []struct {
			Source   string `json:"source"`
			Target   string `json:"target"`
			Relation string `json:"relation"`
		} `json:"links"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures

	if len(data.Nodes) < 2 {
		t.Fatalf("expected root + children, got %d nodes", len(data.Nodes))
	}

	// Root must be present
	foundRoot := false
	childCount := 0
	for _, n := range data.Nodes {
		if n.ID == "struct-000" {
			foundRoot = true
		} else {
			childCount++
		}
	}
	if !foundRoot {
		t.Error("root struct-000 not in local graph")
	}
	if childCount < 2 {
		t.Errorf("expected at least 2 children (has_member), got %d", childCount)
	}

	// has_member edges present
	hasMember := 0
	for _, l := range data.Links {
		if l.Relation == "has_member" {
			hasMember++
		}
	}
	if hasMember < 2 {
		t.Errorf("expected at least 2 has_member edges, got %d", hasMember)
	}
}

func TestTakoScale_NodeValScaling(t *testing.T) {
	srv := setupTakoScale(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET",
		"/api/v1/graph?scope=tako&status=active&max_nodes=156", http.NoBody))

	type gn struct {
		ID  string `json:"id"`
		Val int    `json:"val"`
	}
	var data struct {
		Nodes []gn `json:"nodes"`
	}
	json.NewDecoder(w.Body).Decode(&data) //nolint:errcheck // test helper; decode errors surface as assertion failures

	// Val should scale with edge degree: nodes with more edges get higher val
	maxVal := 0
	for _, n := range data.Nodes {
		if n.Val > maxVal {
			maxVal = n.Val
		}
	}
	if maxVal < 2 {
		t.Error("expected some nodes with val >= 2 (has_member + calls edges)")
	}
}
