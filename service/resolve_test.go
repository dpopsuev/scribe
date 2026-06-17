package service_test

import (
	"context"
	"fmt"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

type mockResolver struct {
	sections []parchment.Section
	err      error
}

func (m *mockResolver) Resolve(_ context.Context, _ string) (*service.ResolvedContent, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.ResolvedContent{
		Sections: m.sections,
		Fresh:    true,
	}, nil
}

func TestResolve_Success(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if err := svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID:    "test-art",
		Title: "Test",
		Extra: map[string]any{"ref_backend": service.BackendEmcee, "ref_id": "jira:TEST-1"},
	}); err != nil {
		t.Fatal(err)
	}

	resolvers := map[string]service.Resolver{
		service.BackendEmcee: &mockResolver{
			sections: []parchment.Section{{Name: "description", Text: "live content"}},
		},
	}

	result, err := service.Resolve(ctx, svc, "test-art", resolvers)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Fresh {
		t.Error("expected fresh=true")
	}
	if len(result.Sections) != 1 || result.Sections[0].Text != "live content" {
		t.Errorf("unexpected sections: %v", result.Sections)
	}
}

func TestResolve_ResolverFails_ReturnsCached(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if err := svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID:       "test-art",
		Title:    "Test",
		Extra:    map[string]any{"ref_backend": service.BackendEmcee, "ref_id": "jira:TEST-1"},
		Sections: []parchment.Section{{Name: "cached", Text: "old data"}},
	}); err != nil {
		t.Fatal(err)
	}

	resolvers := map[string]service.Resolver{
		service.BackendEmcee: &mockResolver{err: fmt.Errorf("connection refused")},
	}

	result, err := service.Resolve(ctx, svc, "test-art", resolvers)
	if err != nil {
		t.Fatal(err)
	}
	if result.Fresh {
		t.Error("expected fresh=false")
	}
	if !result.Stale {
		t.Error("expected stale=true")
	}
	if len(result.Sections) != 1 || result.Sections[0].Name != "cached" {
		t.Errorf("expected cached sections, got: %v", result.Sections)
	}
}

func TestResolve_NoResolver_ReturnsCached(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if err := svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID:       "test-art",
		Title:    "Test",
		Extra:    map[string]any{"ref_backend": "unknown_tool", "ref_id": "x"},
		Sections: []parchment.Section{{Name: "data", Text: "stored"}},
	}); err != nil {
		t.Fatal(err)
	}

	result, err := service.Resolve(ctx, svc, "test-art", map[string]service.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fresh {
		t.Error("expected fresh=false (no resolver)")
	}
}

func TestResolve_NoRefBackend_Error(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	if err := svc.Proto.Store().Put(ctx, &parchment.Artifact{
		ID:    "test-art",
		Title: "No Ref",
		Extra: map[string]any{"foo": "bar"},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := service.Resolve(ctx, svc, "test-art", map[string]service.Resolver{})
	if err == nil {
		t.Error("expected error for artifact without ref_backend")
	}
}
