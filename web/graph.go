package web

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// These are the JSON types consumed by 3d-force-graph on the client.

// graphNode is a node in the 3d-force-graph payload.
type graphNode struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Scope      string `json:"scope"`
	Group      string `json:"group,omitempty"` // kind group for intermediate depth level
	Val        int    `json:"val"`             // sphere radius (degree-proportional)
	Violations int    `json:"violations"`      // compliance violation count; 0 = compliant
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

// Pure functions: no HTTP, no global state. Each receives a Protocol and
// returns graphData. Handlers are thin dispatchers over these builders.

// buildScopeGraph returns one node per scope and one link per cross-scope edge
// pair. Node Val is proportional to artifact count; link Weight is the number
// of cross-scope edges between the two scopes.
func buildScopeGraph(ctx context.Context, proto *parchment.Protocol) (graphData, error) {
	counts, weights, err := proto.Store().ScopeGraph(ctx)
	if err != nil {
		return graphData{}, err
	}
	nodes := make([]graphNode, 0, len(counts))
	for _, sc := range counts {
		if sc.Scope == "" || sc.Scope == parchment.SchemaScope || strings.HasPrefix(sc.Scope, "scribe-session") {
			continue
		}
		nodes = append(nodes, graphNode{
			ID: "scope:" + sc.Scope, Name: sc.Scope,
			Kind: "scope", Scope: sc.Scope,
			Val: max(3, sc.Count/20),
		})
	}
	links := make([]graphLink, 0, len(weights))
	for _, w := range weights {
		links = append(links, graphLink{
			Source: "scope:" + w.FromScope, Target: "scope:" + w.ToScope,
			Relation: "cross-scope", Weight: float64(w.Weight),
		})
	}
	return graphData{Nodes: nodes, Links: links}, nil
}

func buildKindGraph(ctx context.Context, proto *parchment.Protocol, scope string, statuses, relations []string) (graphData, error) {
	statusLabels := make([]string, 0, len(statuses))
	for _, st := range statuses {
		s := strings.TrimSpace(st)
		if parchment.IsDomainStatusLabel(s) {
			statusLabels = append(statusLabels, s)
		} else {
			statusLabels = append(statusLabels, parchment.LabelPrefixStatus+s)
		}
	}
	counts, weights, err := proto.Store().KindGraph(ctx, scope, statusLabels, relations)
	if err != nil {
		return graphData{}, err
	}
	nodes := make([]graphNode, 0, len(counts))
	for _, kc := range counts {
		nodes = append(nodes, graphNode{
			ID: "kind:" + scope + ":" + kc.Scope, Name: kc.Scope,
			Kind: "kind-group", Scope: scope, Group: kc.Scope,
			Val: max(2, kc.Count/5),
		})
	}
	nodeIDs := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeIDs[n.ID] = true
	}
	links := make([]graphLink, 0, len(weights))
	for _, w := range weights {
		src := "kind:" + scope + ":" + w.FromScope
		tgt := "kind:" + scope + ":" + w.ToScope
		if !nodeIDs[src] || !nodeIDs[tgt] {
			continue
		}
		links = append(links, graphLink{
			Source: src, Target: tgt,
			Relation: "cross-kind", Weight: float64(w.Weight),
		})
	}
	return graphData{Nodes: nodes, Links: links}, nil
}

// buildArtifactGraph returns individual artifact nodes and their edges,
// filtered by scope, statuses, and relation types.
func buildArtifactGraph(ctx context.Context, proto *parchment.Protocol, scope string, statuses, relations []string, maxNodes int) (graphData, error) {
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
			ID:         a.ID,
			Name:       a.Title,
			Kind:       a.Label(parchment.LabelPrefixKind),
			Status:     parchment.StatusFromLabels(a.Labels),
			Scope:      a.Label(parchment.LabelPrefixScope),
			Val:        degree[a.ID] + 1,
			Violations: violationCount(a),
		})
	}

	if maxNodes > 0 && len(nodes) > maxNodes {
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Val > nodes[j].Val })
		nodes = nodes[:maxNodes]
	}

	kept := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		kept[n.ID] = true
	}

	links := make([]graphLink, 0, len(edges))
	for _, e := range edges {
		if !kept[e.From] || !kept[e.To] {
			continue
		}
		links = append(links, graphLink{
			Source:   e.From,
			Target:   e.To,
			Relation: e.Relation,
			Weight:   e.Weight,
		})
	}

	return graphData{Nodes: nodes, Links: links}, nil
}

