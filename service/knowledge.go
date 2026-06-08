package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// DetectKnowledgeInput parameterises knowledge health detection.
type DetectKnowledgeInput struct {
	Scope     string
	StaleDays int
}

// DetectKnowledge surfaces knowledge health signals: stuck-fleeting notes,
// uncited sources, and context artifacts with no remembers edges.
func (s *Service) DetectKnowledge(ctx context.Context, in DetectKnowledgeInput) string {
	staleDays := in.StaleDays
	if staleDays == 0 {
		staleDays = 7
	}
	threshold := time.Now().AddDate(0, 0, -staleDays).Format(time.RFC3339)
	var issues []string

	fleetingNotes, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels:        []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixStatus + parchment.StatusFleeting},
		Scope:         in.Scope,
		CreatedBefore: threshold,
	})
	for _, art := range fleetingNotes {
		issues = append(issues, fmt.Sprintf("%-20s %-8s [fleeting >%dd] %s",
			art.ID, art.ResolvedKind(), staleDays, art.Title))
	}

	sources, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindSource}, Scope: in.Scope})
	for _, art := range sources {
		backlinks, _ := s.Proto.Backlinks(ctx, art.ID, parchment.RelCites)
		if len(backlinks) == 0 {
			issues = append(issues, fmt.Sprintf("%-20s %-8s [no cites] %s",
				art.ID, art.ResolvedKind(), art.Title))
		}
	}

	contexts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindContext}, Scope: in.Scope})
	for _, art := range contexts {
		neighbors, _ := s.Proto.GetArtifactEdges(ctx, art.ID)
		remembersCount := 0
		for _, e := range neighbors {
			if e.Relation == parchment.RelRemembers && e.Direction == "outgoing" {
				remembersCount++
			}
		}
		if remembersCount == 0 {
			issues = append(issues, fmt.Sprintf("%-20s %-8s [no remembers] %s",
				art.ID, art.ResolvedKind(), art.Title))
		}
	}

	if len(issues) == 0 {
		return fmt.Sprintf("No knowledge issues (fleeting >%dd: 0, uncited sources: 0).", staleDays)
	}
	var b strings.Builder
	for _, issue := range issues {
		fmt.Fprintln(&b, issue)
	}
	fmt.Fprintf(&b, "%d knowledge issue(s) (staleDays=%d)", len(issues), staleDays)
	return b.String()
}

// LintUnresolvedWikilinks reports [[Title]] references with no matching artifact.
func (s *Service) LintUnresolvedWikilinks(ctx context.Context, scope string) []string {
	all, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Scope: scope})
	titleIndex := make(map[string]bool, len(all))
	for _, a := range all {
		titleIndex[strings.ToLower(a.Title)] = true
	}
	kinds := []string{parchment.KindNote, parchment.KindJournal, parchment.KindConcept}
	var issues []string
	for _, kind := range kinds {
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + kind}, Scope: scope})
		for _, art := range arts {
			body := ""
			for _, sec := range art.Sections {
				body += sec.Text + "\n"
			}
			for _, title := range parchment.UniqueWikilinks(body) {
				if !titleIndex[strings.ToLower(title)] {
					issues = append(issues, fmt.Sprintf("%s — [[%s]] has no matching artifact", art.ID, title))
				}
			}
		}
	}
	return issues
}

// LintOrphanedNotes finds knowledge notes with zero knowledge-relation edges.
func (s *Service) LintOrphanedNotes(ctx context.Context, scope string) []string {
	knowledgeRels := map[string]bool{
		parchment.RelCites: true, parchment.RelElaborates: true,
		parchment.RelSynthesises: true, parchment.RelContradicts: true,
		parchment.RelRemembers: true, parchment.RelDocuments: true,
	}
	kinds := []string{parchment.KindNote, parchment.KindConcept, parchment.KindSource}
	var issues []string
	for _, kind := range kinds {
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + kind}, Scope: scope})
		for _, art := range arts {
			edges, _ := s.Proto.GetArtifactEdges(ctx, art.ID)
			connected := false
			for _, e := range edges {
				if knowledgeRels[e.Relation] {
					connected = true
					break
				}
			}
			if !connected {
				issues = append(issues, fmt.Sprintf("%s [%s|%s] %s — no knowledge edges",
					art.ID, art.ResolvedKind(), art.ResolvedStatus(), art.Title))
			}
		}
	}
	return issues
}

