package mcp

// knowledge_handler.go — thin dispatch for knowledge tool actions.
// Business logic lives in service/knowledge.go and service/memory.go.

import (
	"context"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleKnowledgeCatalog(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	result, err := h.svc.KnowledgeCatalog(ctx, in.Scope)
	if err != nil {
		return nil, nil, err
	}
	if result.Total == 0 {
		return text("Vault is empty. Start with knowledge(action=capture) or knowledge(action=ingest)."), nil, nil
	}
	return text(result.Text + fmt.Sprintf("Total: %d artifact(s)", result.Total)), nil, nil
}

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

func (h *handler) handleKnowledgeOrient(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) { //nolint:funlen,gocyclo // orient report is inherently multi-section
	var b strings.Builder
	scope := in.Scope

	fmt.Fprintf(&b, "## Schema Legend\n\n")
	kinds := []struct{ kind, status, meaning string }{
		{parchment.KindNote, parchment.StatusFleeting + "→evergreen", "core knowledge unit"},
		{parchment.KindJournal, parchment.StatusActive, "daily dated entry"},
		{parchment.KindSource, parchment.StatusActive, "external material — ingest it, cite it"},
		{parchment.KindConcept, parchment.StatusActive, "atomic idea — elaborate on it"},
		{parchment.KindContext, parchment.StatusActive, "agent memory — remembers edges"},
	}
	for _, k := range kinds {
		fmt.Fprintf(&b, "  %-12s %-24s %s\n", k.kind, k.status, k.meaning)
	}
	b.WriteString("\nRelations:\n")
	rels := []struct{ rel, from, meaning string }{
		{parchment.RelCites, "note→source", "this note draws from this source"},
		{parchment.RelElaborates, "note→concept", "expands on an atomic idea"},
		{parchment.RelSynthesises, "note→[note…]", "synthesis of multiple notes"},
		{parchment.RelContradicts, "note↔note", "documents disagreement"},
		{parchment.RelRemembers, "context→note", "agent bookmarked this"},
	}
	for _, r := range rels {
		fmt.Fprintf(&b, "  %-14s %-18s %s\n", r.rel, r.from, r.meaning)
	}

	fmt.Fprintf(&b, "\n## Vault State\n\n")
	knowledgeKindList := []string{
		parchment.KindNote, parchment.KindJournal,
		parchment.KindSource, parchment.KindConcept, parchment.KindContext,
	}
	totalByKind := make(map[string]int)
	fleetingCount, evergreenCount := 0, 0
	var allKnowledge []*parchment.Artifact
	for _, kind := range knowledgeKindList {
		arts, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: kind, Scope: scope})
		totalByKind[kind] = len(arts)
		allKnowledge = append(allKnowledge, arts...)
		for _, a := range arts {
			switch a.Status {
			case parchment.StatusFleeting:
				fleetingCount++
			case parchment.StatusEvergreen:
				evergreenCount++
			}
		}
	}
	for _, kind := range knowledgeKindList {
		if n := totalByKind[kind]; n > 0 {
			fmt.Fprintf(&b, "  %-12s %d\n", kind, n)
		}
	}
	if fleetingCount > 0 || evergreenCount > 0 {
		fmt.Fprintf(&b, "  fleeting: %d   evergreen: %d\n", fleetingCount, evergreenCount)
	}
	if len(allKnowledge) == 0 {
		b.WriteString("  (empty vault — start with knowledge(action=capture) or knowledge(action=ingest))\n")
	}

	type hub struct {
		art   *parchment.Artifact
		edges int
	}
	var hubs []hub
	for _, art := range allKnowledge {
		edges, _ := h.proto.GetArtifactEdges(ctx, art.ID)
		if len(edges) > 0 {
			hubs = append(hubs, hub{art, len(edges)})
		}
	}
	for i := 1; i < len(hubs); i++ {
		for j := i; j > 0 && hubs[j].edges > hubs[j-1].edges; j-- {
			hubs[j], hubs[j-1] = hubs[j-1], hubs[j]
		}
	}
	if len(hubs) > 0 {
		fmt.Fprintf(&b, "\n## Hub Nodes\n\n")
		topN := min(5, len(hubs))
		for _, hub := range hubs[:topN] {
			fmt.Fprintf(&b, "  %-20s %2d edges  %s\n", hub.art.ID, hub.edges, hub.art.Title)
		}
	}

	recent := time.Now().AddDate(0, 0, -7).Format(time.RFC3339)
	var recentArts []*parchment.Artifact
	for _, kind := range knowledgeKindList {
		arts, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: kind, Scope: scope, CreatedAfter: recent})
		recentArts = append(recentArts, arts...)
	}
	if len(recentArts) > 0 {
		fmt.Fprintf(&b, "\n## Recent (7 days)\n\n")
		topN := min(5, len(recentArts))
		for _, art := range recentArts[:topN] {
			fmt.Fprintf(&b, "  %-20s [%s|%s] %s\n", art.ID, art.Kind, art.Status, art.Title)
		}
	}

	healthPart := h.detectKnowledge(ctx, detectInput{Scope: scope})
	fmt.Fprintf(&b, "\n## Health\n\n  %s\n", strings.TrimSpace(healthPart))

	if sessionLines := h.svc.OrientSessionLines(ctx, scope, 3); len(sessionLines) > 0 {
		b.WriteString("\n## Recent Sessions\n\n")
		for _, l := range sessionLines {
			b.WriteString(l + "\n")
		}
	}

	b.WriteString("\n## You are the compiler\n\n")
	b.WriteString("  ingest(source) → read it, extract concepts, create notes, link via cites/elaborates\n")
	b.WriteString("  synthesize(query) → compile related notes into a new synthesis note\n")
	b.WriteString("  promote(id) → elevate a fleeting note to evergreen when it has landed\n")
	b.WriteString("  lint (detect check=knowledge) → periodically health-check the wiki\n")
	b.WriteString("  File synthesis answers back as notes — don't let them disappear into chat\n")
	b.WriteString("\n→ artifact(action=search, query=) for keyword lookup; artifact(action=recall, query=, top=10) for semantic; artifact(action=get, id=) to read a specific artifact\n")

	return text(b.String()), nil, nil
}

// detectKnowledge delegates to service.DetectKnowledge. Used by handleDetect and handleKnowledgeLint.
func (h *handler) detectKnowledge(ctx context.Context, in detectInput) string {
	return h.svc.DetectKnowledge(ctx, service.DetectKnowledgeInput{
		Scope:     in.Scope,
		StaleDays: in.StaleDays,
	})
}
