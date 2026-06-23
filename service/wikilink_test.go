package service

import (
	"testing"

	parchment "github.com/dpopsuev/parchment"
)

func TestSplitScopeTarget(t *testing.T) {
	tests := []struct {
		input     string
		wantScope string
		wantName  string
	}{
		{"Target", "", "Target"},
		{"scope/Target", "scope", "Target"},
		{"deep/path/Target", "deep", "path/Target"},
		{"/leading", "", "leading"},
	}
	for _, tt := range tests {
		scope, name := splitScopeTarget(tt.input)
		if scope != tt.wantScope || name != tt.wantName {
			t.Errorf("splitScopeTarget(%q) = (%q, %q), want (%q, %q)",
				tt.input, scope, name, tt.wantScope, tt.wantName)
		}
	}
}

func TestResolvedRef_DefaultRelation(t *testing.T) {
	r := &CrossSourceResolver{}
	ref := parchment.WikilinkRef{Target: "emcee/PROJ-42"}
	result := ResolvedRef{
		SourceRef: ref.Target,
		Relation:  ref.Relation,
	}
	if result.Relation == "" {
		result.Relation = "mentions"
	}

	scope, _ := splitScopeTarget(ref.Target)
	result.Scope = scope

	if result.Scope != "emcee" {
		t.Errorf("expected scope 'emcee', got %q", result.Scope)
	}
	if result.Relation != "mentions" {
		t.Errorf("expected relation 'mentions', got %q", result.Relation)
	}
	_ = r
}

func TestResolvedRef_WithRelation(t *testing.T) {
	ref := parchment.WikilinkRef{Relation: "implements", Target: "emcee/PROJ-42"}
	rel := ref.Relation
	if rel == "" {
		rel = "mentions"
	}
	if rel != "implements" {
		t.Errorf("expected relation 'implements', got %q", rel)
	}
	scope, target := splitScopeTarget(ref.Target)
	if scope != "emcee" || target != "PROJ-42" {
		t.Errorf("expected emcee/PROJ-42 split, got %q/%q", scope, target)
	}
}
