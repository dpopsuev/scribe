package service

import (
	"testing"

	parchment "github.com/dpopsuev/parchment"
)

func TestSelectorRoundTrip(t *testing.T) {
	art := &parchment.Artifact{ID: "test-1"}

	sel := Selector{Section: "body", Line: 42, Anchor: "heading-1"}
	SetSelector(art, sel)

	got := GetSelector(art)
	if got.Section != sel.Section || got.Line != sel.Line || got.Anchor != sel.Anchor {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, sel)
	}
}

func TestSelectorIsZero(t *testing.T) {
	if !(Selector{}).IsZero() {
		t.Fatal("zero selector should be zero")
	}
	if (Selector{Section: "x"}).IsZero() {
		t.Fatal("non-zero selector should not be zero")
	}
}

func TestSelectorString(t *testing.T) {
	tests := []struct {
		sel  Selector
		want string
	}{
		{Selector{Anchor: "h1"}, "#h1"},
		{Selector{Section: "body", Line: 10}, "body:10"},
		{Selector{Section: "body"}, "body"},
		{Selector{Line: 5}, "L5"},
		{Selector{}, ""},
	}
	for _, tt := range tests {
		if got := tt.sel.String(); got != tt.want {
			t.Errorf("Selector%+v.String() = %q, want %q", tt.sel, got, tt.want)
		}
	}
}

func TestEdgeSelectorRoundTrip(t *testing.T) {
	art := &parchment.Artifact{ID: "kernel-1"}

	es := EdgeSelector{
		TargetID: "pointer-1",
		Relation: "traces_to",
		Selector: Selector{Section: "context", Line: 7},
	}
	SetEdgeSelector(art, es)

	got := GetEdgeSelectors(art)
	if len(got) != 1 {
		t.Fatalf("expected 1 edge selector, got %d", len(got))
	}
	if got[0].TargetID != es.TargetID || got[0].Selector.Line != 7 {
		t.Fatalf("roundtrip mismatch: got %+v", got[0])
	}

	es2 := EdgeSelector{
		TargetID: "pointer-1",
		Relation: "traces_to",
		Selector: Selector{Section: "context", Line: 15},
	}
	SetEdgeSelector(art, es2)
	got = GetEdgeSelectors(art)
	if len(got) != 1 {
		t.Fatalf("expected update-in-place, got %d", len(got))
	}
	if got[0].Selector.Line != 15 {
		t.Fatalf("expected line 15 after update, got %d", got[0].Selector.Line)
	}

	es3 := EdgeSelector{
		TargetID: "pointer-2",
		Relation: "produced_by",
		Selector: Selector{Anchor: "fig1"},
	}
	SetEdgeSelector(art, es3)
	got = GetEdgeSelectors(art)
	if len(got) != 2 {
		t.Fatalf("expected 2 edge selectors after new target, got %d", len(got))
	}
}
