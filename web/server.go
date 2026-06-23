package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

const (
	tmplKeyTitle = "Title"
	logKeyMethod = "method"
	logKeyPath   = "path"
	logKeyStatus = "status"
	logKeyDur    = "dur"
)

var (
	httpRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scribe_http_requests_total",
		Help: "Total HTTP requests by method and path",
	}, []string{"method", "path"})
	httpRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "scribe_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

func init() {
	prometheus.MustRegister(httpRequestsTotal, httpRequestDuration)
}

var markdownParser = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

type Server struct {
	svc     *service.Service
	pages   map[string]*template.Template
	mux     *http.ServeMux
	version string
	webPath string // filesystem path to web/ directory (templates + static)

	perfMu   sync.Mutex
	perfSnap json.RawMessage // latest perf snapshot from frontend
}

// NewServer creates the UI server. webPath is the filesystem path to the web/
// directory containing templates/ and static/. Templates are parsed fresh on
// every request so changes take effect on browser refresh.
func NewServer(proto *parchment.Protocol, version, webPath string) *Server {
	if webPath == "" {
		webPath = "."
	}
	s := &Server{
		svc:     service.New(proto, nil, nil),
		pages:   make(map[string]*template.Template),
		version: version,
		webPath: webPath,
	}
	// Templates are parsed fresh on every request — see loadTemplate().

	s.mux = http.NewServeMux()
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(webPath+"/static"))))
	appDir := webPath + "/frontend/build"
	appFS := http.Dir(appDir)
	s.mux.Handle("GET /app/", http.StripPrefix("/app", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try the exact file first; fall back to index.html for SPA routing.
		if _, err := appFS.Open(r.URL.Path); err == nil {
			http.FileServer(appFS).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, appDir+"/index.html")
	})))

	// Read-only pages
	s.mux.HandleFunc("GET /", s.handleDashboard)
	s.mux.HandleFunc("GET /artifacts", s.handleList)
	s.mux.HandleFunc("GET /artifacts/{id}", s.handleDetail)
	s.mux.HandleFunc("GET /tree/{id}", s.handleTree)
	s.mux.HandleFunc("GET /search", s.handleSearch)
	// /graph route removed — graph lives at /app/graph (SvelteKit)
	s.mux.HandleFunc("GET /events", s.handleEvents)

	// Fragment endpoints (HTMX sidebar loads)
	s.mux.HandleFunc("GET /fragments/artifacts/{id}", s.handleFragmentArtifact)

	// JSON API — read (more-specific routes first)
	// Dataset export (streaming JSONL)
	s.mux.HandleFunc("GET /api/v1/export/dataset", s.handleExportDataset)

	// JSON API v1 — artifact read
	s.mux.HandleFunc("GET /api/v1/artifacts/{id}/edges", s.handleAPIArtifactEdges)
	s.mux.HandleFunc("GET /api/v1/artifacts/{id}", s.handleAPIGetArtifact)
	s.mux.HandleFunc("GET /api/v1/artifacts", s.handleAPIListArtifacts)
	s.mux.HandleFunc("GET /api/v1/graph/local", s.handleAPIGraphLocal)

	// JSON API v1 — graph read
	s.mux.HandleFunc("GET /api/v1/graph/scopes", s.handleAPIGraphScopes)
	s.mux.HandleFunc("GET /api/v1/graph/kinds", s.handleAPIGraphKinds)
	s.mux.HandleFunc("GET /api/v1/graph/lens", s.handleAPIGraphLens)
	s.mux.HandleFunc("GET /api/v1/graph", s.handleAPIGraph)
	s.mux.HandleFunc("GET /api/v1/lenses", s.handleAPILenses)
	s.mux.HandleFunc("POST /api/v1/lenses", s.handleAPICreateLens)
	s.mux.HandleFunc("GET /api/v1/scopes", s.handleAPIScopes)
	s.mux.HandleFunc("GET /api/v1/schema/hierarchy", s.handleAPISchemaHierarchy)

	// Resolve — fetch live content from source backend on demand
	s.mux.HandleFunc("GET /api/v1/artifacts/{id}/resolve", s.handleResolve)

	// Debug perf ring buffer — frontend POSTs frames, CLI curls GET
	s.mux.HandleFunc("POST /api/v1/debug/perf", s.handleDebugPerfPost)
	s.mux.HandleFunc("GET /api/v1/debug/perf", s.handleDebugPerfGet)

	// JSON API v1 — write
	s.mux.HandleFunc("POST /api/v1/ingest", s.handleAPIIngest)
	s.mux.HandleFunc("POST /api/v1/artifacts", s.handleAPICreateArtifact)
	s.mux.HandleFunc("PATCH /api/v1/artifacts/{id}", s.handleAPIPatchArtifact)
	s.mux.HandleFunc("POST /api/v1/edges", s.handleAPICreateEdge)
	s.mux.HandleFunc("DELETE /api/v1/edges/{from}/{relation}/{to}", s.handleAPIDeleteEdge)

	// Prometheus metrics
	s.mux.Handle("GET /metrics", promhttp.Handler())

	// Health probes (Kubernetes liveness/readiness)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if _, err := proto.Store().List(r.Context(), parchment.Filter{Limit: 1}); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "not ready: %v", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ready")
	})

	// Legacy /api/* → /api/v1/* (301 permanent redirect).
	// Preserves query string so old bookmarks and curl scripts keep working.
	legacyRedirect := func(w http.ResponseWriter, r *http.Request) {
		target := "/api/v1" + r.URL.Path[4:] // "/api/x" → "/api/v1/x"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently) //nolint:gosec // G710: target is built from fixed prefix + request path, not user-controlled input
	}
	for _, pattern := range []string{
		"GET /api/graph/scopes", "GET /api/graph/kinds", "GET /api/graph",
		"GET /api/scopes",
		"POST /api/artifacts", "PATCH /api/artifacts/{id}",
		"POST /api/edges", "DELETE /api/edges/{from}/{relation}/{to}",
	} {
		s.mux.HandleFunc(pattern, legacyRedirect)
	}

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if slog.Default().Enabled(r.Context(), slog.LevelDebug) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		s.mux.ServeHTTP(rw, r)
		slog.DebugContext(r.Context(), "http",
			slog.String(logKeyMethod, r.Method),
			slog.String(logKeyPath, r.URL.Path),
			slog.Int(logKeyStatus, rw.status),
			slog.Duration(logKeyDur, time.Since(start)),
		)
		return
	}
	s.mux.ServeHTTP(w, r)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	inv, err := s.svc.Inventory(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "dashboard.html", map[string]any{
		tmplKeyTitle: "Dashboard",
		"Inventory":  inv,
	})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()
	var listLabels []string
	var kindPrefix string
	if kind := queryParams.Get("kind"); kind != "" {
		if strings.Contains(kind, ".") {
			listLabels = append(listLabels, parchment.LabelPrefixKind+kind)
		} else {
			kindPrefix = kind
		}
	}
	if status := queryParams.Get("status"); status != "" {
		if parchment.IsDomainStatusLabel(status) {
			listLabels = append(listLabels, status)
		} else {
			listLabels = append(listLabels, parchment.LabelPrefixStatus+status)
		}
	}
	if sc := queryParams.Get("scope"); sc != "" {
		listLabels = append(listLabels, parchment.LabelPrefixScope+sc)
	}
	in := parchment.ListInput{
		Labels:     listLabels,
		KindPrefix: kindPrefix,
	}
	arts, err := s.svc.Proto.ListArtifacts(r.Context(), in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "list.html", map[string]any{
		tmplKeyTitle:  "Artifacts",
		"Artifacts":   arts,
		"Filter":      in,
		"ScopeFilter": queryParams.Get("scope"),
	})
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.svc.Proto.GetArtifact(r.Context(), id)
	if err != nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	data := map[string]any{
		tmplKeyTitle: art.Title,
		"Artifact":   art,
	}
	if backend := service.RefBackend(art); backend != "" {
		data["RefBackend"] = backend
		data["RefID"] = service.RefID(art)
		data["Stale"] = service.IsStale(art)
	}
	s.render(w, "detail.html", data)
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tree, err := s.svc.Proto.ArtifactTree(r.Context(), parchment.TreeInput{ID: id})
	if err != nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	s.render(w, "tree.html", map[string]any{
		tmplKeyTitle: "Tree: " + id,
		"Root":       tree,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("q")
	var results []*parchment.Artifact
	if searchQuery != "" {
		var err error
		results, err = s.svc.Proto.SearchArtifacts(r.Context(), searchQuery, parchment.ListInput{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.render(w, "search.html", map[string]any{
		tmplKeyTitle: "Search",
		"Query":      searchQuery,
		"Results":    results,
	})
}

func (s *Server) loadTemplate(name string) (*template.Template, error) {
	funcMap := template.FuncMap{
		"renderMarkdown": renderMarkdown,
		"labelValue":     labelValue,
	}
	layout, err := template.New("layout.html").Funcs(funcMap).ParseFiles(s.webPath + "/templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("parse layout: %w", err)
	}
	clone, err := layout.Clone()
	if err != nil {
		return nil, err
	}
	return clone.ParseFiles(s.webPath + "/templates/" + name)
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	tmpl, err := s.loadTemplate(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func labelValue(labels []string, prefix string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}

func renderMarkdown(text string) template.HTML {
	var buf bytes.Buffer
	if err := markdownParser.Convert([]byte(text), &buf); err != nil {
		return template.HTML("<pre>" + template.HTMLEscapeString(text) + "</pre>") //nolint:gosec // G203: content is HTMLEscapeString-escaped; not raw user input
	}
	out := buf.String()
	out = convertMermaidBlocks(out)
	return template.HTML(out) //nolint:gosec // G203: output is goldmark-rendered markdown, not user-controlled raw HTML
}

func convertMermaidBlocks(s string) string {
	const openTag = `<code class="language-mermaid">`
	const closeTag = `</code>`

	result := s
	for {
		idx := strings.Index(result, openTag)
		if idx < 0 {
			break
		}
		preStart := strings.LastIndex(result[:idx], "<pre>")
		end := strings.Index(result[idx:], closeTag)
		if end < 0 || preStart < 0 {
			break
		}
		end += idx + len(closeTag)
		preEnd := strings.Index(result[end:], "</pre>")
		if preEnd < 0 {
			break
		}
		preEnd += end + len("</pre>")

		mermaidContent := result[idx+len(openTag) : end-len(closeTag)]
		replacement := `<pre class="mermaid">` + mermaidContent + `</pre>`
		result = result[:preStart] + replacement + result[preEnd:]
	}

	return result
}

// handleEvents serves the EventLog change feed as SSE.
// GET /events?since=<RFC3339>[&scope=<scope>][&artifact_id=<id>]
// Each event is written as: data: <JSON>\n\n
// The feed is a batch dump: events since the cursor, then close.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	sinceStr := r.URL.Query().Get("since")
	if sinceStr == "" {
		http.Error(w, "since parameter is required (RFC3339 timestamp)", http.StatusBadRequest)
		return
	}
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid since: %v", err), http.StatusBadRequest)
		return
	}
	filter := parchment.EventFilter{
		Scope:      r.URL.Query().Get("scope"),
		ArtifactID: r.URL.Query().Get("artifact_id"),
	}
	if et := r.URL.Query().Get("event_type"); et != "" {
		filter.EventTypes = []string{et}
	}

	events, err := s.svc.Proto.GetEvents(r.Context(), since, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for _, event := range events {
		data, jsonErr := json.Marshal(event)
		if jsonErr != nil {
			continue
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
}

func (s *Server) handleAPIListArtifacts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var labels []string
	if kind := q.Get("kind"); kind != "" {
		labels = append(labels, parchment.LabelPrefixKind+kind)
	}
	if scope := q.Get("scope"); scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}
	if status := q.Get("status"); status != "" {
		if parchment.IsDomainStatusLabel(status) {
			labels = append(labels, status)
		} else {
			labels = append(labels, parchment.LabelPrefixStatus+status)
		}
	}
	var kindPrefix string
	if kp := q.Get("kind_prefix"); kp != "" {
		kindPrefix = kp
	}
	arts, err := s.svc.Proto.ListArtifacts(r.Context(), parchment.ListInput{
		Labels: labels, KindPrefix: kindPrefix,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type cardJSON struct {
		ID     string  `json:"id"`
		Title  string  `json:"title"`
		Kind   string  `json:"kind"`
		Status string  `json:"status"`
		Scope  string  `json:"scope"`
		Score  float64 `json:"score"`
	}
	cards := make([]cardJSON, len(arts))
	for i, a := range arts {
		cards[i] = cardJSON{
			ID:     a.ID,
			Title:  a.Title,
			Kind:   a.Label(parchment.LabelPrefixKind),
			Status: parchment.StatusFromLabels(a.Labels),
			Scope:  a.Label(parchment.LabelPrefixScope),
			Score:  s.svc.Proto.CompletionScore(r.Context(), a),
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cards)
}
