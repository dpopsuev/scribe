package service_test

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// TestRematerializePipeline tests the full pipeline:
// materialize → detect changes → propagate to kernels.
func TestRematerializePipeline(t *testing.T) {
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	// Step 1: Initial materialization — 2 pointer artifacts
	pointers := []*parchment.Artifact{
		{ID: "PTR-A", Title: "Pointer A", Labels: []string{"kind:knowledge.source", "scope:test"},
			Sections: []parchment.Section{{Name: "body", Text: "Original content A."}}},
		{ID: "PTR-B", Title: "Pointer B", Labels: []string{"kind:knowledge.source", "scope:test"},
			Sections: []parchment.Section{{Name: "body", Text: "Original content B."}}},
	}
	r1 := service.Rematerialize(ctx, store, pointers)
	if r1.Created != 2 {
		t.Fatalf("step 1: expected 2 created, got %d", r1.Created)
	}

	// Step 2: Create kernels linked to each pointer, then confirm them
	_ = service.CreateKernel(ctx, proto, "KRN-A1", "Kernel from A", "PTR-A", "test",
		service.Selector{Section: "body"}, "Extracted from A.")
	_ = service.CreateKernel(ctx, proto, "KRN-B1", "Kernel from B", "PTR-B", "test",
		service.Selector{Section: "body"}, "Extracted from B.")
	_ = service.ConfirmKernel(ctx, proto, "KRN-A1")
	_ = service.ConfirmKernel(ctx, proto, "KRN-B1")

	// Step 3: Re-materialize with PTR-A changed, PTR-B unchanged
	pointers2 := []*parchment.Artifact{
		{ID: "PTR-A", Title: "Pointer A", Labels: []string{"kind:knowledge.source", "scope:test"},
			Sections: []parchment.Section{{Name: "body", Text: "UPDATED content A."}}},
		{ID: "PTR-B", Title: "Pointer B", Labels: []string{"kind:knowledge.source", "scope:test"},
			Sections: []parchment.Section{{Name: "body", Text: "Original content B."}}},
	}
	r2 := service.Rematerialize(ctx, store, pointers2)
	if r2.Updated != 1 {
		t.Fatalf("step 3: expected 1 updated, got %d", r2.Updated)
	}
	if r2.Unchanged != 1 {
		t.Fatalf("step 3: expected 1 unchanged, got %d", r2.Unchanged)
	}

	// Step 4: Propagate changes to kernels
	propResults := service.PropagateFromChanges(ctx, proto, r2)
	if len(propResults) != 1 {
		t.Fatalf("step 4: expected 1 propagation result, got %d", len(propResults))
	}
	if propResults[0].PointerID != "PTR-A" {
		t.Errorf("propagated to wrong pointer: %s", propResults[0].PointerID)
	}
	if len(propResults[0].Invalidated) != 1 || propResults[0].Invalidated[0] != "KRN-A1" {
		t.Errorf("expected KRN-A1 invalidated, got %v", propResults[0].Invalidated)
	}

	// Step 5: Verify final states
	artA, _ := store.Get(ctx, "KRN-A1")
	if parchment.StatusFromLabels(artA.Labels) != service.StatusKernelPending {
		t.Errorf("KRN-A1 should be pending after propagation, got %s",
			parchment.StatusFromLabels(artA.Labels))
	}
	artB, _ := store.Get(ctx, "KRN-B1")
	if parchment.StatusFromLabels(artB.Labels) != service.StatusKernelConfirmed {
		t.Errorf("KRN-B1 should still be confirmed (unchanged pointer), got %s",
			parchment.StatusFromLabels(artB.Labels))
	}
}

// TestRematerialize_IdempotentRerun verifies that re-materializing
// the same data twice produces no changes on the second run.
func TestRematerialize_IdempotentRerun(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	arts := []*parchment.Artifact{
		{ID: "IDEM-1", Title: "Stable", Labels: []string{"kind:knowledge.note"}},
		{ID: "IDEM-2", Title: "Also stable", Labels: []string{"kind:knowledge.note"}},
	}

	r1 := service.Rematerialize(ctx, store, arts)
	if r1.Created != 2 {
		t.Fatalf("run 1: expected 2 created, got %d", r1.Created)
	}

	arts2 := []*parchment.Artifact{
		{ID: "IDEM-1", Title: "Stable", Labels: []string{"kind:knowledge.note"}},
		{ID: "IDEM-2", Title: "Also stable", Labels: []string{"kind:knowledge.note"}},
	}
	r2 := service.Rematerialize(ctx, store, arts2)
	if r2.Unchanged != 2 {
		t.Fatalf("run 2: expected 2 unchanged, got %d (created=%d updated=%d)",
			r2.Unchanged, r2.Created, r2.Updated)
	}
	if len(r2.Changes) != 0 {
		t.Errorf("run 2: expected no changes, got %d", len(r2.Changes))
	}
}

// TestRematerialize_MultipleDiffs verifies multiple artifacts changing at once.
func TestRematerialize_MultipleDiffs(t *testing.T) {
	store := parchment.NewMemoryStore()
	parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	ctx := context.Background()

	arts := []*parchment.Artifact{
		{ID: "MD-1", Title: "First", Labels: []string{"kind:knowledge.note"}},
		{ID: "MD-2", Title: "Second", Labels: []string{"kind:knowledge.note"}},
		{ID: "MD-3", Title: "Third", Labels: []string{"kind:knowledge.note"}},
	}
	service.Rematerialize(ctx, store, arts)

	arts2 := []*parchment.Artifact{
		{ID: "MD-1", Title: "First UPDATED", Labels: []string{"kind:knowledge.note"}},
		{ID: "MD-2", Title: "Second", Labels: []string{"kind:knowledge.note"}},
		{ID: "MD-3", Title: "Third UPDATED", Labels: []string{"kind:knowledge.note"}},
	}
	r := service.Rematerialize(ctx, store, arts2)
	if r.Updated != 2 {
		t.Errorf("expected 2 updated, got %d", r.Updated)
	}
	if r.Unchanged != 1 {
		t.Errorf("expected 1 unchanged, got %d", r.Unchanged)
	}

	changedIDs := make(map[string]bool)
	for _, c := range r.Changes {
		changedIDs[c.ID] = true
	}
	if !changedIDs["MD-1"] || !changedIDs["MD-3"] {
		t.Errorf("expected MD-1 and MD-3 in changes, got %v", r.Changes)
	}
}
