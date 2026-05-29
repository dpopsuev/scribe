package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleKnowledgeCatalog returns the full Container List of all knowledge
// artifacts — the Finding Aid inventory. Equivalent to Lex's inspect action,
// a library catalog, or Karpathy's index.md.
//
// Format per entry:
//
//	ID  [kind|status]  labels  N edges
//	"one-line summary from goal or title"
//
// Sorted: evergreen > active > fleeting. Grouped by kind.
func (h *handler) handleKnowledgeCatalog(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) { //nolint:funlen // catalog report is inherently multi-section
	var b strings.Builder
	total := 0

	// Kind groups in display order.
	groups := []struct {
		kind    string
		heading string
	}{
		{parchment.KindConcept, "Concepts"},
		{parchment.KindNote, "Notes"},
		{parchment.KindSource, "Sources"},
		{parchment.KindJournal, "Journals"},
		{parchment.KindContext, "Context"},
	}

	for _, g := range groups {
		arts, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: g.kind, Scope: in.Scope})
		if len(arts) == 0 {
			continue
		}

		// Sort: evergreen first, then active, then fleeting, then rest.
		statusOrder := map[string]int{
			parchment.StatusEvergreen: 0,
			parchment.StatusActive:    1,
			parchment.StatusFleeting:  2,
		}
		for i := 1; i < len(arts); i++ {
			for j := i; j > 0; j-- {
				ai := statusOrder[arts[j].Status]
				aj := statusOrder[arts[j-1].Status]
				if ai == 0 && aj == 0 {
					ai = 99
					aj = 99
				}
				if ai < aj {
					arts[j], arts[j-1] = arts[j-1], arts[j]
				}
			}
		}

		fmt.Fprintf(&b, "## %s (%d)\n\n", g.heading, len(arts))
		for _, art := range arts {
			// Edge count.
			edges, _ := h.proto.GetArtifactEdges(ctx, art.ID)

			// Labels.
			labelStr := ""
			if len(art.Labels) > 0 {
				labelStr = "  [" + strings.Join(art.Labels, ", ") + "]"
			}

			// Summary: goal field, or truncated title.
			summary := art.Goal
			if summary == "" {
				// Fall back to first non-empty section body.
				for _, sec := range art.Sections {
					if sec.Text != "" {
						summary = sec.Text
						if len(summary) > 120 {
							summary = summary[:117] + "..."
						}
						break
					}
				}
			}

			fmt.Fprintf(&b, "  %-22s [%s|%s]%s  %d edges\n",
				art.ID, art.Kind, art.Status, labelStr, len(edges))
			if summary != "" {
				fmt.Fprintf(&b, "  %s\n", summary)
			}
			b.WriteByte('\n')
			total++
		}
	}

	if total == 0 {
		return text("Vault is empty. Start with knowledge(action=capture) or knowledge(action=ingest)."), nil, nil
	}
	fmt.Fprintf(&b, "Total: %d artifact(s)", total)
	return text(b.String()), nil, nil
}

