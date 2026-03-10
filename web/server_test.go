package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/store"
	"github.com/dpopsuev/scribe/web"
)

func setup(t *testing.T) *web.Server {
	t.Helper()
	dir := t.TempDir()
	s, err := store.OpenSQLite(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	s.Put(ctx, &model.Artifact{
		ID: "CON-2026-001", Kind: "contract", Scope: "test",
		Status: "active", Title: "Test Contract",
		Sections: []model.Section{
			{Name: "design", Text: "## Overview\n\nThis is a **test** design."},
		},
	})
	s.Put(ctx, &model.Artifact{
		ID: "SPR-2026-001", Kind: "sprint", Scope: "test",
		Status: "active", Title: "Test Sprint",
	})
	s.Put(ctx, &model.Artifact{
		ID: "GOL-2026-001", Kind: "goal", Scope: "test",
		Status: "current", Title: "Test Goal",
	})
	s.Put(ctx, &model.Artifact{
		ID: "CON-2026-002", Kind: "contract", Scope: "test",
		Status: "active", Title: "Child Contract",
		Parent: "SPR-2026-001",
	})

	proto := protocol.New(s, nil, []string{"test"}, nil)
	return web.NewServer(proto)
}

func TestDashboard(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("dashboard page missing title")
	}
	if !strings.Contains(body, "Test Sprint") {
		t.Error("dashboard missing active sprint")
	}
	if !strings.Contains(body, "Test Goal") {
		t.Error("dashboard missing goal")
	}
}

func TestArtifactList(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /artifacts = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Contract") {
		t.Error("list missing Test Contract")
	}
}

func TestArtifactListFiltered(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts?kind=sprint", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /artifacts?kind=sprint = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Sprint") {
		t.Error("filtered list missing sprint")
	}
	if strings.Contains(body, "Test Contract") {
		t.Error("filtered list should not contain contract")
	}
}

func TestArtifactDetail(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts/CON-2026-001", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /artifacts/CON-2026-001 = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Contract") {
		t.Error("detail missing title")
	}
	if !strings.Contains(body, "<strong>test</strong>") {
		t.Error("markdown not rendered in section")
	}
}

func TestArtifactDetail_NotFound(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts/NOPE-000", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET /artifacts/NOPE-000 = %d, want 404", rr.Code)
	}
}

func TestTree(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/tree/SPR-2026-001", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /tree/SPR-2026-001 = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Child Contract") {
		t.Error("tree missing child")
	}
}

func TestSearch(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/search?q=Test", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /search?q=Test = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Contract") {
		t.Error("search missing result")
	}
}

func TestSearchEmpty(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/search", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /search = %d, want 200", rr.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := setup(t)
	methods := []string{"POST", "PUT", "DELETE", "PATCH"}
	paths := []string{"/", "/artifacts", "/artifacts/CON-2026-001", "/search"}

	for _, method := range methods {
		for _, path := range paths {
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, rr.Code)
			}
		}
	}
}
