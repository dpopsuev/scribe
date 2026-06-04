package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed templates/*.html
var templateFS embed.FS

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithUnsafe()),
)

type Server struct {
	proto *parchment.Protocol
	svc   *service.Service
	pages map[string]*template.Template
	mux   *http.ServeMux
}

func NewServer(proto *parchment.Protocol) *Server {
	s := &Server{
		proto: proto,
		svc:   service.New(proto, nil, nil),
		pages: make(map[string]*template.Template),
	}

	funcMap := template.FuncMap{
		"renderMarkdown": renderMarkdown,
	}

	layoutTmpl := template.Must(
		template.New("layout.html").Funcs(funcMap).ParseFS(templateFS, "templates/layout.html"),
	)

	for _, page := range []string{
		"dashboard.html", "list.html", "detail.html",
		"tree.html", "search.html",
	} {
		clone := template.Must(layoutTmpl.Clone())
		s.pages[page] = template.Must(clone.ParseFS(templateFS, "templates/"+page))
	}

	// fragment templates render without the layout wrapper
	fragmentTmpl := template.Must(
		template.New("fragment_artifact.html").Funcs(funcMap).ParseFS(
			templateFS, "templates/fragment_artifact.html"),
	)
	s.pages["fragment_artifact.html"] = fragmentTmpl

	// graph template needs layout but also uses full viewport — register after loop
	graphClone := template.Must(layoutTmpl.Clone())
	s.pages["graph.html"] = template.Must(graphClone.ParseFS(templateFS, "templates/graph.html"))

	s.mux = http.NewServeMux()

	// Read-only pages
	s.mux.HandleFunc("GET /", s.handleDashboard)
	s.mux.HandleFunc("GET /artifacts", s.handleList)
	s.mux.HandleFunc("GET /artifacts/{id}", s.handleDetail)
	s.mux.HandleFunc("GET /tree/{id}", s.handleTree)
	s.mux.HandleFunc("GET /search", s.handleSearch)
	s.mux.HandleFunc("GET /graph", s.handleGraph)
	s.mux.HandleFunc("GET /events", s.handleEvents)

	// Fragment endpoints (HTMX sidebar loads)
	s.mux.HandleFunc("GET /fragments/artifacts/{id}", s.handleFragmentArtifact)

	// JSON API — read
	s.mux.HandleFunc("GET /api/graph", s.handleAPIGraph)
	s.mux.HandleFunc("GET /api/scopes", s.handleAPIScopes)

	// JSON API — write
	s.mux.HandleFunc("POST /api/artifacts", s.handleAPICreateArtifact)
	s.mux.HandleFunc("PATCH /api/artifacts/{id}", s.handleAPIPatchArtifact)
	s.mux.HandleFunc("POST /api/edges", s.handleAPICreateEdge)
	s.mux.HandleFunc("DELETE /api/edges/{from}/{relation}/{to}", s.handleAPIDeleteEdge)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	inv, err := s.svc.Inventory(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "dashboard.html", map[string]any{
		"Title":     "Dashboard",
		"Inventory": inv,
	})
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := parchment.ListInput{
		Kind:   q.Get("kind"),
		Scope:  q.Get("scope"),
		Status: q.Get("status"),
	}
	arts, err := s.proto.ListArtifacts(r.Context(), in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "list.html", map[string]any{
		"Title":     "Artifacts",
		"Artifacts": arts,
		"Filter":    in,
	})
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := s.proto.GetArtifact(r.Context(), id)
	if err != nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	s.render(w, "detail.html", map[string]any{
		"Title":    art.Title,
		"Artifact": art,
	})
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tree, err := s.proto.ArtifactTree(r.Context(), parchment.TreeInput{ID: id})
	if err != nil {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	s.render(w, "tree.html", map[string]any{
		"Title": "Tree: " + id,
		"Root":  tree,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	var results []*parchment.Artifact
	if q != "" {
		var err error
		results, err = s.proto.SearchArtifacts(r.Context(), q, parchment.ListInput{})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.render(w, "search.html", map[string]any{
		"Title":   "Search",
		"Query":   q,
		"Results": results,
	})
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	tmpl, ok := s.pages[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func renderMarkdown(text string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(text), &buf); err != nil {
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

	events, err := s.proto.GetEvents(r.Context(), since, filter)
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
