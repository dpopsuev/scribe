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
