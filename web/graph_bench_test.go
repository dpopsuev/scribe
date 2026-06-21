package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/web"
)

// ── Graph API benchmarks at production scale (5× real DB) ────────────────
//
// Run:  go test -bench=BenchmarkGraph -benchmem -timeout 300s ./web/...
//
// Each benchmark uses ~8200 artifacts with ~6000 edges and measures
// the HTTP response time for a single graph endpoint.

func buildProductionScale(dir string) (srv *web.Server, cleanup func()) {
	s, err := parchment.OpenSQLite(dir + "/bench.db")
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	type scopeSpec struct {
		name      string
		structs   int
		funcs     int
		sources   int
		notes     int
		campaigns int
		goals     int
		tasks     int
	}

	scopes := []scopeSpec{
		{"mega", 1400, 3350, 90, 0, 0, 0, 0},
		{"work", 0, 0, 0, 110, 22, 100, 90},
		{"code", 700, 1550, 50, 15, 0, 0, 0},
		{"docs", 0, 0, 200, 500, 5, 10, 20},
		{"tiny", 30, 60, 5, 3, 1, 2, 4},
	}

	for _, sc := range scopes {
		for i := range sc.structs {
			_ = s.Put(ctx, &parchment.Artifact{
				ID: fmt.Sprintf("%s-st-%04d", sc.name, i), Title: fmt.Sprintf("%s.Struct%d", sc.name, i),
				Labels: []string{"kind:code.struct", "project:" + sc.name, "status:active"},
			})
		}
		for i := range sc.funcs {
			_ = s.Put(ctx, &parchment.Artifact{
				ID: fmt.Sprintf("%s-fn-%04d", sc.name, i), Title: fmt.Sprintf("%s.Func%d", sc.name, i),
				Labels: []string{"kind:code.function", "project:" + sc.name, "status:active"},
			})
		}
		for i := range sc.sources {
			_ = s.Put(ctx, &parchment.Artifact{
				ID: fmt.Sprintf("%s-src-%04d", sc.name, i), Title: fmt.Sprintf("%s Source %d", sc.name, i),
				Labels: []string{"kind:knowledge.source", "project:" + sc.name, "status:active"},
			})
		}
		for i := range sc.notes {
			_ = s.Put(ctx, &parchment.Artifact{
				ID: fmt.Sprintf("%s-note-%04d", sc.name, i), Title: fmt.Sprintf("%s Note %d", sc.name, i),
				Labels: []string{"kind:knowledge.note", "project:" + sc.name, "status:active"},
			})
		}
		for i := range sc.campaigns {
			_ = s.Put(ctx, &parchment.Artifact{
				ID: fmt.Sprintf("%s-camp-%04d", sc.name, i), Title: fmt.Sprintf("%s Campaign %d", sc.name, i),
				Labels: []string{"kind:effort.campaign", "project:" + sc.name, "status:active"},
			})
		}
		for i := range sc.goals {
			_ = s.Put(ctx, &parchment.Artifact{
				ID: fmt.Sprintf("%s-goal-%04d", sc.name, i), Title: fmt.Sprintf("%s Goal %d", sc.name, i),
				Labels: []string{"kind:effort.goal", "project:" + sc.name, "status:active"},
			})
		}
		for i := range sc.tasks {
			_ = s.Put(ctx, &parchment.Artifact{
				ID: fmt.Sprintf("%s-task-%04d", sc.name, i), Title: fmt.Sprintf("%s Task %d", sc.name, i),
				Labels: []string{"kind:effort.task", "project:" + sc.name, "status:active"},
			})
		}
		for i := range sc.structs {
			for j := range 3 {
				fi := i*3 + j
				if fi >= sc.funcs {
					break
				}
				_ = s.AddEdge(ctx, parchment.Edge{
					From:     fmt.Sprintf("%s-st-%04d", sc.name, i),
					To:       fmt.Sprintf("%s-fn-%04d", sc.name, fi),
					Relation: "has_member",
				})
			}
		}
		for i := range sc.funcs / 5 {
			target := (i*7 + 13) % sc.funcs
			if target == i {
				target = (target + 1) % sc.funcs
			}
			_ = s.AddEdge(ctx, parchment.Edge{
				From:     fmt.Sprintf("%s-fn-%04d", sc.name, i),
				To:       fmt.Sprintf("%s-fn-%04d", sc.name, target),
				Relation: "calls",
			})
		}
		for i := range sc.goals {
			campIdx := i % max(sc.campaigns, 1)
			_ = s.AddEdge(ctx, parchment.Edge{
				From:     fmt.Sprintf("%s-camp-%04d", sc.name, campIdx),
				To:       fmt.Sprintf("%s-goal-%04d", sc.name, i),
				Relation: "parent_of",
			})
		}
		for i := range sc.tasks {
			goalIdx := i % max(sc.goals, 1)
			_ = s.AddEdge(ctx, parchment.Edge{
				From:     fmt.Sprintf("%s-goal-%04d", sc.name, goalIdx),
				To:       fmt.Sprintf("%s-task-%04d", sc.name, i),
				Relation: "parent_of",
			})
		}
	}

	allScopes := make([]string, len(scopes))
	for i, sc := range scopes {
		allScopes[i] = sc.name
	}
	proto := parchment.New(s, nil, allScopes, nil, parchment.ProtocolConfig{})
	return web.NewServer(proto, "dev", ""), func() { _ = s.Close() }
}

func BenchmarkGraphScopes(b *testing.B) {
	srv, cleanup := buildProductionScale(b.TempDir())
	b.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/graph/scopes", http.NoBody)

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}
}

func BenchmarkGraphKinds_LargeScope(b *testing.B) {
	srv, cleanup := buildProductionScale(b.TempDir())
	b.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/graph/kinds?scope=mega&status=active", http.NoBody)

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}
}

func BenchmarkGraphArtifacts_2000Nodes(b *testing.B) {
	srv, cleanup := buildProductionScale(b.TempDir())
	b.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/graph?scope=mega&max_nodes=2000", http.NoBody)

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}
}

func BenchmarkGraphArtifacts_500Nodes(b *testing.B) {
	srv, cleanup := buildProductionScale(b.TempDir())
	b.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/graph?scope=code&max_nodes=500", http.NoBody)

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}
}

func BenchmarkGraphLocal_1Hop(b *testing.B) {
	srv, cleanup := buildProductionScale(b.TempDir())
	b.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/graph/local?id=code-st-0000&hops=1", http.NoBody)

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}
}

func BenchmarkGraphLocal_2Hops(b *testing.B) {
	srv, cleanup := buildProductionScale(b.TempDir())
	b.Cleanup(cleanup)
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/graph/local?id=code-st-0000&hops=2", http.NoBody)

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}
}
