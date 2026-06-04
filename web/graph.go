package web

import (
	"encoding/json"
	"net/http"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// graphNode is the node format expected by 3d-force-graph.
type graphNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Scope  string `json:"scope"`
	Val    int    `json:"val"` // controls sphere size (degree)
}

// graphLink is the link format expected by 3d-force-graph.
type graphLink struct {
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// graphData is the JSON payload returned by GET /api/graph.
type graphData struct {
	Nodes []graphNode `json:"nodes"`
	Links []graphLink `json:"links"`
}

// handleAPIGraph serves the graph data for 3d-force-graph.
// GET /api/graph?scope=&status=&relations=
func (s *Server) handleAPIGraph(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	scope := q.Get("scope")
	statusFilter := q.Get("status") // comma-separated, default active+draft
	relFilter := q.Get("relations")

	if statusFilter == "" {
		statusFilter = "active,draft,current,proposed,in_progress,in_review,fleeting"
	}
	statuses := strings.Split(statusFilter, ",")

	var relations []string
	if relFilter != "" {
		relations = strings.Split(relFilter, ",")
	}

	ctx := r.Context()

	// Fetch all matching artifacts across requested statuses.
	var arts []*parchment.Artifact
	for _, st := range statuses {
		batch, err := s.proto.ListArtifacts(ctx, parchment.ListInput{
			Scope:  scope,
			Status: strings.TrimSpace(st),
		})
		if err != nil {
			continue
		}
		arts = append(arts, batch...)
	}

	// Build id set for edge filtering.
	ids := make([]string, 0, len(arts))
	idSet := make(map[string]bool, len(arts))
	for _, a := range arts {
		ids = append(ids, a.ID)
		idSet[a.ID] = true
	}

	// Fetch edges between these artifacts.
	edges, _ := s.proto.Store().ListEdges(ctx, ids, relations)

	// Build degree map for node sizing.
	degree := make(map[string]int, len(ids))
	for _, e := range edges {
		degree[e.From]++
		degree[e.To]++
	}

	// Assemble response.
	nodes := make([]graphNode, 0, len(arts))
	for _, a := range arts {
		nodes = append(nodes, graphNode{
			ID:     a.ID,
			Name:   a.Title,
			Kind:   a.ResolvedKind(),
			Status: a.ResolvedStatus(),
			Scope:  a.ResolvedScope(),
			Val:    degree[a.ID] + 1, // min 1 so zero-degree nodes are visible
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

	writeJSON(w, graphData{Nodes: nodes, Links: links})
}

// handleAPIScopes returns the distinct scopes present in the store.
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
// Body: {"field": "status", "value": "active"}
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

// handleFragmentArtifact serves a stripped artifact detail for the sidebar.
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
	s.render(w, "graph.html", map[string]any{
		"Title": "Graph",
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "")
	_ = enc.Encode(v) //nolint:errcheck // write errors are not actionable server-side
}
