package mcp

// knowledge_handler.go — thin dispatch for knowledge tool actions.
// Business logic lives in service/knowledge.go and service/memory.go.

import (
	"context"
	"fmt"
	"strings"

	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleKnowledgeLint(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	var b strings.Builder
	total := 0

	basic := h.detectKnowledge(ctx, detectInput{Scope: in.Scope})
	if !strings.Contains(basic, "0 knowledge issue") {
		fmt.Fprintf(&b, "## Health (fleeting + uncited)\n\n%s\n\n", strings.TrimSpace(basic))
	}

	unresolved := h.svc.LintUnresolvedWikilinks(ctx, in.Scope)
	if len(unresolved) > 0 {
		total += len(unresolved)
		fmt.Fprintf(&b, "## Unresolved [[wikilinks]] (%d)\n\n", len(unresolved))
		for _, entry := range unresolved {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}

	orphan := h.svc.LintOrphanedNotes(ctx, in.Scope)
	if len(orphan) > 0 {
		total += len(orphan)
		fmt.Fprintf(&b, "## Orphaned notes (%d)\n\n", len(orphan))
		for _, entry := range orphan {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}

	gaps := h.svc.LintClusterGaps(ctx, in.Scope)
	if len(gaps) > 0 {
		total += len(gaps)
		fmt.Fprintf(&b, "## Cluster synthesis gaps (%d)\n\n", len(gaps))
		for _, entry := range gaps {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}

	if b.Len() == 0 {
		return text("Lint clean — no issues found."), nil, nil
	}
	fmt.Fprintf(&b, "Total issues: %d", total)
	return text(b.String()), nil, nil
}

// detectKnowledge delegates to service.DetectKnowledge. Used by handleDetect and handleKnowledgeLint.
func (h *handler) detectKnowledge(ctx context.Context, in detectInput) string {
	return h.svc.DetectKnowledge(ctx, service.DetectKnowledgeInput{
		Scope:     in.Scope,
		StaleDays: in.StaleDays,
	})
}
