package service

import (
	"testing"

	parchment "github.com/dpopsuev/parchment"
)

func testArtifact() *parchment.Artifact {
	return &parchment.Artifact{
		ID: "test-1",
		Sections: []parchment.Section{
			{Name: "context", Text: "line one\nline two\nline three"},
			{Name: "body", Text: "# Overview\nSome overview text.\n\n## Details\nDetail paragraph.\n\n## Notes\nA note."},
		},
	}
}

func TestResolveAnchor_Section(t *testing.T) {
	art := testArtifact()
	r := ResolveAnchor(art, Selector{Section: "context"})
	if !r.Found {
		t.Fatal("expected found")
	}
	if r.Text != "line one\nline two\nline three" {
		t.Fatalf("unexpected text: %q", r.Text)
	}
}

func TestResolveAnchor_SectionLine(t *testing.T) {
	art := testArtifact()
	r := ResolveAnchor(art, Selector{Section: "context", Line: 2})
	if !r.Found || r.Text != "line two" {
		t.Fatalf("expected 'line two', got found=%v text=%q", r.Found, r.Text)
	}
}

func TestResolveAnchor_LineOnly(t *testing.T) {
	art := testArtifact()
	r := ResolveAnchor(art, Selector{Line: 3})
	if !r.Found || r.Text != "line three" {
		t.Fatalf("expected 'line three', got found=%v text=%q", r.Found, r.Text)
	}
}

func TestResolveAnchor_HeadingAnchor(t *testing.T) {
	art := testArtifact()
	r := ResolveAnchor(art, Selector{Anchor: "details"})
	if !r.Found {
		t.Fatal("expected found for anchor 'details'")
	}
	if r.Section != "body" {
		t.Fatalf("expected section 'body', got %q", r.Section)
	}
	want := "## Details\nDetail paragraph.\n"
	if r.Text != want {
		t.Fatalf("expected %q, got %q", want, r.Text)
	}
}

func TestResolveAnchor_HeadingAnchorNotFound(t *testing.T) {
	art := testArtifact()
	r := ResolveAnchor(art, Selector{Anchor: "nonexistent"})
	if r.Found {
		t.Fatal("expected not found for nonexistent anchor")
	}
}

func TestResolveAnchor_Zero(t *testing.T) {
	art := testArtifact()
	r := ResolveAnchor(art, Selector{})
	if r.Found {
		t.Fatal("expected not found for zero selector")
	}
}

func TestResolveAnchor_OutOfBoundsLine(t *testing.T) {
	art := testArtifact()
	r := ResolveAnchor(art, Selector{Section: "context", Line: 99})
	if r.Found {
		t.Fatal("expected not found for out-of-bounds line")
	}
}

func TestHeadingSlug(t *testing.T) {
	tests := []struct {
		heading string
		want    string
	}{
		{"Overview", "overview"},
		{"Some Details Here", "some-details-here"},
		{"API v2.0", "api-v20"},
		{"under_score", "under-score"},
	}
	for _, tt := range tests {
		if got := headingSlug(tt.heading); got != tt.want {
			t.Errorf("headingSlug(%q) = %q, want %q", tt.heading, got, tt.want)
		}
	}
}
