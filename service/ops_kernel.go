package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

const (
	// KindKernel is the kind label for kernel artifacts.
	KindKernel = "knowledge.kernel"

	// StatusKernelPending is the initial status for newly created kernels.
	StatusKernelPending = "kernel.pending"
	// StatusKernelConfirmed marks a kernel as reviewed and accepted.
	StatusKernelConfirmed = "kernel.confirmed"
	// StatusKernelRejected marks a kernel as reviewed and rejected.
	StatusKernelRejected = "kernel.rejected"

	sectionContent = "content"
	edgeTracesTo   = "traces_to"
)

// errKernelTransition is returned when a kernel status transition fails.
var errKernelTransition = errors.New("kernel transition failed")

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
		return fmt.Errorf("%w: confirm %s: %s", errKernelTransition, id, results[0].Error)
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
		return fmt.Errorf("%w: reject %s: %s", errKernelTransition, id, results[0].Error)
	}
	return nil
}

// --- Op handlers ---

type kernelCreateInput struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title"`
	PointerID string `json:"pointer_id,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Content   string `json:"content"`
	Section   string `json:"section,omitempty"`
	Line      int    `json:"line,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
}

type kernelTransitionInput struct {
	ID string `json:"id"`
}

var opKernelCreate = Op{
	Name: "kernel_create",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in kernelCreateInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Title == "" {
			return "", fmt.Errorf("title is required") //nolint:err113 // user-facing
		}
		if in.Content == "" {
			return "", fmt.Errorf("content is required") //nolint:err113 // user-facing
		}

		id := in.ID
		if id == "" {
			id = parchment.GenerateUUID()
		}

		sel := Selector{
			Section: in.Section,
			Line:    in.Line,
			Anchor:  in.Anchor,
		}

		if err := CreateKernel(ctx, svc.Proto, id, in.Title, in.PointerID, in.Scope, sel, in.Content); err != nil {
			return "", err
		}

		return fmt.Sprintf("created kernel %s (status: %s)", id, StatusKernelPending), nil
	},
}

var opKernelConfirm = Op{
	Name: "kernel_confirm",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in kernelTransitionInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" {
			return "", fmt.Errorf("id is required") //nolint:err113 // user-facing
		}
		if err := ConfirmKernel(ctx, svc.Proto, in.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("kernel %s confirmed", in.ID), nil
	},
}

var opKernelReject = Op{
	Name: "kernel_reject",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in kernelTransitionInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" {
			return "", fmt.Errorf("id is required") //nolint:err113 // user-facing
		}
		if err := RejectKernel(ctx, svc.Proto, in.ID); err != nil {
			return "", err
		}
		return fmt.Sprintf("kernel %s rejected", in.ID), nil
	},
}