// LintClusterGaps finds source clusters with 3+ citing notes but no synthesis.
func (s *Service) LintClusterGaps(ctx context.Context, scope string) []string {
	sources, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindSource}, Scope: scope})
	var issues []string
	for _, src := range sources {
		backlinks, _ := s.Proto.Backlinks(ctx, src.ID, parchment.RelCites)
		if len(backlinks) < 3 {
			continue
		}
		hasSynthesis := false
		for _, note := range backlinks {
			edges, _ := s.Proto.GetArtifactEdges(ctx, note.ID)
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

// KnowledgeCatalogResult holds the catalog output.
type KnowledgeCatalogResult struct {
	Text  string
	Total int
}

// KnowledgeCatalog returns the full Container List of knowledge artifacts.
func (s *Service) KnowledgeCatalog(ctx context.Context, scope string) (*KnowledgeCatalogResult, error) { //nolint:funlen // catalog groups each kind — inherently multi-section
	var b strings.Builder
	total := 0
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
	statusOrder := map[string]int{
		parchment.StatusEvergreen: 0,
		parchment.StatusActive:    1,
		parchment.StatusFleeting:  2,
	}
	for _, g := range groups {
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + g.kind}, Scope: scope})
		if len(arts) == 0 {
			continue
		}
		for i := 1; i < len(arts); i++ {
			for j := i; j > 0; j-- {
				ai, aj := statusOrder[arts[j].ResolvedStatus()], statusOrder[arts[j-1].ResolvedStatus()]
				if ai == 0 && aj == 0 {
					ai, aj = 99, 99
				}
				if ai < aj {
					arts[j], arts[j-1] = arts[j-1], arts[j]
				}
			}
		}
		fmt.Fprintf(&b, "## %s (%d)\n\n", g.heading, len(arts))
		for _, art := range arts {
			edges, _ := s.Proto.GetArtifactEdges(ctx, art.ID)
			labelStr := ""
			if len(art.Labels) > 0 {
				labelStr = "  [" + strings.Join(art.Labels, ", ") + "]"
			}
			summary := art.Goal
			if summary == "" {
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
				art.ID, art.ResolvedKind(), art.ResolvedStatus(), labelStr, len(edges))
			if summary != "" {
				fmt.Fprintf(&b, "  %s\n", summary)
			}
			b.WriteByte('\n')
			total++
		}
	}
	return &KnowledgeCatalogResult{Text: b.String(), Total: total}, nil
}

func (s *Service) KnowledgeOrient(ctx context.Context, scope string) (string, error) { //nolint:gocyclo,cyclop,funlen // orient report is inherently multi-section
	var b strings.Builder
	kinds := []struct{ kind, status, meaning string }{
		{parchment.KindNote, parchment.StatusFleeting + "→evergreen", "core knowledge unit"},
		{parchment.KindJournal, parchment.StatusActive, "daily dated entry"},
		{parchment.KindSource, parchment.StatusActive, "external material — ingest it, cite it"},
		{parchment.KindConcept, parchment.StatusActive, "atomic idea — elaborate on it"},
		{parchment.KindContext, parchment.StatusActive, "agent memory — remembers edges"},
	}
	fmt.Fprintf(&b, "## Schema Legend\n\n")
	for _, k := range kinds {
		fmt.Fprintf(&b, "  %-12s %-24s %s\n", k.kind, k.status, k.meaning)
	}
	b.WriteString("\nRelations:\n")
	for _, r := range []struct{ rel, from, meaning string }{
		{parchment.RelCites, "note→source", "this note draws from this source"},
		{parchment.RelElaborates, "note→concept", "expands on an atomic idea"},
		{parchment.RelSynthesises, "note→[note…]", "synthesis of multiple notes"},
		{parchment.RelContradicts, "note↔note", "documents disagreement"},
		{parchment.RelRemembers, "context→note", "agent bookmarked this"},
	} {
		fmt.Fprintf(&b, "  %-14s %-18s %s\n", r.rel, r.from, r.meaning)
	}
	fmt.Fprintf(&b, "\n## Vault State\n\n")
	knowledgeKinds := []string{
		parchment.KindNote, parchment.KindJournal,
		parchment.KindSource, parchment.KindConcept, parchment.KindContext,
	}
	totalByKind := make(map[string]int)
	fleeting, evergreen := 0, 0
	var all []*parchment.Artifact
	for _, kind := range knowledgeKinds {
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + kind}, Scope: scope})
		totalByKind[kind] = len(arts)
		all = append(all, arts...)
		for _, a := range arts {
			switch a.ResolvedStatus() {
			case parchment.StatusFleeting:
				fleeting++
			case parchment.StatusEvergreen:
				evergreen++
			}
		}
	}
	for _, kind := range knowledgeKinds {
		if n := totalByKind[kind]; n > 0 {
			fmt.Fprintf(&b, "  %-12s %d\n", kind, n)
		}
	}
	if fleeting > 0 || evergreen > 0 {
		fmt.Fprintf(&b, "  fleeting: %d   evergreen: %d\n", fleeting, evergreen)
	}
	if len(all) == 0 {
		b.WriteString("  (empty vault)\n")
	}
	type hub struct {
		art   *parchment.Artifact
		edges int
	}
	var hubs []hub
	for _, art := range all {
		edges, _ := s.Proto.GetArtifactEdges(ctx, art.ID)
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
		for _, h := range hubs[:min(5, len(hubs))] {
			fmt.Fprintf(&b, "  %-20s %2d edges  %s\n", h.art.ID, h.edges, h.art.Title)
		}
	}
	healthPart := s.DetectKnowledge(ctx, DetectKnowledgeInput{Scope: scope})
	fmt.Fprintf(&b, "\n## Health\n\n  %s\n", strings.TrimSpace(healthPart))
	if sessionLines := s.OrientSessionLines(ctx, scope, 3); len(sessionLines) > 0 {
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
	return b.String(), nil
}
