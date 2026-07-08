package service_test

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func newProto(t *testing.T) *parchment.Protocol {
	t.Helper()
	store := parchment.NewMemoryStore()
	return parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
}

func TestCreateKernel_Basic(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	err := service.CreateKernel(ctx, proto, "KRN-1", "Extracted insight", "SRC-1", "test",
		service.Selector{Section: "body", Line: 5}, "The PTP offset threshold is 100ns.")
	if err != nil {
		t.Fatalf("CreateKernel: %v", err)
	}

	art, err := proto.Store().Get(ctx, "KRN-1")
	if err != nil {
		t.Fatalf("Get kernel: %v", err)
	}
	if art.Title != "Extracted insight" {
		t.Errorf("title = %q, want %q", art.Title, "Extracted insight")
	}
	if kind := art.Label(parchment.LabelPrefixKind); kind != "knowledge.kernel" {
		t.Errorf("kind = %q, want knowledge.kernel", kind)
	}
	status := parchment.StatusFromLabels(art.Labels)
	if status != service.StatusKernelPending {
		t.Errorf("status = %q, want %q", status, service.StatusKernelPending)
	}
	if art.Extra["content_hash"] == nil {
		t.Error("expected content_hash in Extra")
	}
	if art.Extra["materialized_at"] == nil {
		t.Error("expected materialized_at in Extra")
	}
}

func TestCreateKernel_WithSelector(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	sel := service.Selector{Section: "context", Line: 42, Anchor: "details"}
	err := service.CreateKernel(ctx, proto, "KRN-2", "Detail kernel", "SRC-2", "test", sel, "Detail content.")
	if err != nil {
		t.Fatalf("CreateKernel: %v", err)
	}

	art, err := proto.Store().Get(ctx, "KRN-2")
	if err != nil {
		t.Fatalf("Get kernel: %v", err)
	}

	got := service.GetSelector(art)
	if got.Section != "context" || got.Line != 42 || got.Anchor != "details" {
		t.Errorf("selector = %+v, want section=context line=42 anchor=details", got)
	}

	edgeSels := service.GetEdgeSelectors(art)
	if len(edgeSels) != 1 {
		t.Fatalf("expected 1 edge selector, got %d", len(edgeSels))
	}
	if edgeSels[0].TargetID != "SRC-2" {
		t.Errorf("edge selector target = %q, want SRC-2", edgeSels[0].TargetID)
	}
}

func TestCreateKernel_TracesToEdge(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	err := service.CreateKernel(ctx, proto, "KRN-3", "Linked kernel", "SRC-3", "test",
		service.Selector{}, "Some content.")
	if err != nil {
		t.Fatalf("CreateKernel: %v", err)
	}

	edges, err := proto.Store().Neighbors(ctx, "KRN-3", "traces_to", parchment.Outgoing)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(edges) != 1 || edges[0].To != "SRC-3" {
		t.Errorf("expected traces_to edge to SRC-3, got %v", edges)
	}
}

func TestConfirmKernel(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	if err := service.CreateKernel(ctx, proto, "KRN-4", "To confirm", "", "test",
		service.Selector{}, "Content."); err != nil {
		t.Fatalf("CreateKernel: %v", err)
	}

	if err := service.ConfirmKernel(ctx, proto, "KRN-4"); err != nil {
		t.Fatalf("ConfirmKernel: %v", err)
	}

	art, _ := proto.Store().Get(ctx, "KRN-4")
	status := parchment.StatusFromLabels(art.Labels)
	if status != service.StatusKernelConfirmed {
		t.Errorf("status = %q, want %q", status, service.StatusKernelConfirmed)
	}
}

func TestRejectKernel(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	if err := service.CreateKernel(ctx, proto, "KRN-5", "To reject", "", "test",
		service.Selector{}, "Content."); err != nil {
		t.Fatalf("CreateKernel: %v", err)
	}

	if err := service.RejectKernel(ctx, proto, "KRN-5"); err != nil {
		t.Fatalf("RejectKernel: %v", err)
	}

	art, _ := proto.Store().Get(ctx, "KRN-5")
	status := parchment.StatusFromLabels(art.Labels)
	if status != service.StatusKernelRejected {
		t.Errorf("status = %q, want %q", status, service.StatusKernelRejected)
	}
}

func TestKernel_ConfirmThenBackToPending(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	_ = service.CreateKernel(ctx, proto, "KRN-6", "Roundtrip", "", "test", service.Selector{}, "C.")
	_ = service.ConfirmKernel(ctx, proto, "KRN-6")

	// confirmed → pending is a valid transition (re-review after source change)
	results, err := proto.SetField(ctx, []string{"KRN-6"}, "status", service.StatusKernelPending, parchment.SetFieldOptions{Force: true})
	if err != nil {
		t.Fatalf("SetField: %v", err)
	}
	if len(results) == 0 || !results[0].OK {
		t.Fatalf("expected successful transition, got %+v", results)
	}

	art, _ := proto.Store().Get(ctx, "KRN-6")
	if parchment.StatusFromLabels(art.Labels) != service.StatusKernelPending {
		t.Error("expected kernel.pending after roundtrip")
	}
}

func TestCreateKernel_NoPointer(t *testing.T) {
	proto := newProto(t)
	ctx := context.Background()

	err := service.CreateKernel(ctx, proto, "KRN-7", "Standalone", "", "test",
		service.Selector{}, "Standalone content.")
	if err != nil {
		t.Fatalf("CreateKernel: %v", err)
	}

	edges, _ := proto.Store().Neighbors(ctx, "KRN-7", "traces_to", parchment.Outgoing)
	if len(edges) != 0 {
		t.Errorf("expected no traces_to edges for standalone kernel, got %d", len(edges))
	}
}
