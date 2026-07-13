package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestCreate_DryRunPlan_NoWrites(t *testing.T) {
	svc := newTestService(t)
	raw, _ := json.Marshal(map[string]any{
		"dry_run": true,
		"artifacts": []any{
			map[string]any{"kind": "effort.campaign", "title": "C", "scope": "ax-test"},
			map[string]any{"kind": "effort.goal", "title": "G", "scope": "ax-test", "parent": "$0"},
		},
	})
	res, err := service.Find("create").Execute(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	mr, ok := res.Data.(service.MutationResult)
	if !ok {
		t.Fatalf("expected MutationResult, got %T", res.Data)
	}
	if !mr.DryRun || mr.Status != "plan" || mr.Count != 2 {
		t.Fatalf("unexpected plan result: %+v", mr)
	}
	arts, _ := svc.Proto.ListArtifacts(context.Background(), parchment.ListInput{
		Labels: []string{"scope:ax-test"},
	})
	if len(arts) != 0 {
		t.Fatalf("dry_run wrote %d artifacts", len(arts))
	}
}

func TestCreate_BatchApply_StructuredIDs(t *testing.T) {
	svc := newTestService(t)
	raw, _ := json.Marshal(map[string]any{
		"mutation_id": "mut-test-1",
		"artifacts": []any{
			map[string]any{"kind": "effort.goal", "title": "Parent", "scope": "ax-apply"},
			map[string]any{
				"kind": "effort.task", "title": "Child", "scope": "ax-apply", "parent": "$0",
				"sections": []map[string]string{{"name": "context", "text": "c"}},
				"priority": "high",
			},
		},
	})
	res, err := service.Find("create").Execute(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	mr := res.Data.(service.MutationResult)
	if mr.Count != 2 || len(mr.IDs) != 2 {
		t.Fatalf("expected 2 IDs, got %+v", mr)
	}
	t.Logf("created IDs=%v text=%s", mr.IDs, res.Text)
	for _, id := range mr.IDs {
		art, err := svc.Proto.GetArtifact(context.Background(), id)
		t.Logf("get %s err=%v art=%v", id, err, art != nil)
	}
	if mr.Artifacts[0].ID == "" || strings.HasPrefix(mr.Artifacts[0].ID, "$") {
		t.Fatalf("expected real IDs, got %+v", mr.Artifacts)
	}
	res2, err := service.Find("create").Execute(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	mr2 := res2.Data.(service.MutationResult)
	if !mr2.Idempotent || mr2.IDs[0] != mr.IDs[0] {
		t.Fatalf("expected idempotent replay, got %+v", mr2)
	}
	for _, id := range mr.IDs {
		if _, err := svc.Proto.GetArtifact(context.Background(), id); err != nil {
			t.Fatalf("artifact %s missing after replay: %v", id, err)
		}
	}
}

func TestCreate_BatchRollback_OnFailure(t *testing.T) {
	svc := newTestService(t)
	raw, _ := json.Marshal(map[string]any{
		"artifacts": []any{
			map[string]any{"kind": "effort.goal", "title": "Keep?", "scope": "ax-rollback"},
			map[string]any{"kind": "effort.task", "title": "", "scope": "ax-rollback", "parent": "$0"},
		},
	})
	_, err := service.Find("create").Execute(context.Background(), svc, raw)
	if err == nil {
		t.Fatal("expected error")
	}
	arts, _ := svc.Proto.ListArtifacts(context.Background(), parchment.ListInput{
		Labels: []string{"scope:ax-rollback"},
	})
	if len(arts) != 0 {
		t.Fatalf("expected rollback to leave 0 artifacts, got %d", len(arts))
	}
}

func TestHygiene_ReadOnlyByDefault(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "orphan note", Labels: []string{"kind:knowledge.note", "scope:ax-hyg", "note.fleeting"},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"scope": "ax-hyg", "format": "full"})
	res, err := service.Find("hygiene").Execute(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	data := res.Data.(map[string]any)
	if data["read_only"] != true {
		t.Fatalf("expected read_only true, got %+v", data)
	}
	if pruned, _ := data["pruned"].(int); pruned != 0 {
		t.Fatalf("expected no prune by default, got %d", pruned)
	}
}

func TestHygiene_AcknowledgeWithoutCascade(t *testing.T) {
	svc := newTestService(t)
	raw, _ := json.Marshal(map[string]any{"acknowledge_ids": []string{"art-a", "art-b"}})
	res, err := service.Find("hygiene").Execute(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "acknowledged 2") {
		t.Fatalf("unexpected text: %s", res.Text)
	}
}

func TestCreate_TwentyNodeDogfoodFixture(t *testing.T) {
	svc := newTestService(t)
	artifacts := []any{
		map[string]any{"kind": "intent.decision", "title": "D", "scope": "ax-fixture", "status": "decision.proposed"},
		map[string]any{"kind": "effort.campaign", "title": "Camp", "scope": "ax-fixture"},
	}
	for i := 0; i < 3; i++ {
		artifacts = append(artifacts, map[string]any{
			"kind": "effort.goal", "title": "G" + string(rune('1'+i)), "scope": "ax-fixture", "parent": "$1",
		})
	}
	for i := 0; i < 15; i++ {
		goalIdx := 2 + (i % 3)
		artifacts = append(artifacts, map[string]any{
			"kind": "effort.task", "title": "T" + string(rune('A'+i)), "scope": "ax-fixture",
			"parent":   "$" + string(rune('0'+goalIdx)),
			"priority": "medium",
			"sections": []map[string]string{{"name": "context", "text": "dogfood"}},
		})
	}
	// Fix parent refs with proper formatting
	artifacts = []any{
		map[string]any{"kind": "intent.decision", "title": "D", "scope": "ax-fixture", "status": "decision.proposed"},
		map[string]any{"kind": "effort.campaign", "title": "Camp", "scope": "ax-fixture"},
		map[string]any{"kind": "effort.goal", "title": "G1", "scope": "ax-fixture", "parent": "$1"},
		map[string]any{"kind": "effort.goal", "title": "G2", "scope": "ax-fixture", "parent": "$1"},
		map[string]any{"kind": "effort.goal", "title": "G3", "scope": "ax-fixture", "parent": "$1"},
	}
	for i := 0; i < 15; i++ {
		parent := "$2"
		switch i % 3 {
		case 1:
			parent = "$3"
		case 2:
			parent = "$4"
		}
		artifacts = append(artifacts, map[string]any{
			"kind": "effort.task", "title": "Task-" + string(rune('A'+i)), "scope": "ax-fixture",
			"parent": parent, "priority": "medium",
			"sections": []map[string]string{{"name": "context", "text": "dogfood"}},
		})
	}
	planRaw, _ := json.Marshal(map[string]any{"mode": "plan", "mutation_id": "fixture-20", "artifacts": artifacts})
	plan, err := service.Find("create").Execute(context.Background(), svc, planRaw)
	if err != nil {
		t.Fatal(err)
	}
	pm := plan.Data.(service.MutationResult)
	if pm.Count != 20 || !pm.DryRun {
		t.Fatalf("plan expected 20 dry_run, got %+v", pm)
	}
	arts, _ := svc.Proto.ListArtifacts(context.Background(), parchment.ListInput{Labels: []string{"scope:ax-fixture"}})
	if len(arts) != 0 {
		t.Fatalf("plan wrote artifacts: %d", len(arts))
	}

	applyRaw, _ := json.Marshal(map[string]any{"mutation_id": "fixture-20-apply", "artifacts": artifacts})
	apply, err := service.Find("create").Execute(context.Background(), svc, applyRaw)
	if err != nil {
		t.Fatal(err)
	}
	am := apply.Data.(service.MutationResult)
	if am.Count != 20 || len(am.IDs) != 20 {
		t.Fatalf("apply expected 20, got %+v", am)
	}
	for _, id := range am.IDs {
		if _, err := svc.Proto.GetArtifact(context.Background(), id); err != nil {
			t.Fatalf("missing %s: %v", id, err)
		}
	}
}
