package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// ── Graph handlers — thin dispatchers over service.Build* functions ──────

func (s *Server) handleAPIGraphScopes(w http.ResponseWriter, r *http.Request) {
	data, err := service.BuildScopeGraph(r.Context(), s.svc)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

func (s *Server) handleAPIGraphKinds(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statuses, relations := parseFilters(q.Get("status"), q.Get("relations"))
	data, err := service.BuildKindGraph(r.Context(), s.svc, q.Get("scope"), statuses, relations)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

func (s *Server) handleAPIGraph(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statuses, relations := parseFilters(q.Get("status"), q.Get("relations"))
	maxNodes, _ := strconv.Atoi(q.Get("max_nodes"))
	if maxNodes <= 0 {
		maxNodes = 2000
	}
	data, err := service.BuildArtifactGraph(r.Context(), s.svc, q.Get("scope"), statuses, relations, maxNodes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

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
	data, err := service.BuildLocalGraph(r.Context(), s.svc, id, hops)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

// ── Non-graph API handlers ──────────────────────────────────────────────

func (s *Server) handleAPIArtifactEdges(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	outgoing, _ := s.proto.Store().Neighbors(r.Context(), id, "", parchment.Outgoing)
	incoming, _ := s.proto.Store().Neighbors(r.Context(), id, "", parchment.Incoming)

	type edgeJSON struct {
		From     string `json:"from"`
		To       string `json:"to"`
		Relation string `json:"relation"`
		Title    string `json:"title"`
		Kind     string `json:"kind"`
	}
	edges := make([]edgeJSON, 0, len(outgoing)+len(incoming))
	for _, e := range outgoing {
		title, kind := "", ""
		if art, err := s.proto.GetArtifact(r.Context(), e.To); err == nil {
			title = art.Title
			kind = art.Label(parchment.LabelPrefixKind)
		}
		edges = append(edges, edgeJSON{From: e.From, To: e.To, Relation: e.Relation, Title: title, Kind: kind})
	}
	for _, e := range incoming {
		title, kind := "", ""
		if art, err := s.proto.GetArtifact(r.Context(), e.From); err == nil {
			title = art.Title
			kind = art.Label(parchment.LabelPrefixKind)
		}
		edges = append(edges, edgeJSON{From: e.From, To: e.To, Relation: e.Relation, Title: title, Kind: kind})
	}
	writeJSON(w, edges)
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

func (s *Server) handleAPICreateEdge(w http.ResponseWriter, r *http.Request) {
	var e parchment.Edge
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := s.proto.LinkArtifacts(r.Context(), e.From, e.Relation, []string{e.To}, e.Weight); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, e)
}

func (s *Server) handleAPIDeleteEdge(w http.ResponseWriter, r *http.Request) {
	from := r.PathValue("from")
	rel := r.PathValue("relation")
	to := r.PathValue("to")
	if err := s.proto.Store().RemoveEdge(r.Context(), parchment.Edge{From: from, Relation: rel, To: to}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── HTML page handlers ──────────────────────────────────────────────────

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, s.webPath+"/static/graph-app/index.html")
}

func (s *Server) handleFragmentArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.proto.GetArtifact(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	funcMap := template.FuncMap{"renderMarkdown": renderMarkdown, "labelValue": labelValue}
	tmpl, err := template.New("fragment_artifact.html").Funcs(funcMap).ParseFiles(
		s.webPath + "/templates/fragment_artifact.html")
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, map[string]any{"Artifact": art}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────

func parseFilters(statusParam, relationsParam string) (statuses, relations []string) {
	if statusParam != "" {
		statuses = strings.Split(statusParam, ",")
	}
	if relationsParam != "" {
		relations = strings.Split(relationsParam, ",")
	}
	return
}

// ViolationCount returns the number of compliance violations on an artifact.
func ViolationCount(a *parchment.Artifact) int {
	return service.ViolationCount(a)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck,gosec // response write; error is non-actionable
}
