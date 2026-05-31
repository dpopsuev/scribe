package mcp

// recall.go — thin dispatch for knowledge(action=recall).
// Business logic lives in service/recall.go.

import (
	"context"
	"fmt"
	"strings"

	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleRecall(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	if in.Query == "" {
		return text("query is required for recall — describe what you want to remember"), nil, nil
	}
	results, err := h.svc.Recall(ctx, in.Query, in.Scope, 0)
	if err != nil {
		return text(err.Error()), nil, nil
	}
	if len(results) == 0 {
		return text(fmt.Sprintf("no memory found for %q — try capture or ingest to build up the vault", in.Query)), nil, nil
	}

	queryTerms := strings.Fields(strings.ToLower(in.Query))
	var b strings.Builder
	fmt.Fprintf(&b, "Recall: %q\n\n", in.Query)
	for _, r := range results {
		fmt.Fprintf(&b, "[%s|%s] %s  %s\n", r.Art.Kind, r.Art.Status, r.Art.ID, r.Art.Title)
		if excerpt := service.ExtractExcerpt(r.Art, queryTerms); excerpt != "" {
			fmt.Fprintf(&b, "  %s\n", excerpt)
		}
	}
	return text(b.String()), nil, nil
}
