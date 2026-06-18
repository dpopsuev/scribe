package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestAnalyze_Lens_Inline(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "ptp-1", Title: "PTP task", Labels: []string{"kind:effort.task", "project:ptp"}},
		{ID: "ocp-1", Title: "OCP infra", Labels: []string{"kind:effort.task", "project:ocp"}},
	} {
		if err := svc.Proto.Store().Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Proto.Store().AddEdge(ctx, parchment.Edge{
		From: "ptp-1", To: "ocp-1", Relation: "depends_on",
	}); err != nil {
		t.Fatal(err)
	}

	op := service.Find("analyze")
	if op == nil {
		t.Fatal("analyze op not found")
	}

	raw, _ := json.Marshal(map[string]any{
		"mode":     "lens",
		"anchor":   []string{"project:ptp"},
		"traverse": []map[string]any{{"relation": "depends_on", "direction": "both", "max_depth": 3}},
		"score_by": "edges",
	})

	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("analyze lens: %v", err)
	}

	if !strings.Contains(out, "PTP task") {
		t.Error("expected PTP task in output")
	}
	if !strings.Contains(out, "OCP infra") {
		t.Error("expected OCP infra in output (cross-project via depends_on)")
	}
}

func TestAnalyze_Lens_FromContext(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	for _, a := range []*parchment.Artifact{
		{ID: "ptp-2", Title: "PTP sync", Labels: []string{"kind:effort.task", "project:ptp"}},
		{ID: "ocp-2", Title: "OCP node", Labels: []string{"kind:effort.task", "project:ocp"}},
		{ID: "lens-ptp", Title: "PTP lens", Labels: []string{"kind:knowledge.context"},
			Extra: map[string]any{
				"lens_anchor":   []any{"project:ptp"},
				"lens_traverse": []any{map[string]any{"relation": "depends_on", "direction": "outgoing", "max_depth": float64(3)}},
				"lens_score_by": "edges",
			}},
	} {
		if err := svc.Proto.Store().Put(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.Proto.Store().AddEdge(ctx, parchment.Edge{
		From: "ptp-2", To: "ocp-2", Relation: "depends_on",
	}); err != nil {
		t.Fatal(err)
	}

	op := service.Find("analyze")
	if op == nil {
		t.Fatal("analyze op not found")
	}

	raw, _ := json.Marshal(map[string]any{
		"mode":       "lens",
		"context_id": "lens-ptp",
	})

	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("analyze lens from context: %v", err)
	}

	if !strings.Contains(out, "PTP sync") {
		t.Error("expected PTP sync in output")
	}
	if !strings.Contains(out, "OCP node") {
		t.Error("expected OCP node in output via stored lens")
	}
}
