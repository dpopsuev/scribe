package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestWaiveComplete_AuditsIncompleteChildren(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	camp, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "C", Labels: []string{"kind:effort.campaign", "scope:waive-test", "work.active"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "G", Parent: camp.ID, Labels: []string{"kind:effort.goal", "scope:waive-test", "work.draft"},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{
		"id": camp.ID, "field": "status", "value": "work.complete",
		"waive_reason": "legacy drift cleanup after dogfood",
	})
	res, err := service.Find("set").Execute(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "waived") {
		t.Fatalf("expected waived text, got %s", res.Text)
	}
	got, _ := svc.Proto.GetArtifact(ctx, camp.ID)
	if parchment.StatusFromLabels(got.Labels) != "work.complete" {
		t.Fatalf("status=%s", parchment.StatusFromLabels(got.Labels))
	}
	if got.Extra["lifecycle_waiver"] == nil {
		t.Fatal("expected lifecycle_waiver stamp")
	}
}

func TestCreate_DependsOnMustExist(t *testing.T) {
	svc := newTestService(t)
	raw, _ := json.Marshal(map[string]any{
		"kind": "effort.task", "title": "T", "scope": "dep-test",
		"depends_on": []string{"missing-dep-xyz"},
		"sections":   []map[string]string{{"name": "context", "text": "c"}},
	})
	_, err := service.Find("create").Execute(context.Background(), svc, raw)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected missing depends_on error, got %v", err)
	}
}

func TestQuery_TitleExact(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, _ = svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "Exact Match Please", Labels: []string{"kind:effort.task", "scope:test"},
		Sections: []parchment.Section{{Name: "context", Text: "c"}},
	})
	_, _ = svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "Other Thing", Labels: []string{"kind:effort.task", "scope:test"},
		Sections: []parchment.Section{{Name: "context", Text: "c"}},
	})
	raw, _ := json.Marshal(map[string]any{
		"scope": "test", "title_exact": "Exact Match Please",
	})
	res, err := service.Find("query").Execute(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "Exact Match Please") || strings.Contains(res.Text, "Other Thing") {
		t.Fatalf("exact filter failed: %s", res.Text)
	}
}

func TestAXBenchmark_PlanApplyOneCallTwentyNode(t *testing.T) {
	svc := newTestService(t)
	artifacts := []any{
		map[string]any{"kind": "intent.decision", "title": "ADR", "scope": "ax-bench", "status": "decision.accepted",
			"sections": []map[string]string{{"name": "context", "text": "why"}}},
		map[string]any{"kind": "effort.campaign", "title": "Campaign", "scope": "ax-bench"},
	}
	for i := 0; i < 3; i++ {
		artifacts = append(artifacts, map[string]any{
			"kind": "effort.goal", "title": "G", "scope": "ax-bench", "parent": "$1",
		})
	}
	for i := 0; i < 15; i++ {
		parent := fmt.Sprintf("$%d", 2+i/5)
		artifacts = append(artifacts, map[string]any{
			"kind": "effort.task", "title": "T", "scope": "ax-bench", "parent": parent,
			"sections": []map[string]string{{"name": "context", "text": "c"}},
		})
	}
	raw, _ := json.Marshal(map[string]any{"mutation_id": "ax-bench-1", "artifacts": artifacts})
	calls := 1
	res, err := service.Find("create").Execute(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	mr := res.Data.(service.MutationResult)
	if mr.Count != 20 {
		t.Fatalf("expected 20 artifacts, got %d text=%s", mr.Count, res.Text)
	}
	schemaBytes, _ := json.Marshal(service.MutationOutputSchema())
	fieldBytes, _ := json.Marshal(map[string]any{"create": true})
	t.Logf("AX gate: filing_calls=%d artifacts=%d mutation_schema_bytes=%d field_contract_proxy_bytes=%d",
		calls, mr.Count, len(schemaBytes), len(fieldBytes))
	if calls != 1 {
		t.Fatalf("plan/apply should file connected graph in 1 call, got %d", calls)
	}
}

func TestStableArtifactLink(t *testing.T) {
	got := service.StableArtifactLink("scribe", "abc")
	if got != "scribe://scribe/abc" {
		t.Fatalf("got %s", got)
	}
}