// fetchArtifacts fetches artifacts for a scope matching any of the given statuses.
func fetchArtifacts(ctx context.Context, proto *parchment.Protocol, scope string, statuses []string) ([]*parchment.Artifact, error) {
	labelsOr := make([]string, 0, len(statuses))
	for _, st := range statuses {
		s := strings.TrimSpace(st)
		if parchment.IsDomainStatusLabel(s) {
			labelsOr = append(labelsOr, s)
		} else {
			labelsOr = append(labelsOr, parchment.LabelPrefixStatus+s)
		}
	}
	labels := []string{}
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}
	return proto.ListArtifacts(ctx, parchment.ListInput{
		Labels:   labels,
		LabelsOr: labelsOr,
	})
}

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
	maxNodes, _ := strconv.Atoi(q.Get("max_nodes"))
	if maxNodes <= 0 {
		maxNodes = 2000
	}

	data, err := buildArtifactGraph(r.Context(), s.proto, scope, statuses, relations, maxNodes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

// handleAPIGraphLocal serves a local neighborhood graph rooted at one artifact.
// GET /api/v1/graph/local?id=&hops=2
func (s *Server) handleAPIGraphLocal(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("id")
	if id == "" {
		http.Error(w, "id parameter required", http.StatusBadRequest)
		return
	}
	hops, _ := strconv.Atoi(q.Get("hops"))
	if hops <= 0 {
		hops = 2
	}
	data, err := buildLocalGraph(r.Context(), s.proto, id, hops)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

func buildLocalGraph(ctx context.Context, proto *parchment.Protocol, rootID string, hops int) (graphData, error) {
	collected := make(map[string]*parchment.Artifact)
	var edges []parchment.Edge

	root, err := proto.GetArtifact(ctx, rootID)
	if err != nil {
		return graphData{}, err
	}
	collected[root.ID] = root

	frontier := []string{rootID}
	for depth := range hops {
		_ = depth
		var nextFrontier []string
		for _, id := range frontier {
			neighbors, _ := proto.Store().Neighbors(ctx, id, "", parchment.Both)
			for _, e := range neighbors {
				edges = append(edges, e)
				peerID := e.To
				if peerID == id {
					peerID = e.From
				}
				if _, ok := collected[peerID]; !ok {
					peer, err := proto.GetArtifact(ctx, peerID)
					if err != nil {
						continue
					}
					collected[peerID] = peer
					nextFrontier = append(nextFrontier, peerID)
				}
			}
		}
		frontier = nextFrontier
	}

	nodes := make([]graphNode, 0, len(collected))
	for _, a := range collected {
		nodes = append(nodes, graphNode{
			ID:     a.ID,
			Name:   a.Title,
			Kind:   a.Label(parchment.LabelPrefixKind),
			Status: parchment.StatusFromLabels(a.Labels),
			Scope:  a.Label(parchment.LabelPrefixScope),
			Val:    1,
		})
	}

	seen := make(map[string]bool)
	links := make([]graphLink, 0, len(edges))
	for _, e := range edges {
		if collected[e.From] == nil || collected[e.To] == nil {
			continue
		}
		key := e.From + "|" + e.Relation + "|" + e.To
		if seen[key] {
			continue
		}
		seen[key] = true
		links = append(links, graphLink{
			Source: e.From, Target: e.To,
			Relation: e.Relation, Weight: e.Weight,
		})
	}

	return graphData{Nodes: nodes, Links: links}, nil
}

func (s *Server) handleAPIGetArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.proto.GetArtifact(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, art)
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
	var raw struct {
		parchment.CreateInput
		Kind   string `json:"kind,omitempty"`
		Status string `json:"status,omitempty"`
		Scope  string `json:"scope,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	in := raw.CreateInput
	if raw.Kind != "" {
		in.Labels = append([]string{parchment.LabelPrefixKind + raw.Kind}, in.Labels...)
	}
	if raw.Status != "" {
		if parchment.IsDomainStatusLabel(raw.Status) {
			in.Labels = append(in.Labels, raw.Status)
		} else {
			in.Labels = append(in.Labels, parchment.LabelPrefixStatus+raw.Status)
		}
	}
	if raw.Scope != "" {
		in.Labels = append(in.Labels, parchment.LabelPrefixScope+raw.Scope)
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
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, "graph.html", map[string]any{tmplKeyTitle: "Graph", "Version": s.version})
}

const defaultStatuses = "work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted,cancelled,archived,active" //nolint:misspell // "cancelled" is the stored value

// violationCount returns the number of compliance violations on an artifact.
// 0 = compliant (compliance:ok label or no compliance label at all).
// N>0 = N distinct violations from extra["compliance_violations"].
// ViolationCount is exported for testing.
func ViolationCount(a *parchment.Artifact) int { return violationCount(a) }

func violationCount(a *parchment.Artifact) int {
	compliant := true
	for _, l := range a.Labels {
		if l == parchment.LabelPrefixCompliance+"violation" {
			compliant = false
		}
	}
	if compliant {
		return 0
	}
	// Count violations from Extra field.
	if viols, ok := a.Extra[parchment.ExtraKeyComplianceViolations]; ok {
		switch v := viols.(type) {
		case []any:
			return len(v)
		case []string:
			return len(v)
		}
	}
	return 1 // violation label present but no detail → count as 1
}

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
