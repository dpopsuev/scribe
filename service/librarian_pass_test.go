package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestLibrarianPass_MarksEdgelessOldNotes(t *testing.T) {
	svc := newTestService(t, "lib")
	ctx := context.Background()
	oldTime := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	old, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:    []string{"kind:knowledge.note", parchment.LabelPrefixScope + "lib"},
		Title:     "Orphan note",
		Sections:  []parchment.Section{{Name: "body", Text: "lonely"}},
		CreatedAt: oldTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := service.LibrarianPass(ctx, svc, service.LibrarianPassOpts{
		Scope:  "lib",
		MaxAge: 30 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, old.ID) {
		t.Fatalf("expected %s marked (InsertedAt=%v), got %s", old.ID, old.InsertedAt, out)
	}
	got, _ := svc.Proto.GetArtifact(ctx, old.ID)
	if parchment.StatusFromLabels(got.Labels) != "archived" {
		t.Fatalf("status=%q want archived", parchment.StatusFromLabels(got.Labels))
	}
}

func TestLibrarianPass_DryRunAndSkipsLinked(t *testing.T) {
	svc := newTestService(t, "lib")
	ctx := context.Background()
	oldTime := time.Now().UTC().Add(-40 * 24 * time.Hour).Format(time.RFC3339)
	a, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:    []string{"kind:knowledge.note", parchment.LabelPrefixScope + "lib"},
		Title:     "Linked A",
		Sections:  []parchment.Section{{Name: "body", Text: "a"}},
		CreatedAt: oldTime,
	})
	b, _ := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.note", parchment.LabelPrefixScope + "lib"},
		Title:    "Linked B",
		Sections: []parchment.Section{{Name: "body", Text: "b"}},
	})
	_ = svc.Proto.Store().AddEdgeSource(ctx, a.ID, parchment.RelRelatesTo, b.ID, "manual")

	out, err := service.LibrarianPass(ctx, svc, service.LibrarianPassOpts{
		Scope:  "lib",
		MaxAge: 30 * 24 * time.Hour,
		DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, a.ID) {
		t.Fatalf("linked note should not be marked: %s", out)
	}
}