// handleKnowledgeLint runs all knowledge health checks and returns a
// structured report. Karpathy's third operation: periodically lint the wiki.
//
// Checks:
//  1. Stuck-fleeting notes (> stale_days, reuses detectKnowledge)
//  2. Uncited sources (reuses detectKnowledge)
//  3. Unresolved [[wikilinks]] — titles with no matching artifact
//  4. Orphaned notes — zero incoming AND outgoing knowledge edges
//  5. Cluster synthesis gaps — 3+ notes sharing a source, no synthesis
func (h *handler) handleKnowledgeLint(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) {
	var b strings.Builder
	total := 0

	// --- Check 1+2: stuck-fleeting + uncited sources (reuse existing) ---
	basic := h.detectKnowledge(ctx, detectInput{Scope: in.Scope})
	if !strings.Contains(basic, "0 knowledge issue") {
		fmt.Fprintf(&b, "## Health (fleeting + uncited)\n\n%s\n\n", strings.TrimSpace(basic))
	}

	// --- Check 3: unresolved [[wikilinks]] ---
	unresolved := h.lintUnresolvedWikilinks(ctx, in.Scope)
	if len(unresolved) > 0 {
		total += len(unresolved)
		fmt.Fprintf(&b, "## Unresolved [[wikilinks]] (%d)\n\n", len(unresolved))
		for _, entry := range unresolved {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}

	// --- Check 4: orphaned notes ---
	orphan := h.lintOrphanedNotes(ctx, in.Scope)
	if len(orphan) > 0 {
		total += len(orphan)
		fmt.Fprintf(&b, "## Orphaned notes (%d)\n\n", len(orphan))
		for _, entry := range orphan {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}

	// --- Check 5: cluster synthesis gaps ---
	gaps := h.lintClusterGaps(ctx, in.Scope)
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

// lintUnresolvedWikilinks reports [[Title]] references in knowledge note sections
// that match no artifact by exact title. Exact matching is intentional: FTS
// fallback would hide broken links rather than surface them as gaps.
func (h *handler) lintUnresolvedWikilinks(ctx context.Context, scope string) []string {
	// Build a title index for fast exact lookup.
	all, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Scope: scope})
	titleIndex := make(map[string]bool, len(all))
	for _, a := range all {
		titleIndex[strings.ToLower(a.Title)] = true
	}

	kinds := []string{parchment.KindNote, parchment.KindJournal, parchment.KindConcept}
	var issues []string
	for _, kind := range kinds {
		arts, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: kind, Scope: scope})
		for _, art := range arts {
			body := ""
			for _, sec := range art.Sections {
				body += sec.Text + "\n"
			}
			for _, title := range parchment.UniqueWikilinks(body) {
				if !titleIndex[strings.ToLower(title)] {
					issues = append(issues, fmt.Sprintf("%s — [[%s]] has no matching artifact",
						art.ID, title))
				}
			}
		}
	}
	return issues
}

// lintOrphanedNotes finds knowledge notes with zero incoming AND outgoing
// edges of any knowledge relation (cites, elaborates, synthesizes, contradicts, remembers).
// Journals are excluded — they are inherently standalone.
func (h *handler) lintOrphanedNotes(ctx context.Context, scope string) []string {
	knowledgeRels := map[string]bool{
		parchment.RelCites: true, parchment.RelElaborates: true,
		parchment.RelSynthesises: true, parchment.RelContradicts: true,
		parchment.RelRemembers: true, parchment.RelDocuments: true,
	}
	kinds := []string{parchment.KindNote, parchment.KindConcept, parchment.KindSource}
	var issues []string
	for _, kind := range kinds {
		arts, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: kind, Scope: scope})
		for _, art := range arts {
			edges, _ := h.proto.GetArtifactEdges(ctx, art.ID)
			connected := false
			for _, e := range edges {
				if knowledgeRels[e.Relation] {
					connected = true
					break
				}
			}
			if !connected {
				issues = append(issues, fmt.Sprintf("%s [%s|%s] %s — no knowledge edges",
					art.ID, art.Kind, art.Status, art.Title))
			}
		}
	}
	return issues
}

// lintClusterGaps finds groups of 3+ notes that share a common source
// (via cites edges) but have no synthesizes artifact connecting them.
func (h *handler) lintClusterGaps(ctx context.Context, scope string) []string {
	// Build source → citing notes map.
	sources, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: scope})
	var issues []string
	for _, src := range sources {
		backlinks, _ := h.proto.Backlinks(ctx, src.ID, parchment.RelCites)
		if len(backlinks) < 3 {
			continue
		}
		// Check if any of these notes has a synthesizes edge.
		hasSynthesis := false
		for _, note := range backlinks {
			edges, _ := h.proto.GetArtifactEdges(ctx, note.ID)
			for _, e := range edges {
				if e.Relation == parchment.RelSynthesises {
					hasSynthesis = true
					break
				}
			}
			if hasSynthesis {
				break
			}
		}
		if !hasSynthesis {
			ids := make([]string, 0, len(backlinks))
			for _, n := range backlinks {
				ids = append(ids, n.ID)
			}
			issues = append(issues, fmt.Sprintf("%d notes cite %s (%s) with no synthesis: %s",
				len(backlinks), src.ID, src.Title, strings.Join(ids, ", ")))
		}
	}
	return issues
}

