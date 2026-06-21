package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestAutoRepair_FixesLifecycleMismatch(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "mismatch1", Title: "Mismatched Task",
		Labels: []string{"kind:effort.task", "project:test", "note.fleeting"},
	})

	op := service.Find("auto_repair")
	if op == nil {
		t.Fatal("auto_repair op not registered")
	}

	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("auto_repair error: %v", err)
	}
	if !strings.Contains(out, "1 fixed") {
		t.Errorf("expected 1 fixed, got: %s", out)
	}

	art, _ := svc.Proto.GetArtifact(ctx, "mismatch1")
	status := parchment.StatusFromLabels(art.Labels)
	if status != "work.draft" {
		t.Errorf("expected work.draft after repair, got %s", status)
	}
}

func TestAutoRepair_SkipsNonSafe(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "zombie1", Title: "Zombie Campaign",
		Labels: []string{"kind:effort.campaign", "project:test", "work.active"},
	})

	op := service.Find("auto_repair")
	raw, _ := json.Marshal(map[string]any{"scope": "test"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("auto_repair error: %v", err)
	}
	if !strings.Contains(out, "0 fixed") {
		t.Errorf("expected 0 fixed (zombie campaign not safe_autofix), got: %s", out)
	}

	art, _ := svc.Proto.GetArtifact(ctx, "zombie1")
	status := parchment.StatusFromLabels(art.Labels)
	if status != "work.active" {
		t.Errorf("zombie campaign should not have been repaired, got %s", status)
	}
}

func TestAutoRepair_DryRun(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, "test")
	ctx := context.Background()

	_ = svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID: "mismatch2", Title: "Another Mismatch",
		Labels: []string{"kind:effort.task", "project:test", "note.mature"},
	})

	op := service.Find("auto_repair")
	raw, _ := json.Marshal(map[string]any{"scope": "test", "dry_run": true})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("auto_repair error: %v", err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected dry-run in output, got: %s", out)
	}
	if !strings.Contains(out, "1 fixed") {
		t.Errorf("expected 1 fixed in dry-run, got: %s", out)
	}

	art, _ := svc.Proto.GetArtifact(ctx, "mismatch2")
	status := parchment.StatusFromLabels(art.Labels)
	if status == "work.active" {
		t.Errorf("dry-run should not change status, but it changed to work.active")
	}
}
