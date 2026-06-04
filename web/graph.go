package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// ── graph wire types ────────────────────────────────────────────────────────
// These are the JSON types consumed by 3d-force-graph on the client.

// graphNode is a node in the 3d-force-graph payload.
type graphNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
	Group  string `json:"group,omitempty"` // kind group for intermediate depth level
	Val    int    `json:"val"`             // sphere radius (degree-proportional)
}

// graphLink is an edge in the 3d-force-graph payload.
type graphLink struct {
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// graphData is the full payload returned by all /api/graph* endpoints.
type graphData struct {
	Nodes []graphNode `json:"nodes"`
	Links []graphLink `json:"links"`
}

// ── graph builders ──────────────────────────────────────────────────────────
// Pure functions: no HTTP, no global state. Each receives a Protocol and
// returns graphData. Handlers are thin dispatchers over these builders.

// buildScopeGraph returns one node per scope and one link per cross-scope edge
// pair. Node Val is proportional to artifact count; link Weight is the number
// of cross-scope edges between the two scopes.
func buildScopeGraph(ctx context.Context, proto *parchment.Protocol) (graphData, error) {
	allArts, err := proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return graphData{}, err
	}

	scopeOf := make(map[string]string, len(allArts))
	countByScope := make(map[string]int)
	ids := make([]string, 0, len(allArts))
	for _, a := range allArts {
		sc := a.Scope
		if sc == "" || sc == parchment.SchemaScope {
			continue
		}
		scopeOf[a.ID] = sc
		countByScope[sc]++
		ids = append(ids, a.ID)
	}

	edges, _ := proto.Store().ListEdges(ctx, ids, nil)

	type edgeKey struct{ from, to string }
	weight := make(map[edgeKey]float64)
	for _, e := range edges {
		fs, ts := scopeOf[e.From], scopeOf[e.To]
		if fs == "" || ts == "" || fs == ts {
			continue
		}
		// Canonical direction: lexicographic min → max avoids double-counting.
		if fs > ts {
			fs, ts = ts, fs
		}
		weight[edgeKey{fs, ts}]++
	}

	nodes := make([]graphNode, 0, len(countByScope))
	for scope, count := range countByScope {
		nodes = append(nodes, graphNode{
			ID:    "scope:" + scope,
			Name:  scope,
			Kind:  "scope",
			Scope: scope,
			Val:   max(3, count/20),
		})
	}

	links := make([]graphLink, 0, len(weight))
	for ek, w := range weight {
		links = append(links, graphLink{
			Source:   "scope:" + ek.from,
			Target:   "scope:" + ek.to,
			Relation: "cross-scope",
			Weight:   w,
		})
	}

	return graphData{Nodes: nodes, Links: links}, nil
}

// buildKindGraph returns one node per kind within a scope and links between
// kinds based on cross-kind edges. Used as the intermediate depth level
// between scope super-nodes and individual artifact nodes.
func buildKindGraph(ctx context.Context, proto *parchment.Protocol, scope string, statuses, relations []string) (graphData, error) {
	arts, err := fetchArtifacts(ctx, proto, scope, statuses)
	if err != nil {
		return graphData{}, err
	}

	kindOf := make(map[string]string, len(arts))
	countByKind := make(map[string]int)
	ids := make([]string, 0, len(arts))
	for _, a := range arts {
		k := a.ResolvedKind()
		kindOf[a.ID] = k
		countByKind[k]++
		ids = append(ids, a.ID)
	}

	edges, _ := proto.Store().ListEdges(ctx, ids, relations)

	type edgeKey struct{ from, to string }
	weight := make(map[edgeKey]float64)
	for _, e := range edges {
		fk, tk := kindOf[e.From], kindOf[e.To]
		if fk == "" || tk == "" || fk == tk {
			continue
		}
		if fk > tk {
			fk, tk = tk, fk
		}
		weight[edgeKey{fk, tk}]++
	}

	nodes := make([]graphNode, 0, len(countByKind))
	for kind, count := range countByKind {
		nodes = append(nodes, graphNode{
			ID:    "kind:" + scope + ":" + kind,
			Name:  kind,
			Kind:  "kind-group",
			Scope: scope,
			Group: kind,
			Val:   max(2, count/5),
		})
	}

	links := make([]graphLink, 0, len(weight))
	for ek, w := range weight {
		links = append(links, graphLink{
			Source:   "kind:" + scope + ":" + ek.from,
			Target:   "kind:" + scope + ":" + ek.to,
			Relation: "cross-kind",
			Weight:   w,
		})
	}

	return graphData{Nodes: nodes, Links: links}, nil
}

// buildArtifactGraph returns individual artifact nodes and their edges,
// filtered by scope, statuses, and relation types.
func buildArtifactGraph(ctx context.Context, proto *parchment.Protocol, scope string, statuses, relations []string) (graphData, error) {
	arts, err := fetchArtifacts(ctx, proto, scope, statuses)
	if err != nil {
		return graphData{}, err
	}

	ids := make([]string, 0, len(arts))
	for _, a := range arts {
		ids = append(ids, a.ID)
	}

	edges, _ := proto.Store().ListEdges(ctx, ids, relations)

	degree := make(map[string]int, len(ids))
	for _, e := range edges {
		degree[e.From]++
		degree[e.To]++
	}

	nodes := make([]graphNode, 0, len(arts))
	for _, a := range arts {
		nodes = append(nodes, graphNode{
			ID:     a.ID,
			Name:   a.Title,
			Kind:   a.ResolvedKind(),
			Status: a.ResolvedStatus(),
			Scope:  a.ResolvedScope(),
			Val:    degree[a.ID] + 1,
		})
	}

	links := make([]graphLink, 0, len(edges))
	for _, e := range edges {
		links = append(links, graphLink{
			Source:   e.From,
			Target:   e.To,
			Relation: e.Relation,
			Weight:   e.Weight,
		})
	}

	return graphData{Nodes: nodes, Links: links}, nil
}

