package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func runChangelog(t *testing.T, svc *service.Service, id string, limit int) string {
	t.Helper()
	op := service.Find("changelog")
	if op == nil {
		t.Fatal("changelog op not registered")
	}
	in := map[string]any{"id": id}
	if limit > 0 {
		in["limit"] = limit
	}
	raw, _ := json.Marshal(in)
	out, err := op.Run(context.Background(), svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestChangelog_NoRevisions(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	art, _ := svc.Proto.CreateArtifact(context.Background(), parchment.CreateInput{
		Title:  "fresh",
		Labels: []string{"kind:effort.task"},
	})
	out := runChangelog(t, svc, art.ID, 0)
	if out != "no revisions found" {
		t.Errorf("expected 'no revisions found', got %q", out)
	}
}

func TestChangelog_ShowsTitleDiff(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "original",
		Labels: []string{"kind:effort.task"},
	})
	svc.Proto.SetField(ctx, []string{art.ID}, "title", "renamed") //nolint:errcheck // test setup

	out := runChangelog(t, svc, art.ID, 0)
	if !strings.Contains(out, `title: "original"`) {
		t.Errorf("changelog should show old title, got:\n%s", out)
	}
	if !strings.Contains(out, `"renamed"`) {
		t.Errorf("changelog should show new title, got:\n%s", out)
	}
}

func TestChangelog_ShowsStatusChange(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "status test",
		Labels: []string{"kind:effort.task"},
	})
	svc.Proto.SetField(ctx, []string{art.ID}, "status", "work.active", parchment.SetFieldOptions{BypassGuards: true}) //nolint:errcheck // test setup

	out := runChangelog(t, svc, art.ID, 0)
	if !strings.Contains(out, "status:") {
		t.Errorf("changelog should show status change, got:\n%s", out)
	}
}

func TestChangelog_ShowsSectionChanges(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:    "section test",
		Labels:   []string{"kind:effort.task"},
		Sections: []parchment.Section{{Name: "context", Text: "initial"}},
	})
	// Update the section content by re-putting
	got, _ := svc.Proto.GetArtifact(ctx, art.ID)
	got.Sections = []parchment.Section{{Name: "context", Text: "updated content"}}
	svc.Proto.Store().Put(ctx, got) //nolint:errcheck // test setup

	out := runChangelog(t, svc, art.ID, 0)
	if !strings.Contains(out, `section "context": modified`) {
		t.Errorf("changelog should show section modification, got:\n%s", out)
	}
}

func TestChangelog_LimitRespectsInput(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "v0",
		Labels: []string{"kind:effort.task"},
	})
	for i := 1; i <= 5; i++ {
		svc.Proto.SetField(ctx, []string{art.ID}, "title", "v"+string(rune('0'+i))) //nolint:errcheck // test setup
	}

	out := runChangelog(t, svc, art.ID, 2)
	// With limit=2, we get 2 revisions → at most 2 diff blocks (current vs rev[0], rev[0] vs rev[1])
	count := strings.Count(out, "### rev")
	if count > 2 {
		t.Errorf("expected at most 2 diff blocks with limit=2, got %d:\n%s", count, out)
	}
}

func TestChangelog_RequiresID(t *testing.T) {
	t.Parallel()
	op := service.Find("changelog")
	if op == nil {
		t.Fatal("changelog op not registered")
	}
	svc := newTestService(t)
	raw, _ := json.Marshal(map[string]any{})
	_, err := op.Run(context.Background(), svc, raw)
	if err == nil || !strings.Contains(err.Error(), "id required") {
		t.Errorf("expected 'id required' error, got %v", err)
	}
}

func TestChangelog_MultipleRevisionDiffs(t *testing.T) {
	t.Parallel()
	svc := newTestService(t)
	ctx := context.Background()
	art, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  "alpha",
		Labels: []string{"kind:effort.task"},
	})
	svc.Proto.SetField(ctx, []string{art.ID}, "title", "beta")  //nolint:errcheck // test setup
	svc.Proto.SetField(ctx, []string{art.ID}, "title", "gamma") //nolint:errcheck // test setup

	out := runChangelog(t, svc, art.ID, 0)
	if !strings.Contains(out, "2 revision(s)") {
		t.Errorf("should report 2 revisions, got:\n%s", out)
	}
	if !strings.Contains(out, "current") {
		t.Errorf("should show 'current' diff block, got:\n%s", out)
	}
}
