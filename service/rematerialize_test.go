package service

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/testkit"
)

func TestRematerialize_NewArtifacts(t *testing.T) {
	store := testkit.NewStore(t)
	ctx := context.Background()

	arts := []*parchment.Artifact{
		{ID: "rm-1", Title: "Artifact One", Labels: []string{"kind:knowledge.note"}},
		{ID: "rm-2", Title: "Artifact Two", Labels: []string{"kind:knowledge.note"}},
	}

	result := Rematerialize(ctx, store, arts)
	if result.Created != 2 {
		t.Fatalf("expected 2 created, got %d", result.Created)
	}
	if result.Updated != 0 || result.Unchanged != 0 {
		t.Fatalf("expected 0 updated/unchanged, got %d/%d", result.Updated, result.Unchanged)
	}
}

func TestRematerialize_Unchanged(t *testing.T) {
	store := testkit.NewStore(t)
	ctx := context.Background()

	arts := []*parchment.Artifact{
		{ID: "rm-1", Title: "Artifact One", Labels: []string{"kind:knowledge.note"}},
	}
	Rematerialize(ctx, store, arts)

	arts2 := []*parchment.Artifact{
		{ID: "rm-1", Title: "Artifact One", Labels: []string{"kind:knowledge.note"}},
	}
	result := Rematerialize(ctx, store, arts2)
	if result.Unchanged != 1 {
		t.Fatalf("expected 1 unchanged, got %d", result.Unchanged)
	}
}

func TestRematerialize_Changed(t *testing.T) {
	store := testkit.NewStore(t)
	ctx := context.Background()

	arts := []*parchment.Artifact{
		{ID: "rm-1", Title: "Original Title", Labels: []string{"kind:knowledge.note"}},
	}
	Rematerialize(ctx, store, arts)

	arts2 := []*parchment.Artifact{
		{ID: "rm-1", Title: "Updated Title", Labels: []string{"kind:knowledge.note"}},
	}
	result := Rematerialize(ctx, store, arts2)
	if result.Updated != 1 {
		t.Fatalf("expected 1 updated, got %d", result.Updated)
	}
	if len(result.Changes) != 1 || result.Changes[0].ChangeDesc != "title" {
		t.Fatalf("expected title change description, got %+v", result.Changes)
	}
}

func TestDiffDescription(t *testing.T) {
	old := &parchment.Artifact{
		ID:    "x",
		Title: "old",
		Sections: []parchment.Section{
			{Name: "body", Text: "hello"},
		},
		Labels: []string{"a"},
	}
	curr := &parchment.Artifact{
		ID:    "x",
		Title: "new",
		Sections: []parchment.Section{
			{Name: "body", Text: "world"},
		},
		Labels: []string{"a", "b"},
	}
	desc := diffDescription(old, curr)
	if desc != "title, sections, labels" {
		t.Fatalf("expected 'title, sections, labels', got %q", desc)
	}
}
