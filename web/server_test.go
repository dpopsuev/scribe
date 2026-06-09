package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/web"
)

func setup(t *testing.T) *web.Server {
	t.Helper()
	dir := t.TempDir()
	s, err := parchment.OpenSQLite(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{
		Labels: []string{"kind:task", "status:active", "scope:test"}, ID: "TASK-2026-001", Title: "Test Task",
		Sections: []parchment.Section{
			{Name: "design", Text: "## Overview\n\nThis is a **test** design."},
		},
	})
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:campaign", "status:active", "scope:test"}, ID: "CMP-2026-001", Title: "Test Campaign"})
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:goal", "status:current", "scope:test"}, ID: "GOL-2026-001", Title: "Test Goal"})
	s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:task", "status:active", "scope:test"}, ID: "TASK-2026-002", Title: "Child Task",
		Parent: "GOL-2026-001"})

	proto := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	return web.NewServer(proto, "dev", "")
}

func TestDashboard(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/", http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Dashboard") {
		t.Error("dashboard page missing title")
	}
	if !strings.Contains(body, "Test Campaign") {
		t.Error("dashboard missing active campaign")
	}
	if !strings.Contains(body, "Test Goal") {
		t.Error("dashboard missing goal")
	}
}

func TestArtifactList(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts", http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /artifacts = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Task") {
		t.Error("list missing Test Task")
	}
}

func TestArtifactListFiltered(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts?kind=campaign", http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /artifacts?kind=campaign = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Campaign") {
		t.Error("filtered list missing campaign")
	}
	if strings.Contains(body, "Test Task") {
		t.Error("filtered list should not contain task")
	}
}

func TestArtifactDetail(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts/TASK-2026-001", http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /artifacts/TASK-2026-001 = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Task") {
		t.Error("detail missing title")
	}
	if !strings.Contains(body, "<strong>test</strong>") {
		t.Error("markdown not rendered in section")
	}
}

func TestArtifactDetail_NotFound(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/artifacts/NOPE-000", http.NoBody))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("GET /artifacts/NOPE-000 = %d, want 404", rr.Code)
	}
}

func TestTree(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/tree/GOL-2026-001", http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /tree/GOL-2026-001 = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Child Task") {
		t.Error("tree missing child")
	}
}

func TestSearch(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/search?q=Test", http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /search?q=Test = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Test Task") {
		t.Error("search missing result")
	}
}

func TestSearchEmpty(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/search", http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /search = %d, want 200", rr.Code)
	}
}

func TestEventFeed_EmitsSSE(t *testing.T) {
	// Given: an artifact created via Protocol (emits EventCreated into EventLog)
	// When: GET /events?since=<before creation>
	// Then: response is text/event-stream containing the artifact's created event
	dir := t.TempDir()
	s, err := parchment.OpenSQLite(dir + "/events_test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	proto := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	before := "1970-01-01T00:00:00Z"

	art, err := proto.CreateArtifact(context.Background(), parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindTask}, Title: "SSE test artifact", Scope: "test"})
	if err != nil {
		t.Fatal(err)
	}

	srv := web.NewServer(proto, "dev", "")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/events?since="+before, http.NoBody))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /events = %d, want 200", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, art.ID) {
		t.Errorf("SSE body does not contain artifact ID %q\nbody: %s", art.ID, body)
	}
	if !strings.Contains(body, "created") {
		t.Errorf("SSE body does not contain event type 'created'\nbody: %s", body)
	}
}

func TestEventFeed_MissingSince(t *testing.T) {
	srv := setup(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest("GET", "/events", http.NoBody))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("GET /events (no since) = %d, want 400", rr.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := setup(t)
	methods := []string{"POST", "PUT", "DELETE", "PATCH"}
	paths := []string{"/", "/artifacts", "/artifacts/TASK-2026-001", "/search"}

	for _, method := range methods {
		for _, path := range paths {
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, httptest.NewRequest(method, path, http.NoBody))
			if rr.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, rr.Code)
			}
		}
	}
}
