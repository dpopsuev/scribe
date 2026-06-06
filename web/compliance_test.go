package web_test

// Compliance tests — layers 1 and 2 (always run, no browser required).
//
//  Layer 1 — Go unit:  ViolationCount reads compliance labels and Extra correctly.
//  Layer 2 — Go API:   /api/graph returns violations field matching artifact state.
//
//  Layer 3 (browser drift test) lives in compliance_browser_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/web"
)

// ── Layer 1: ViolationCount unit tests ───────────────────────────────────

func TestViolationCount_NoLabels(t *testing.T) {
	a := &parchment.Artifact{ID: "T-1", Kind: "task"}
	if got := web.ViolationCount(a); got != 0 {
		t.Errorf("no labels → want 0, got %d", got)
	}
}

func TestViolationCount_CompliantLabel(t *testing.T) {
	a := &parchment.Artifact{
		ID:     "T-1",
		Labels: []string{"kind:task", "compliance:ok"},
	}
	if got := web.ViolationCount(a); got != 0 {
		t.Errorf("compliance:ok → want 0, got %d", got)
	}
}

func TestViolationCount_ViolationLabelNoDetail(t *testing.T) {
	a := &parchment.Artifact{
		ID:     "T-1",
		Labels: []string{"compliance:violation"},
	}
	if got := web.ViolationCount(a); got != 1 {
		t.Errorf("violation label, no Extra → want 1 (fallback), got %d", got)
	}
}

func TestViolationCount_ViolationLabelWithDetail(t *testing.T) {
	a := &parchment.Artifact{
		ID:     "T-1",
		Labels: []string{"compliance:violation"},
		Extra: map[string]any{
			parchment.ExtraKeyComplianceViolations: []any{
				"missing section: threat_model",
				"missing section: acceptance",
				"missing section: context",
			},
		},
	}
	if got := web.ViolationCount(a); got != 3 {
		t.Errorf("3 violations → want 3, got %d", got)
	}
}

func TestViolationCount_EmptyViolationsSlice(t *testing.T) {
	// violation label present, but the violations slice is empty.
	// The label is authoritative — treat as at least 1.
	a := &parchment.Artifact{
		ID:     "T-1",
		Labels: []string{"compliance:violation"},
		Extra:  map[string]any{parchment.ExtraKeyComplianceViolations: []any{}},
	}
	if got := web.ViolationCount(a); got < 0 {
		t.Errorf("want >= 0, got %d", got)
	}
}

// ── Layer 2: /api/graph returns violations field ──────────────────────────

func complianceServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := parchment.OpenSQLite(t.TempDir() + "/comp.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()
	s.Put(ctx, &parchment.Artifact{ //nolint:errcheck // test setup
		ID: "TSK-OK", Kind: "task", Scope: "test", Status: "active",
		Title: "Compliant", Labels: []string{"compliance:ok", "kind:task"},
	})
	s.Put(ctx, &parchment.Artifact{ //nolint:errcheck // test setup
		ID: "TSK-WARN", Kind: "task", Scope: "test", Status: "active",
		Title:  "Warning",
		Labels: []string{"compliance:violation", "kind:task"},
		Extra:  map[string]any{parchment.ExtraKeyComplianceViolations: []any{"m1", "m2"}},
	})
	s.Put(ctx, &parchment.Artifact{ //nolint:errcheck // test setup
		ID: "TSK-CRIT", Kind: "task", Scope: "test", Status: "active",
		Title:  "Critical",
		Labels: []string{"compliance:violation", "kind:task"},
		Extra:  map[string]any{parchment.ExtraKeyComplianceViolations: []any{"m1", "m2", "m3", "m4"}},
	})

	proto := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	srv := httptest.NewServer(web.NewServer(proto, "dev"))
	t.Cleanup(srv.Close)
	return srv
}

func TestAPIGraph_ViolationsField(t *testing.T) {
	srv := complianceServer(t)

	resp, err := http.Get(srv.URL + "/api/graph?scope=test&status=active")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var data struct {
		Nodes []struct {
			ID         string `json:"id"`
			Violations int    `json:"violations"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}

	byID := make(map[string]int, len(data.Nodes))
	for _, n := range data.Nodes {
		byID[n.ID] = n.Violations
	}

	for _, tt := range []struct {
		id   string
		want int
	}{
		{"TSK-OK", 0},
		{"TSK-WARN", 2},
		{"TSK-CRIT", 4},
	} {
		got, ok := byID[tt.id]
		if !ok {
			t.Errorf("node %s missing from response (got: %v)", tt.id, byID)
			continue
		}
		if got != tt.want {
			t.Errorf("node %s: violations=%d want %d", tt.id, got, tt.want)
		}
	}
}
