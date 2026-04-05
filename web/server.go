package web

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
	"strings"

	parchment "github.com/dpopsuev/parchment"
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
	pages map[string]*template.Template
	mux   *http.ServeMux
}

func NewServer(proto *parchment.Protocol) *Server {
	s := &Server{proto: proto, pages: make(map[string]*template.Template)}

	funcMap := template.FuncMap{
		"renderMarkdown": renderMarkdown,
	}

	layoutTmpl := template.Must(
		template.New("layout.html").Funcs(funcMap).ParseFS(templateFS, "templates/layout.html"),
	)

	for _, page := range []string{"dashboard.html", "list.html", "detail.html", "tree.html", "search.html"} {
		clone := template.Must(layoutTmpl.Clone())
		s.pages[page] = template.Must(clone.ParseFS(templateFS, "templates/"+page))
	}

	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /", s.handleDashboard)
	s.mux.HandleFunc("GET /artifacts", s.handleList)
	s.mux.HandleFunc("GET /artifacts/{id}", s.handleDetail)
	s.mux.HandleFunc("GET /tree/{id}", s.handleTree)
	s.mux.HandleFunc("GET /search", s.handleSearch)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	inv, err := s.proto.Inventory(r.Context())
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
		return template.HTML("<pre>" + template.HTMLEscapeString(text) + "</pre>")
	}
	out := buf.String()
	out = convertMermaidBlocks(out)
	return template.HTML(out) //nolint:gosec
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
