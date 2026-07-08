package service_test

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func seedArtifact(t *testing.T, store parchment.Store, id, title, kind, scope string) {
	t.Helper()
	labels := []string{parchment.LabelPrefixKind + kind}
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}
	if err := store.Put(context.Background(), &parchment.Artifact{
		ID: id, Title: title, Labels: labels,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestCrossSourceResolver_GlobalResolution(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"alpha"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	seedArtifact(t, store, "NOTE-1", "PTP Clock Architecture", "knowledge.note", "alpha")
	seedArtifact(t, store, "NOTE-2", "DPLL Integration", "knowledge.note", "alpha")

	resolver := service.NewCrossSourceResolver(store)

	ref := parchment.WikilinkRef{Target: "PTP Clock Architecture"}
	result := resolver.ResolveRef(ctx, ref)
	if result.TargetID != "NOTE-1" {
		t.Errorf("expected NOTE-1, got %q", result.TargetID)
	}
	if result.Relation != "mentions" {
		t.Errorf("expected relation 'mentions', got %q", result.Relation)
	}
}

func TestCrossSourceResolver_ScopedResolution(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"alpha", "beta"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	seedArtifact(t, store, "A-1", "Protocol", "knowledge.note", "alpha")
	seedArtifact(t, store, "B-1", "Protocol", "knowledge.note", "beta")

	resolver := service.NewCrossSourceResolver(store)

	// Scoped: should find B-1 in beta scope
	ref := parchment.WikilinkRef{Target: "beta/Protocol"}
	result := resolver.ResolveRef(ctx, ref)
	if result.TargetID != "B-1" {
		t.Errorf("expected B-1 for beta/Protocol, got %q", result.TargetID)
	}
	if result.Scope != "beta" {
		t.Errorf("expected scope 'beta', got %q", result.Scope)
	}

	// Scoped: should find A-1 in alpha scope
	ref2 := parchment.WikilinkRef{Target: "alpha/Protocol"}
	result2 := resolver.ResolveRef(ctx, ref2)
	if result2.TargetID != "A-1" {
		t.Errorf("expected A-1 for alpha/Protocol, got %q", result2.TargetID)
	}
}

func TestCrossSourceResolver_ScopedNotFound(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"alpha"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	seedArtifact(t, store, "A-1", "Protocol", "knowledge.note", "alpha")

	resolver := service.NewCrossSourceResolver(store)
	ref := parchment.WikilinkRef{Target: "nonexistent/Protocol"}
	result := resolver.ResolveRef(ctx, ref)
	if result.TargetID != "" {
		t.Errorf("expected empty target for nonexistent scope, got %q", result.TargetID)
	}
}

func TestCrossSourceResolver_WithRelation(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	seedArtifact(t, store, "SPEC-1", "Auth Spec", "intent.spec", "test")

	resolver := service.NewCrossSourceResolver(store)
	ref := parchment.WikilinkRef{Relation: "implements", Target: "Auth Spec"}
	result := resolver.ResolveRef(ctx, ref)
	if result.TargetID != "SPEC-1" {
		t.Errorf("expected SPEC-1, got %q", result.TargetID)
	}
	if result.Relation != "implements" {
		t.Errorf("expected relation 'implements', got %q", result.Relation)
	}
}

func TestCrossSourceResolver_ResolveAll(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	seedArtifact(t, store, "N-1", "Alpha", "knowledge.note", "test")
	seedArtifact(t, store, "N-2", "Beta", "knowledge.note", "test")

	resolver := service.NewCrossSourceResolver(store)
	resolved := resolver.ResolveAll(ctx, "See [[Alpha]] and [[Beta]] and [[Nonexistent]]")
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved, got %d", len(resolved))
	}
}

func TestSyncCrossSourceWikilinks(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	seedArtifact(t, store, "TARGET-1", "Clock Model", "knowledge.note", "test")
	// Source artifact with a wikilink in its section
	if err := store.Put(ctx, &parchment.Artifact{
		ID:     "SRC-W1",
		Title:  "Source with wikilinks",
		Labels: []string{parchment.LabelPrefixKind + "knowledge.note"},
		Sections: []parchment.Section{
			{Name: "body", Text: "Refer to [[Clock Model]] for details."},
		},
	}); err != nil {
		t.Fatalf("put source: %v", err)
	}

	resolver := service.NewCrossSourceResolver(store)
	refs, err := resolver.SyncCrossSourceWikilinks(ctx, store, "SRC-W1")
	if err != nil {
		t.Fatalf("SyncCrossSourceWikilinks: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 resolved ref, got %d", len(refs))
	}
	if refs[0].TargetID != "TARGET-1" {
		t.Errorf("expected TARGET-1, got %q", refs[0].TargetID)
	}

	// Verify edge was created
	edges, _ := store.Neighbors(ctx, "SRC-W1", "mentions", parchment.Outgoing)
	if len(edges) != 1 || edges[0].To != "TARGET-1" {
		t.Errorf("expected mentions edge to TARGET-1, got %v", edges)
	}
}
