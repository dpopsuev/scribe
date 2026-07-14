package service_test

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func mustNote(t *testing.T, svc *service.Service, title, body string) *parchment.Artifact {
	t.Helper()
	art, err := svc.Proto.CreateArtifact(context.Background(), parchment.CreateInput{
		Labels:   []string{"kind:knowledge.note", parchment.LabelPrefixScope + "lib"},
		Title:    title,
		Sections: []parchment.Section{{Name: "body", Text: body}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return art
}

func TestLibrarian_MergeRewiresAndArchives(t *testing.T) {
	svc := newTestService(t, "lib")
	ctx := context.Background()
	keep := mustNote(t, svc, "Keeper", "keep")
	lose := mustNote(t, svc, "Loser", "lose")
	other := mustNote(t, svc, "Other", "other")
	lib := service.Find("librarian")

	if _, err := lib.Run(ctx, svc, mustJSON(map[string]any{
		"mode": "link", "from": other.ID, "to": lose.ID, "relation": parchment.RelRelatesTo,
	})); err != nil {
		t.Fatal(err)
	}
	out, err := lib.Run(ctx, svc, mustJSON(map[string]any{
		"mode": "merge", "from": lose.ID, "to": keep.ID,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "merged") {
		t.Fatalf("unexpected merge out: %s", out)
	}
	got, err := svc.Proto.GetArtifact(ctx, lose.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parchment.StatusFromLabels(got.Labels) != "archived" {
		t.Fatalf("loser status = %q, want archived", parchment.StatusFromLabels(got.Labels))
	}
	sup, _ := svc.Proto.Store().Neighbors(ctx, keep.ID, parchment.RelSupersedes, parchment.Outgoing)
	if !hasEdgeTo(sup, lose.ID) {
		t.Fatal("expected supersedes edge to loser")
	}
	rewired, _ := svc.Proto.Store().Neighbors(ctx, keep.ID, parchment.RelRelatesTo, parchment.Incoming)
	if !hasEdgeFrom(rewired, other.ID) {
		t.Fatal("expected other→keep rewired relates_to")
	}
}

func TestLibrarian_UnlinkAndStale(t *testing.T) {
	svc := newTestService(t, "lib")
	ctx := context.Background()
	keep := mustNote(t, svc, "Keeper", "keep")
	other := mustNote(t, svc, "Other", "other")
	lib := service.Find("librarian")

	if _, err := lib.Run(ctx, svc, mustJSON(map[string]any{
		"mode": "link", "from": other.ID, "to": keep.ID, "relation": parchment.RelRelatesTo,
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Run(ctx, svc, mustJSON(map[string]any{
		"mode": "unlink", "from": other.ID, "to": keep.ID, "relation": parchment.RelRelatesTo,
	})); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.Proto.Store().Neighbors(ctx, keep.ID, parchment.RelRelatesTo, parchment.Incoming)
	if hasEdgeFrom(after, other.ID) {
		t.Fatal("unlink left edge in place")
	}
	if _, err := lib.Run(ctx, svc, mustJSON(map[string]any{
		"mode": "stale", "id": keep.ID, "status": "archived",
	})); err != nil {
		t.Fatal(err)
	}
	stale, _ := svc.Proto.GetArtifact(ctx, keep.ID)
	if parchment.StatusFromLabels(stale.Labels) != "archived" {
		t.Fatalf("stale status = %q", parchment.StatusFromLabels(stale.Labels))
	}
}

func TestLibrarian_Split(t *testing.T) {
	svc := newTestService(t, "lib")
	ctx := context.Background()
	parent := mustNote(t, svc, "Parent blob", "big")
	lib := service.Find("librarian")
	out, err := lib.Run(ctx, svc, mustJSON(map[string]any{
		"mode": "split", "id": parent.ID, "text": "extracted atomic idea",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "split") {
		t.Fatalf("unexpected: %s", out)
	}
	edges, _ := svc.Proto.Store().Neighbors(ctx, parent.ID, parchment.RelRelatesTo, parchment.Outgoing)
	if len(edges) == 0 {
		t.Fatal("expected relates_to to split child")
	}
}

func hasEdgeTo(edges []parchment.Edge, to string) bool {
	for _, e := range edges {
		if e.To == to {
			return true
		}
	}
	return false
}

func hasEdgeFrom(edges []parchment.Edge, from string) bool {
	for _, e := range edges {
		if e.From == from {
			return true
		}
	}
	return false
}