// handleKnowledgeOrient generates the vault map legend — the index.md equivalent.
// One call gives an agent everything needed to navigate the vault:
// schema legend, vault state, hub nodes, recent activity, health snapshot.
func (h *handler) handleKnowledgeOrient(ctx context.Context, in knowledgeInput) (*sdkmcp.CallToolResult, any, error) { //nolint:funlen,gocyclo // orient report is inherently multi-section
	var b strings.Builder
	scope := in.Scope

	// --- Schema Legend ---
	fmt.Fprintf(&b, "## Schema Legend\n\n")
	kinds := []struct{ kind, status, meaning string }{
		{parchment.KindNote, parchment.StatusFleeting + "\u2192evergreen", "core knowledge unit"},
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

	// --- Vault State ---
	fmt.Fprintf(&b, "\n## Vault State\n\n")
	knowledgeKinds := []string{
		parchment.KindNote, parchment.KindJournal,
		parchment.KindSource, parchment.KindConcept, parchment.KindContext,
	}
	totalByKind := make(map[string]int)
	fleetingCount, evergreenCount := 0, 0
	var allKnowledge []*parchment.Artifact
	for _, kind := range knowledgeKinds {
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
	for _, kind := range knowledgeKinds {
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

	// --- Hub Nodes (top 5 by edge count) ---
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
	// Sort descending by edge count.
	for i := 1; i < len(hubs); i++ {
		for j := i; j > 0 && hubs[j].edges > hubs[j-1].edges; j-- {
			hubs[j], hubs[j-1] = hubs[j-1], hubs[j]
		}
	}
	if len(hubs) > 0 {
		fmt.Fprintf(&b, "\n## Hub Nodes\n\n")
		topN := min(5, len(hubs))
		for _, h := range hubs[:topN] {
			fmt.Fprintf(&b, "  %-20s %2d edges  %s\n", h.art.ID, h.edges, h.art.Title)
		}
	}

	// --- Recent Activity (last 7 days) ---
	recent := time.Now().AddDate(0, 0, -7).Format(time.RFC3339)
	var recentArts []*parchment.Artifact
	for _, kind := range knowledgeKinds {
		arts, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{
			Kind: kind, Scope: scope, CreatedAfter: recent,
		})
		recentArts = append(recentArts, arts...)
	}
	if len(recentArts) > 0 {
		fmt.Fprintf(&b, "\n## Recent (7 days)\n\n")
		topN := min(5, len(recentArts))
		for _, art := range recentArts[:topN] {
			fmt.Fprintf(&b, "  %-20s [%s|%s] %s\n", art.ID, art.Kind, art.Status, art.Title)
		}
	}

	// --- Health Snapshot ---
	healthPart := h.detectKnowledge(ctx, detectInput{Scope: scope})
	fmt.Fprintf(&b, "\n## Health\n\n  %s\n", strings.TrimSpace(healthPart))

	// --- Recent Sessions ---
	if sessionLines := h.orientSessionLines(ctx, scope, 3); len(sessionLines) > 0 {
		b.WriteString("\n## Recent Sessions\n\n")
		for _, l := range sessionLines {
			b.WriteString(l + "\n")
		}
	}

	// --- Operating Instructions ---
	b.WriteString("\n## You are the compiler\n\n")
	b.WriteString("  ingest(source) → read it, extract concepts, create notes, link via cites/elaborates\n")
	b.WriteString("  synthesize(query) → compile related notes into a new synthesis note\n")
	b.WriteString("  promote(id) → elevate a fleeting note to evergreen when it has landed\n")
	b.WriteString("  lint (detect check=knowledge) → periodically health-check the wiki\n")
	b.WriteString("  File synthesis answers back as notes — don't let them disappear into chat\n")

	// Tier 2→3 navigation hint — discovery layer.
	b.WriteString("\n→ artifact(action=search, query=) for keyword lookup; artifact(action=recall, query=, top=10) for semantic; artifact(action=get, id=) to read a specific artifact\n")

	return text(b.String()), nil, nil
}
