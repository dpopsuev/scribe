package mcp

// ingest_session.go — thin dispatch for knowledge(action=ingest_session).
// Business logic lives in service/ingest_session.go.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleIngestSession(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	if in.Path == "" {
		return text("path is required for ingest_session — pass a .jsonl file or a directory"), nil, nil
	}

	result, err := h.svc.IngestSession(ctx, in.Path, in.Scope)
	if err != nil {
		return text(fmt.Sprintf("ingest_session: %v", err)), nil, nil
	}

	var b strings.Builder
	for _, p := range result.Paths {
		if result.Created > 0 {
			fmt.Fprintf(&b, "  %s → artifacts created\n", filepath.Base(p))
		} else {
			fmt.Fprintf(&b, "  %s → already indexed (skipped)\n", filepath.Base(p))
		}
	}
	for _, e := range result.Errors {
		fmt.Fprintf(&b, "  error: %s\n", e)
	}

	fmt.Fprintf(&b, "\nIngested %d session(s): %d artifact(s) created, %d skipped (already indexed).\n",
		len(result.Paths), result.Created, result.Skipped)
	if result.Created > 0 {
		fmt.Fprintf(&b, "\nNext — you are the compiler:\n")
		fmt.Fprintf(&b, "  knowledge(action=catalog, scope=%s) — browse what was extracted\n", result.Scope)
		fmt.Fprintf(&b, "  knowledge(action=synthesize, query=<topic>) — connect related notes\n")
	}
	return text(b.String()), nil, nil
}
