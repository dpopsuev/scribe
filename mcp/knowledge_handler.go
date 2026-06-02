package mcp

// knowledge_handler.go — thin dispatch for knowledge tool actions.
// Business logic lives in service/knowledge.go and service/memory.go.

import (
	"context"

	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleKnowledgeLint(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderKnowledgeLint(ctx, in.Scope)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

// detectKnowledge delegates to service.DetectKnowledge. Used by handleDetect and handleKnowledgeLint.
func (h *handler) detectKnowledge(ctx context.Context, in detectInput) string {
	return h.svc.DetectKnowledge(ctx, service.DetectKnowledgeInput{
		Scope:     in.Scope,
		StaleDays: in.StaleDays,
	})
}
