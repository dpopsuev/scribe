package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

func TestHygiene_StaleKnowledge(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	staleTime := time.Now().Add(-120 * 24 * time.Hour)
	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "stale1", Title: "Old Note",
		Labels:    []string{"kind:knowledge.note", "project:test", "note.fleeting"},
		UpdatedAt: staleTime,
	})

	out := runHygiene(t, svc)
	if !strings.Contains(out, "stale_knowledge") {
		t.Errorf("expected stale_knowledge finding, got: %s", out)
	}
	if !strings.Contains(out, "Old Note") {
		t.Errorf("expected 'Old Note' in output, got: %s", out)
	}
}

func TestHygiene_StaleKnowledge_EvergreenExcluded(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	staleTime := time.Now().Add(-200 * 24 * time.Hour)
	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "eg1", Title: "Evergreen Note",
		Labels:    []string{"kind:knowledge.note", "project:test", "note.evergreen"},
		UpdatedAt: staleTime,
	})

	out := runHygiene(t, svc)
	if strings.Contains(out, "stale_knowledge") {
		t.Errorf("evergreen notes should NOT appear as stale, got: %s", out)
	}
}

func TestHygiene_IncompleteKnowledge(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "inc1", Title: "Incomplete Concept",
		Labels: []string{"kind:knowledge.concept", "project:test", "work.active"},
	})

	out := runHygiene(t, svc)
	if !strings.Contains(out, "incomplete_knowledge") {
		t.Errorf("expected incomplete_knowledge finding, got: %s", out)
	}
}

func TestHygiene_LegacyKnowledge(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "legacy1", Title: "Legacy Note",
		Labels: []string{"kind:knowledge.note", "project:test", "note.fleeting"},
	})
	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "done1", Title: "Done Task",
		Labels: []string{"kind:effort.task", "project:test", "work.complete"},
	})
	_ = svc.Proto.Store().AddEdge(ctx, parchment.Edge{
		From: "done1", To: "legacy1", Relation: "cites",
	})

	out := runHygiene(t, svc)
	if !strings.Contains(out, "legacy_knowledge") {
		t.Errorf("expected legacy_knowledge finding, got: %s", out)
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
