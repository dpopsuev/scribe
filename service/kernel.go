package service

import (
	"context"
	"errors"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

const (
	KindKernel = "knowledge.kernel"

	StatusKernelPending   = "kernel.pending"
	StatusKernelConfirmed = "kernel.confirmed"
	StatusKernelRejected  = "kernel.rejected"

	sectionContent = "content"
	edgeTracesTo   = "traces_to"
)

// ErrKernelTransition is returned when a kernel status transition fails.
var ErrKernelTransition = errors.New("kernel transition failed")

// CreateKernel creates a kernel artifact linked to its source pointer via traces_to.
// The selector anchors the kernel to a precise location in the source.
func CreateKernel(ctx context.Context, proto *parchment.Protocol, id, title, pointerID, scope string, sel Selector, content string) error {
	sections := []parchment.Section{{Name: sectionContent, Text: content}}

	labels := []string{
		parchment.LabelPrefixKind + KindKernel,
	}
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}

	art := &parchment.Artifact{
		ID:       id,
		Title:    title,
		Labels:   labels,
		Sections: sections,
	}

	if !sel.IsZero() {
		SetSelector(art, sel)
		SetEdgeSelector(art, EdgeSelector{
			TargetID: pointerID,
			Relation: edgeTracesTo,
			Selector: sel,
		})
	}

	StampContentHash(art)

	if _, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		ExplicitID: art.ID,
		Title:      art.Title,
		Labels:     art.Labels,
		Sections:   art.Sections,
		Extra:      art.Extra,
	}); err != nil {
		return fmt.Errorf("create kernel: %w", err)
	}

	if pointerID != "" {
		if err := proto.Store().AddEdge(ctx, parchment.Edge{
			From:     id,
			To:       pointerID,
			Relation: edgeTracesTo,
		}); err != nil {
			return fmt.Errorf("link kernel→pointer: %w", err)
		}
	}

	return nil
}

// ConfirmKernel transitions a kernel from pending to confirmed.
func ConfirmKernel(ctx context.Context, proto *parchment.Protocol, id string) error {
	results, err := proto.SetField(ctx, []string{id}, "status", StatusKernelConfirmed, parchment.SetFieldOptions{})
	if err != nil {
		return fmt.Errorf("confirm kernel: %w", err)
	}
	if len(results) > 0 && !results[0].OK {
		return fmt.Errorf("%w: confirm %s: %s", ErrKernelTransition, id, results[0].Error)
	}
	return nil
}

// RejectKernel transitions a kernel from pending to rejected.
func RejectKernel(ctx context.Context, proto *parchment.Protocol, id string) error {
	results, err := proto.SetField(ctx, []string{id}, "status", StatusKernelRejected, parchment.SetFieldOptions{})
	if err != nil {
		return fmt.Errorf("reject kernel: %w", err)
	}
	if len(results) > 0 && !results[0].OK {
		return fmt.Errorf("%w: reject %s: %s", ErrKernelTransition, id, results[0].Error)
	}
	return nil
}
