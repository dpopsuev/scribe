package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestOpQuery_WorkingSet_EmptyScope(t *testing.T) {
	svc := newTestService(t, "ws-empty")
	ctx := context.Background()

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"mode": "working_set", "scope": "ws-empty"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("expected JSON, got %q: %v", out, err)
	}
	if _, ok := got["campaigns"]; !ok {
		t.Fatalf("missing campaigns: %s", out)
	}
	if _, ok := got["ready"]; !ok {
		t.Fatalf("missing ready: %s", out)
	}
	if _, ok := got["recent"]; !ok {
		t.Fatalf("missing recent: %s", out)
	}
	if _, ok := got["hygiene_top"]; !ok {
		t.Fatalf("missing hygiene_top: %s", out)
	}
	if _, ok := got["repair"]; ok {
		t.Fatalf("repair key should be removed: %s", out)
	}
}

func TestOpQuery_WorkingSet_ReadyUnblockedLeaf(t *testing.T) {
	svc := newTestService(t, "ws-ready")
	ctx := context.Background()

	camp, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.campaign", "scope:ws-ready", "work.active"},
		Title:  "Campaign",
	})
	if err != nil {
		t.Fatal(err)
	}
	goal, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:effort.goal", "scope:ws-ready", "work.active"},
		Title:  "Goal",
		Parent: camp.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:effort.task", "scope:ws-ready", "work.active", "priority:high"},
		Title:    "First",
		Parent:   goal.ID,
		Sections: []parchment.Section{{Name: "context", Text: "do first"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:    []string{"kind:effort.task", "scope:ws-ready", "work.active", "priority:medium"},
		Title:     "Second",
		Parent:    goal.ID,
		DependsOn: []string{first.ID},
		Sections:  []parchment.Section{{Name: "context", Text: "do second"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{"mode": "working_set", "scope": "ws-ready"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Campaigns []struct {
			ID string `json:"id"`
		} `json:"campaigns"`
		Ready []struct {
			ID string `json:"id"`
		} `json:"ready"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	foundCamp := false
	for _, c := range got.Campaigns {
		if c.ID == camp.ID {
			foundCamp = true
		}
	}
	if !foundCamp {
		t.Fatalf("expected campaign %s in campaigns: %s", camp.ID, out)
	}
	foundFirst, foundSecond := false, false
	for _, r := range got.Ready {
		if r.ID == first.ID {
			foundFirst = true
		}
		if r.ID == second.ID {
			foundSecond = true
		}
	}
	if !foundFirst {
		t.Fatalf("expected ready first task %s in %s", first.ID, out)
	}
	if foundSecond {
		t.Fatalf("blocked second task %s should not be ready: %s", second.ID, out)
	}
}

func TestOpQuery_WorkingSet_ExcerptChars(t *testing.T) {
	svc := newTestService(t, "ws-excerpt")
	ctx := context.Background()

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.note", "scope:ws-excerpt", "note.fleeting"},
		Title:    "Note",
		Sections: []parchment.Section{{Name: "body", Text: "abcdefghijklmnopqrstuvwxyz"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	op := service.Find("query")
	raw, _ := json.Marshal(map[string]any{
		"mode": "working_set", "scope": "ws-excerpt", "excerpt_chars": 5,
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"excerpt":"abcde"`) {
		t.Fatalf("expected excerpt abcde in %s", out)
	}
}
