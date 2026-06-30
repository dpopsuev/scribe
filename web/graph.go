package web

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"
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
	const defaultMaxNodes = 2000
	maxNodes, _ := strconv.Atoi(q.Get("max_nodes"))
	if maxNodes <= 0 {
		maxNodes = defaultMaxNodes
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

func (s *Server) handleAPIGraphLens(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var spec parchment.LensSpec

	if cid := q.Get("context_id"); cid != "" {
		art, err := s.svc.Proto.GetArtifact(r.Context(), cid)
		if err != nil {
			http.Error(w, "lens context not found: "+err.Error(), http.StatusNotFound)
			return
		}
		parsed, err := parchment.LensSpecFromArtifact(art)
		if err != nil {
			http.Error(w, "invalid lens context: "+err.Error(), http.StatusBadRequest)
			return
		}
		spec = parsed
	} else {
		spec = parseLensFromQuery(q)
	}

	data, err := service.BuildLensGraph(r.Context(), s.svc, spec)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, data)
}

func parseLensFromQuery(q url.Values) parchment.LensSpec { //nolint:cyclop // flat sequence of independent field reads
	var spec parchment.LensSpec
	if v := q.Get("anchor"); v != "" {
		spec.Anchor = strings.Split(v, ",")
	}
	if v := q.Get("anchor_or"); v != "" {
		spec.AnchorOr = strings.Split(v, ",")
	}
	if v := q.Get("anchor_ids"); v != "" {
		spec.AnchorIDs = strings.Split(v, ",")
	}
	if v := q.Get("exclude"); v != "" {
		spec.Exclude = strings.Split(v, ",")
	}
	if v := q.Get("include"); v != "" {
		spec.Include = strings.Split(v, ",")
	}
	spec.ScoreBy = q.Get("score_by")
	spec.MaxDepth, _ = strconv.Atoi(q.Get("max_depth"))
	spec.Limit, _ = strconv.Atoi(q.Get("limit"))
	for _, tv := range q["traverse"] {
		parts := strings.SplitN(tv, ":", 3)
		rule := parchment.TraversalRule{Relation: parts[0]}
		if len(parts) > 1 {
			rule.Direction = parts[1]
		}
		if len(parts) > 2 {
			rule.MaxDepth, _ = strconv.Atoi(parts[2])
		}
		spec.Traverse = append(spec.Traverse, rule)
	}
	return spec
}

func (s *Server) handleAPILenses(w http.ResponseWriter, r *http.Request) {
	arts, err := s.svc.Proto.ListArtifacts(r.Context(), parchment.ListInput{
		Labels: []string{parchment.LabelPrefixKind + "knowledge.context"},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type lensInfo struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	var lenses []lensInfo
	for _, a := range arts {
		if a.Extra == nil {
			continue
		}
		if _, ok := a.Extra["lens_anchor"]; !ok {
			if _, ok2 := a.Extra["lens_anchor_or"]; !ok2 {
				continue
			}
		}
		lenses = append(lenses, lensInfo{ID: a.ID, Title: a.Title})
	}
	if lenses == nil {
		lenses = []lensInfo{}
	}
	writeJSON(w, lenses)
}

func (s *Server) handleAPICreateLens(w http.ResponseWriter, r *http.Request) {
	raw, err := json.Marshal(json.RawMessage(mustReadBody(r)))
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	op := service.Find("lens_create")
	if op == nil {
		http.Error(w, "lens_create op not found", http.StatusInternalServerError)
		return
	}
	out, err := op.Run(r.Context(), s.svc, raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"result": out})
}

func mustReadBody(r *http.Request) []byte {
	b, _ := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	return b
}

// ── Non-graph API handlers ──────────────────────────────────────────────

func (s *Server) handleAPIArtifactEdges(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	outgoing, _ := s.svc.Proto.Store().Neighbors(r.Context(), id, "", parchment.Outgoing)
	incoming, _ := s.svc.Proto.Store().Neighbors(r.Context(), id, "", parchment.Incoming)

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
		if art, err := s.svc.Proto.GetArtifact(r.Context(), e.To); err == nil {
			title = art.Title
			kind = art.Label(parchment.LabelPrefixKind)
		}
		edges = append(edges, edgeJSON{From: e.From, To: e.To, Relation: e.Relation, Title: title, Kind: kind})
	}
	for _, e := range incoming {
		title, kind := "", ""
		if art, err := s.svc.Proto.GetArtifact(r.Context(), e.From); err == nil {
			title = art.Title
			kind = art.Label(parchment.LabelPrefixKind)
		}
		edges = append(edges, edgeJSON{From: e.From, To: e.To, Relation: e.Relation, Title: title, Kind: kind})
	}
	writeJSON(w, edges)
}

func (s *Server) handleAPIGetArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.svc.Proto.GetArtifact(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, art)
}

func (s *Server) handleAPIScopes(w http.ResponseWriter, r *http.Request) {
	info, err := s.svc.Proto.Store().ListScopeInfo(r.Context())
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
	art, err := s.svc.Proto.CreateArtifact(r.Context(), in)
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
		Field    string              `json:"field"`
		Value    string              `json:"value"`
		Force    bool                `json:"force,omitempty"`
		Sections []parchment.Section `json:"sections,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body.Sections) > 0 {
		if err := s.svc.Proto.Store().PatchArtifact(r.Context(), id, parchment.ArtifactPatch{
			AppendSections: body.Sections,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"id": id, "sections_updated": len(body.Sections)}) //nolint:goconst // "id" is a JSON key, not worth a constant
		return
	}
	results, err := s.svc.Proto.SetField(r.Context(), []string{id}, body.Field, body.Value,
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
	if _, err := s.svc.Proto.LinkArtifacts(r.Context(), e.From, e.Relation, []string{e.To}, e.Weight); err != nil {
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
	if err := s.svc.Proto.Store().RemoveEdge(r.Context(), parchment.Edge{From: from, Relation: rel, To: to}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── HTML page handlers ──────────────────────────────────────────────────

func (s *Server) handleFragmentArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.svc.Proto.GetArtifact(r.Context(), id)
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

// ── Resolve — fetch live content from source backend ───────────────────

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	result, err := service.Resolve(r.Context(), s.svc, id, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, result)
}

// ── Debug perf ring buffer ─────────────────────────────────────────────
// Frontend POSTs per-frame perf data, CLI curls GET to read it.
// curl http://localhost:8083/api/v1/debug/perf

func (s *Server) handleDebugPerfPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	s.perfMu.Lock()
	s.perfSnap = json.RawMessage(body)
	s.perfMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDebugPerfGet(w http.ResponseWriter, _ *http.Request) {
	s.perfMu.Lock()
	snap := s.perfSnap
	s.perfMu.Unlock()
	if snap == nil {
		writeJSON(w, map[string]string{"status": "no data yet — open /app/graph first"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(snap) //nolint:errcheck,gosec // response write; error is non-actionable
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck,gosec // response write; error is non-actionable
}
