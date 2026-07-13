package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestProgress_AllDraftCampaign_ZeroDelivery(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	camp, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "C", Labels: []string{"kind:effort.campaign", "scope:prog-test"},
		Sections: []parchment.Section{{Name: "mission", Text: "filled"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	goal, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "G", Parent: camp.ID, Labels: []string{"kind:effort.goal", "scope:prog-test"},
		Sections: []parchment.Section{{Name: "goal", Text: "g"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "T", Parent: goal.ID, Labels: []string{"kind:effort.task", "scope:prog-test"},
		Sections: []parchment.Section{
			{Name: "context", Text: "c"},
			{Name: "checklist", Text: "- [ ] a\n- [ ] b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	camp, _ = svc.Proto.GetArtifact(ctx, camp.ID)
	m := service.ComputeProgress(ctx, svc, camp)
	if m.DeliveryProgress != 0 {
		t.Fatalf("delivery=%v want 0 for all-draft", m.DeliveryProgress)
	}
	if m.VerifiedProgress != 0 {
		t.Fatalf("verified=%v want 0", m.VerifiedProgress)
	}
	if m.ContentCompleteness <= 0 {
		t.Fatalf("content should be >0 when sections filled, got %v", m.ContentCompleteness)
	}
}

func TestProgress_VerifiedRequiresEvidenceEdge(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "Done task", Labels: []string{"kind:effort.task", "scope:verf-test", "work.complete"},
		Sections: []parchment.Section{
			{Name: "context", Text: "c"},
			{Name: "evidence", Text: "I pinky-promise this works"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	task.Extra = map[string]any{"verification": "true"}
	_ = svc.Proto.Store().Put(ctx, task)

	m := service.ComputeProgress(ctx, svc, task)
	if m.VerifiedProgress != 0 {
		t.Fatalf("self-claim must not verify: got %v", m.VerifiedProgress)
	}

	build, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "CI build 42", Labels: []string{"kind:delivery.build", "scope:verf-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Proto.Store().AddEdge(ctx, parchment.Edge{
		From: task.ID, Relation: parchment.RelEvidencedBy, To: build.ID,
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = svc.Proto.GetArtifact(ctx, task.ID)
	m = service.ComputeProgress(ctx, svc, task)
	if m.VerifiedProgress != 1 {
		t.Fatalf("evidenced_by delivery.build should verify: got %v", m.VerifiedProgress)
	}
}

func TestProgress_CampaignVERFFromLeafEvidence(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	camp, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "Camp", Labels: []string{"kind:effort.campaign", "scope:verf-camp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	goal, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "Goal", Parent: camp.ID, Labels: []string{"kind:effort.goal", "scope:verf-camp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "Leaf", Parent: goal.ID, Labels: []string{"kind:effort.task", "scope:verf-camp", "work.complete"},
		Sections: []parchment.Section{{Name: "context", Text: "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "go test ./...", Labels: []string{"kind:test.run", "scope:verf-camp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Proto.Store().AddEdge(ctx, parchment.Edge{
		From: task.ID, Relation: parchment.RelEvidencedBy, To: run.ID,
	}); err != nil {
		t.Fatal(err)
	}
	camp, _ = svc.Proto.GetArtifact(ctx, camp.ID)
	m := service.ComputeProgress(ctx, svc, camp)
	if m.VerifiedProgress != 1 {
		t.Fatalf("campaign VERF want 1, got %v", m.VerifiedProgress)
	}
}

func TestSetLinkDelete_Structured(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "A", Labels: []string{"kind:effort.task", "scope:mut-test"},
		Sections: []parchment.Section{{Name: "context", Text: "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "B", Labels: []string{"kind:effort.task", "scope:mut-test"},
		Sections: []parchment.Section{{Name: "context", Text: "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	setRaw, _ := json.Marshal(map[string]any{"id": a.ID, "field": "priority", "value": "high"})
	setRes, err := service.Find("set").Execute(ctx, svc, setRaw)
	if err != nil {
		t.Fatal(err)
	}
	smr := setRes.Data.(service.MutationResult)
	if smr.Count != 1 || smr.Action != "set" {
		t.Fatalf("set result: %+v", smr)
	}

	linkRaw, _ := json.Marshal(map[string]any{
		"id": a.ID, "relation": "depends_on", "targets": []string{b.ID},
	})
	linkRes, err := service.Find("link").Execute(ctx, svc, linkRaw)
	if err != nil {
		t.Fatal(err)
	}
	lmr := linkRes.Data.(service.MutationResult)
	if lmr.Count != 1 || len(lmr.Edges) != 1 {
		t.Fatalf("link result: %+v", lmr)
	}

	delRaw, _ := json.Marshal(map[string]any{"id": b.ID, "force": true})
	delRes, err := service.Find("delete").Execute(ctx, svc, delRaw)
	if err != nil {
		t.Fatal(err)
	}
	dmr := delRes.Data.(service.MutationResult)
	if dmr.Count != 1 {
		t.Fatalf("delete result: %+v", dmr)
	}
}

func TestGovernedBy_CanonicalDiscovery(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	dec, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "ADR", Labels: []string{"kind:intent.decision", "scope:canon-test", "status:decision.accepted"},
		Sections: []parchment.Section{{Name: "context", Text: "why"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	camp, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "C", Labels: []string{"kind:effort.campaign", "scope:canon-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	linkRaw, _ := json.Marshal(map[string]any{
		"id": camp.ID, "relation": service.RelGovernedBy, "targets": []string{dec.ID},
	})
	if _, err := service.Find("link").Execute(ctx, svc, linkRaw); err != nil {
		t.Fatal(err)
	}
	got, via := service.ResolveCanonicalDecision(ctx, svc, camp.ID)
	if got == nil || got.ID != dec.ID || via != service.RelGovernedBy {
		t.Fatalf("canonical=%v via=%s", got, via)
	}
	edges, _ := svc.Proto.Neighbors(ctx, camp.ID, parchment.RelJustifies, parchment.Incoming)
	if len(edges) != 1 || edges[0].From != dec.ID {
		t.Fatalf("expected stored justifies edge, got %+v", edges)
	}
}

func TestSanitizeExtraIDs_RoundTrip(t *testing.T) {
	extra := service.SanitizeExtraIDs(map[string]any{
		"github_run_id": float64(1234567890123),
		"nested":        map[string]any{"jira_id": float64(42)},
	})
	if extra["github_run_id"] != "1234567890123" {
		t.Fatalf("github_run_id=%v", extra["github_run_id"])
	}
	nested := extra["nested"].(map[string]any)
	if nested["jira_id"] != "42" {
		t.Fatalf("jira_id=%v", nested["jira_id"])
	}
}

func TestHygiene_IntentionalOrphanSkipped(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:    "standalone note",
		Labels:   []string{"kind:knowledge.note", "scope:hygiene-orphan", "hygiene:intentional_orphan"},
		Sections: []parchment.Section{{Name: "body", Text: "keep"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"scope": "hygiene-orphan", "format": "full"})
	res, err := service.Find("hygiene").Execute(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "standalone note") {
		t.Fatalf("intentional orphan should be skipped: %s", res.Text)
	}
}

func TestSchema_ActionFieldContract(t *testing.T) {
	svc := newTestService(t)
	raw, _ := json.Marshal(map[string]any{"name": "create"})
	res, err := service.Find("schema").Execute(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "mutation_id") {
		t.Fatalf("expected create fields, got %s", res.Text)
	}
}

func TestLint_IDsMany(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	a, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: "lonely", Labels: []string{"kind:effort.task", "scope:lint-many"},
		Sections: []parchment.Section{{Name: "context", Text: "c"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"ids": []string{a.ID}})
	res, err := service.Find("lint").Execute(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if res.Data == nil {
		t.Fatal("expected structured lint result")
	}
	if !strings.Contains(res.Text, "orphan") && !strings.Contains(res.Text, "no issues") {
		t.Fatalf("unexpected lint text: %s", res.Text)
	}
}
