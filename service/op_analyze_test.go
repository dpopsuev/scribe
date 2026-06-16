package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func setupAnalyzeService(t *testing.T) *service.Service {
	t.Helper()
	svc := newTestService(t, "test")
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "a", Title: "Alpha", Labels: []string{"kind:knowledge.note", "project:test"}},
		{ID: "b", Title: "Beta", Labels: []string{"kind:knowledge.note", "project:test"}},
		{ID: "c", Title: "Charlie", Labels: []string{"kind:knowledge.note", "project:test"}},
		{ID: "d", Title: "Delta", Labels: []string{"kind:effort.task", "project:test"}},
	} {
		if err := svc.Proto.Store().Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	for _, e := range []parchment.Edge{
		{From: "a", To: "b", Relation: "cites"},
		{From: "a", To: "c", Relation: "cites"},
		{From: "d", To: "b", Relation: "depends_on"},
	} {
		if err := svc.Proto.Store().AddEdge(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	return svc
}

func runAnalyze(t *testing.T, svc *service.Service, input any) string {
	t.Helper()
	op := service.Find("analyze")
	if op == nil {
		t.Fatal("analyze op not registered")
	}
	raw, _ := json.Marshal(input)
	out, err := op.Run(context.Background(), svc, raw)
	if err != nil {
		t.Fatalf("analyze error: %v", err)
	}
	return out
}

func TestAnalyze_Fan(t *testing.T) {
	t.Parallel()
	svc := setupAnalyzeService(t)
	out := runAnalyze(t, svc, map[string]any{
		"mode": "fan", "scope": "test", "limit": 10,
	})
	if !strings.Contains(out, "fan analysis") {
		t.Errorf("expected 'fan analysis' header, got: %s", out)
	}
	if !strings.Contains(out, "Beta") {
		t.Errorf("expected Beta (highest fan-in) in output")
	}
}

func TestAnalyze_CoCitation(t *testing.T) {
	t.Parallel()
	svc := setupAnalyzeService(t)
	out := runAnalyze(t, svc, map[string]any{
		"mode": "co_citation", "id": "b", "min_shared": 1,
	})
	if !strings.Contains(out, "co-citation") {
		t.Errorf("expected 'co-citation' header, got: %s", out)
	}
}

func TestAnalyze_Coupling(t *testing.T) {
	t.Parallel()
	svc := setupAnalyzeService(t)
	out := runAnalyze(t, svc, map[string]any{
		"mode": "coupling", "id": "a", "min_shared": 1,
	})
	if !strings.Contains(out, "bibliographic coupling") {
		t.Errorf("expected 'bibliographic coupling' header, got: %s", out)
	}
}

func TestAnalyze_Paths(t *testing.T) {
	t.Parallel()
	svc := setupAnalyzeService(t)
	out := runAnalyze(t, svc, map[string]any{
		"mode": "paths", "from": "a", "to": "b",
	})
	if !strings.Contains(out, "shortest path") {
		t.Errorf("expected 'shortest path' in output, got: %s", out)
	}
	if !strings.Contains(out, "cites") {
		t.Errorf("expected 'cites' relation in path, got: %s", out)
	}
}

func TestAnalyze_Paths_NoPath(t *testing.T) {
	t.Parallel()
	svc := setupAnalyzeService(t)
	out := runAnalyze(t, svc, map[string]any{
		"mode": "paths", "from": "c", "to": "d",
	})
	if !strings.Contains(out, "no path") {
		t.Errorf("expected 'no path' in output, got: %s", out)
	}
}

func TestAnalyze_PageRank(t *testing.T) {
	t.Parallel()
	svc := setupAnalyzeService(t)
	out := runAnalyze(t, svc, map[string]any{
		"mode": "pagerank", "scope": "test", "limit": 10,
	})
	if !strings.Contains(out, "pagerank") {
		t.Errorf("expected 'pagerank' header, got: %s", out)
	}
	lines := strings.Split(out, "\n")
	var dataLines []string
	for _, l := range lines {
		if strings.Contains(l, "knowledge.note") || strings.Contains(l, "effort.task") {
			dataLines = append(dataLines, l)
		}
	}
	if len(dataLines) == 0 {
		t.Errorf("expected data rows with kind labels")
	}
}

func TestAnalyze_UnknownMode(t *testing.T) {
	t.Parallel()
	svc := setupAnalyzeService(t)
	op := service.Find("analyze")
	raw, _ := json.Marshal(map[string]any{"mode": "bogus"})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil {
		t.Error("expected error for unknown mode")
	}
}

func TestAnalyze_Registered(t *testing.T) {
	if service.Find("analyze") == nil {
		t.Fatal("analyze op not found in registry")
	}
}
