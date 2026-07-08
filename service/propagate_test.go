package service_test

import (
	"context"
	"fmt"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// setupPointerWithKernels creates a pointer artifact and N kernels that
// traces_to it. Returns the pointer ID and kernel IDs.
func setupPointerWithKernels(t *testing.T, proto *parchment.Protocol, n int) (pointerID string, kernelIDs []string) { //nolint:nonamedreturns // named returns for clarity
	t.Helper()
	ctx := context.Background()

	pointerID = "PTR-1"
	if err := proto.Store().Put(ctx, &parchment.Artifact{
		ID:     pointerID,
		Title:  "Source pointer",
		Labels: []string{parchment.LabelPrefixKind + "knowledge.source", "scope:test"},
	}); err != nil {
		t.Fatalf("put pointer: %v", err)
	}

	kernelIDs = make([]string, 0, n)
	for i := range n {
		kid := fmt.Sprintf("KRN-P%d", i)
		if err := service.CreateKernel(ctx, proto, kid, fmt.Sprintf("Kernel %d", i),
			pointerID, "test", service.Selector{}, fmt.Sprintf("Content %d.", i)); err != nil {
			t.Fatalf("create kernel %d: %v", i, err)
		}
		kernelIDs = append(kernelIDs, kid)
	}
	return pointerID, kernelIDs
}

func TestPropagateChanges_InvalidatesConfirmedKernels(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	pointerID, kernelIDs := setupPointerWithKernels(t, proto, 3)

	// Confirm all kernels
	for _, kid := range kernelIDs {
		if err := service.ConfirmKernel(ctx, proto, kid); err != nil {
			t.Fatalf("confirm %s: %v", kid, err)
		}
	}

	// Propagate a change to the pointer
	result := service.PropagateChanges(ctx, proto, pointerID)
	if len(result.Invalidated) != 3 {
		t.Fatalf("expected 3 invalidated, got %d: %v", len(result.Invalidated), result.Invalidated)
	}

	// All kernels should be back to pending
	for _, kid := range kernelIDs {
		art, _ := proto.Store().Get(ctx, kid)
		status := parchment.StatusFromLabels(art.Labels)
		if status != service.StatusKernelPending {
			t.Errorf("%s: status = %q, want kernel.pending", kid, status)
		}
	}
}

func TestPropagateChanges_LeavsPendingAlone(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	pointerID, _ := setupPointerWithKernels(t, proto, 2)

	// Kernels are pending by default — propagation should NOT touch them
	result := service.PropagateChanges(ctx, proto, pointerID)
	if len(result.Invalidated) != 0 {
		t.Errorf("expected 0 invalidated for pending kernels, got %d", len(result.Invalidated))
	}
	if len(result.AlreadyStale) != 2 {
		t.Errorf("expected 2 already_stale, got %d", len(result.AlreadyStale))
	}
}

func TestPropagateChanges_LeavesRejectedAlone(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	pointerID, kernelIDs := setupPointerWithKernels(t, proto, 1)
	_ = service.RejectKernel(ctx, proto, kernelIDs[0])

	result := service.PropagateChanges(ctx, proto, pointerID)
	if len(result.Invalidated) != 0 {
		t.Errorf("expected 0 invalidated for rejected kernel, got %d", len(result.Invalidated))
	}
	if len(result.AlreadyStale) != 1 {
		t.Errorf("expected 1 already_stale, got %d", len(result.AlreadyStale))
	}
}

func TestPropagateChanges_MixedStatuses(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	pointerID, kernelIDs := setupPointerWithKernels(t, proto, 3)

	// KRN-P0: confirmed (should be invalidated)
	_ = service.ConfirmKernel(ctx, proto, kernelIDs[0])
	// KRN-P1: pending (should be left alone)
	// KRN-P2: rejected (should be left alone)
	_ = service.RejectKernel(ctx, proto, kernelIDs[2])

	result := service.PropagateChanges(ctx, proto, pointerID)
	if len(result.Invalidated) != 1 {
		t.Errorf("expected 1 invalidated, got %d", len(result.Invalidated))
	}
	if len(result.AlreadyStale) != 2 {
		t.Errorf("expected 2 already_stale, got %d", len(result.AlreadyStale))
	}
}

func TestPropagateChanges_IgnoresNonKernelEdges(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	pointerID := "PTR-2"
	_ = proto.Store().Put(ctx, &parchment.Artifact{
		ID:     pointerID,
		Title:  "Source",
		Labels: []string{parchment.LabelPrefixKind + "knowledge.source"},
	})
	// Non-kernel artifact with traces_to edge
	_ = proto.Store().Put(ctx, &parchment.Artifact{
		ID:     "NOTE-1",
		Title:  "A note",
		Labels: []string{parchment.LabelPrefixKind + "knowledge.note", "status:note.fleeting"},
	})
	_ = proto.Store().AddEdge(ctx, parchment.Edge{From: "NOTE-1", To: pointerID, Relation: "traces_to"})

	result := service.PropagateChanges(ctx, proto, pointerID)
	if len(result.Invalidated) != 0 {
		t.Errorf("expected 0 invalidated for non-kernel, got %d", len(result.Invalidated))
	}
}

func TestPropagateFromChanges_OnlyProcessesUpdated(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	pointerID, kernelIDs := setupPointerWithKernels(t, proto, 1)
	_ = service.ConfirmKernel(ctx, proto, kernelIDs[0])

	remat := &service.RematerializeResult{
		Changes: []service.ChangeRecord{
			{ID: pointerID, Change: service.ChangeUpdated},
			{ID: "OTHER-1", Change: service.ChangeCreated},
		},
	}

	results := service.PropagateFromChanges(ctx, proto, remat)
	if len(results) != 1 {
		t.Fatalf("expected 1 propagation result, got %d", len(results))
	}
	if results[0].PointerID != pointerID {
		t.Errorf("expected pointer %s, got %s", pointerID, results[0].PointerID)
	}
	if len(results[0].Invalidated) != 1 {
		t.Errorf("expected 1 invalidated kernel, got %d", len(results[0].Invalidated))
	}
}
