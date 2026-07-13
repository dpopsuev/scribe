package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func runHygiene(t *testing.T, svc *service.Service) string {
	t.Helper()
	op := service.Find("hygiene")
	if op == nil {
		t.Fatal("hygiene op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(context.Background(), svc, raw)
	if err != nil {
		t.Fatalf("hygiene error: %v", err)
	}
	return out
}

func TestHygiene_IncompleteKnowledge_MustSections(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	// knowledge.source has must-section "summary" in the schema.
	// Seed the label trait so MustSections returns it.
	parchment.SeedLabelTraits(ctx, svc.Proto.Store())

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "inc1", Title: "Incomplete Source",
		Labels: []string{"kind:knowledge.source", "project:test", "work.active"},
	})

	out := runHygiene(t, svc)
	if !strings.Contains(out, "incomplete_knowledge") {
		t.Errorf("expected incomplete_knowledge finding, got: %s", out)
	}
}

func TestHygiene_ZombieCampaign(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "camp1", Title: "Zombie Campaign",
		Labels: []string{"kind:effort.campaign", "project:test", "work.active"},
	})

	out := runHygiene(t, svc)
	if !strings.Contains(out, "zombie_campaign") {
		t.Errorf("expected zombie_campaign finding, got: %s", out)
	}
}

func TestHygiene_Orphan(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "orph1", Title: "Lonely Artifact",
		Labels: []string{"kind:effort.task", "project:test", "work.draft"},
	})

	out := runHygiene(t, svc)
	if !strings.Contains(out, "orphan") {
		t.Errorf("expected orphan finding, got: %s", out)
	}
}

func TestHygiene_Clean(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")

	out := runHygiene(t, svc)
	if !strings.Contains(out, "clean") {
		t.Errorf("expected clean result, got: %s", out)
	}
}

func runHygieneJSON(t *testing.T, svc *service.Service) service.HygieneOutput {
	t.Helper()
	op := service.Find("hygiene")
	if op == nil {
		t.Fatal("hygiene op not registered")
	}
	raw, _ := json.Marshal(map[string]any{"scope": "test", "format": "full"})
	out, err := op.Run(context.Background(), svc, raw)
	if err != nil {
		t.Fatalf("hygiene error: %v", err)
	}
	var ho service.HygieneOutput
	if err := json.Unmarshal([]byte(out), &ho); err != nil {
		t.Fatalf("failed to unmarshal hygiene JSON: %v\nraw: %s", err, out)
	}
	return ho
}

func TestHygiene_FormatFull_ReturnsJSON(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "camp-json", Title: "JSON Campaign",
		Labels: []string{"kind:effort.campaign", "project:test", "work.active"},
	})

	ho := runHygieneJSON(t, svc)
	if ho.Total == 0 {
		t.Error("expected at least one finding in JSON output")
	}
	if len(ho.Findings) != ho.Total {
		t.Errorf("findings count mismatch: total=%d, findings=%d", ho.Total, len(ho.Findings))
	}
	if len(ho.Summary) == 0 {
		t.Error("expected non-empty summary in JSON output")
	}
}

func TestHygiene_FindingsAreSortedByScore(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	// Create a zombie campaign (high/certain → score 9) and an orphan (low/likely → score 2)
	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "camp-sort", Title: "Zombie",
		Labels: []string{"kind:effort.campaign", "project:test", "work.active"},
	})
	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "orphan-sort", Title: "Orphan",
		Labels: []string{"kind:effort.task", "project:test", "work.draft"},
	})

	ho := runHygieneJSON(t, svc)
	if len(ho.Findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(ho.Findings))
	}
	if ho.Findings[0].Score < ho.Findings[len(ho.Findings)-1].Score {
		t.Error("findings should be sorted by score descending")
	}
}



func TestHygiene_OwnerFromProvenance(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "prov-test", Title: "Provenance Test Campaign",
		Labels: []string{"kind:effort.campaign", "project:test", "work.active"},
		Extra: map[string]any{
			"provenance": map[string]any{
				"session_id": "ses-abc123",
			},
		},
	})

	ho := runHygieneJSON(t, svc)
	found := false
	for _, f := range ho.Findings {
		if f.ID == "prov-test" {
			found = true
			if f.Owner != "ses-abc123" {
				t.Errorf("expected owner=ses-abc123, got %q", f.Owner)
			}
		}
	}
	if !found {
		t.Error("expected finding for prov-test")
	}
}
