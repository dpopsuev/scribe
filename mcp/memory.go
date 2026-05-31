package mcp

// memory.go — thin wrappers over service memory helpers.
// Business logic lives in service/memory.go.

import "context"

func (h *handler) motdMemoryLines(ctx context.Context, scope string, n int) []string {
	return h.svc.MotdMemoryLines(ctx, scope, n)
}

func (h *handler) orientSessionLines(ctx context.Context, scope string, n int) []string {
	return h.svc.OrientSessionLines(ctx, scope, n)
}
