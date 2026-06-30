package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

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
			b := make([]byte, 4) //nolint:mnd // 4 bytes = 8 hex chars for kernel ID suffix
			_, _ = rand.Read(b)
			id = "KRN-" + hex.EncodeToString(b)
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