// fetchArtifacts fetches artifacts for a scope across multiple statuses.
// Each status is queried separately because ListArtifacts only accepts one.
func fetchArtifacts(ctx context.Context, proto *parchment.Protocol, scope string, statuses []string) ([]*parchment.Artifact, error) {
	var all []*parchment.Artifact
	for _, st := range statuses {
		batch, err := proto.ListArtifacts(ctx, parchment.ListInput{
			Scope:  scope,
			Status: strings.TrimSpace(st),
		})
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
	}
	return all, nil
}

// ── HTTP handlers ───────────────────────────────────────────────────────────
// Handlers are thin: parse request → call builder → write JSON.

// handleAPIGraphScopes serves the scope-level graph (universe view).
// GET /api/graph/scopes
func (s *Server) handleAPIGraphScopes(w http.ResponseWriter, r *http.Request) {
	data, err := buildScopeGraph(r.Context(), s.proto)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

// handleAPIGraphKinds serves the kind-level graph (intermediate depth).
// GET /api/graph/kinds?scope=&status=&relations=
func (s *Server) handleAPIGraphKinds(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope := q.Get("scope")
	statuses, relations := parseFilters(q.Get("status"), q.Get("relations"))

	data, err := buildKindGraph(r.Context(), s.proto, scope, statuses, relations)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

// handleAPIGraph serves the artifact-level graph (deepest view).
// GET /api/graph?scope=&status=&relations=
func (s *Server) handleAPIGraph(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope := q.Get("scope")
	statuses, relations := parseFilters(q.Get("status"), q.Get("relations"))

	data, err := buildArtifactGraph(r.Context(), s.proto, scope, statuses, relations)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

// handleAPIScopes returns the distinct non-schema scopes present in the store.
// GET /api/scopes
func (s *Server) handleAPIScopes(w http.ResponseWriter, r *http.Request) {
	info, err := s.proto.Store().ListScopeInfo(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	scopes := make([]string, 0, len(info))
	for _, si := range info {
		if si.Scope != parchment.SchemaScope {
			scopes = append(scopes, si.Scope)
		}
	}
	writeJSON(w, scopes)
}

// handleAPICreateArtifact handles POST /api/artifacts.
func (s *Server) handleAPICreateArtifact(w http.ResponseWriter, r *http.Request) {
	var in parchment.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	art, err := s.proto.CreateArtifact(r.Context(), in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, art)
}

// handleAPIPatchArtifact handles PATCH /api/artifacts/{id}.
// Body: {"field": "status", "value": "active", "force": false}
func (s *Server) handleAPIPatchArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Field string `json:"field"`
		Value string `json:"value"`
		Force bool   `json:"force,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	results, err := s.proto.SetField(r.Context(), []string{id}, body.Field, body.Value,
		parchment.SetFieldOptions{Force: body.Force})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(results) > 0 && !results[0].OK {
		http.Error(w, results[0].Error, http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, map[string]string{"id": id, "field": body.Field, "value": body.Value})
}

// handleAPICreateEdge handles POST /api/edges.
// Body: {"from": "TSK-1", "to": "SPEC-2", "relation": "implements", "weight": 0}
func (s *Server) handleAPICreateEdge(w http.ResponseWriter, r *http.Request) {
	var e parchment.Edge
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	results, err := s.proto.LinkArtifacts(r.Context(), e.From, e.Relation, []string{e.To}, e.Weight)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(results) > 0 && !results[0].OK {
		http.Error(w, results[0].Error, http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, e)
}

// handleAPIDeleteEdge handles DELETE /api/edges/{from}/{relation}/{to}.
func (s *Server) handleAPIDeleteEdge(w http.ResponseWriter, r *http.Request) {
	from := r.PathValue("from")
	relation := r.PathValue("relation")
	to := r.PathValue("to")
	if err := s.proto.Store().RemoveEdge(r.Context(), parchment.Edge{
		From: from, Relation: relation, To: to,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFragmentArtifact serves a stripped artifact detail for HTMX sidebar loads.
// GET /fragments/artifacts/{id}
func (s *Server) handleFragmentArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.proto.GetArtifact(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	tmpl, ok := s.pages["fragment_artifact.html"]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, map[string]any{"Artifact": art}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleGraph serves the graph explorer page.
// GET /graph
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	s.render(w, "graph.html", map[string]any{"Title": "Graph"})
}


// ── helpers ─────────────────────────────────────────────────────────────────

const defaultStatuses = "active,draft,current,proposed,in_progress,in_review,fleeting"

// parseFilters splits comma-separated status and relation query params.
// An empty status string falls back to the default active-work set.
func parseFilters(statusParam, relationsParam string) (statuses, relations []string) {
	if statusParam == "" {
		statusParam = defaultStatuses
	}
	statuses = strings.Split(statusParam, ",")
	if relationsParam != "" {
		relations = strings.Split(relationsParam, ",")
	}
	return statuses, relations
}

// writeJSON encodes v as JSON and writes it to w with the correct Content-Type.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "")
	_ = enc.Encode(v) //nolint:errcheck // network write errors are not actionable server-side
}
