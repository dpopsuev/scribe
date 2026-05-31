package mcp

// correlate.go — thin dispatch for admin(action=correlate).
// Business logic lives in service/correlate.go.

import (
	"context"

	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleCorrelate(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: consistent with all other admin handlers
	if in.Evidence == "" {
		return text("evidence is required for correlate"), nil, nil
	}
	result, err := h.svc.Correlate(ctx, in.Evidence, in.Scope)
	if err != nil {
		return text(err.Error()), nil, nil
	}
	return text(service.RenderCorrelateReport(result)), nil, nil
}
