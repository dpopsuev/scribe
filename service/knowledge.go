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
		Kind: parchment.KindNote, Status: parchment.StatusFleeting,
		Scope: in.Scope, CreatedBefore: threshold,
	})
	for _, art := range fleetingNotes {
		issues = append(issues, fmt.Sprintf("%-20s %-8s [fleeting >%dd] %s",
			art.ID, art.Kind, staleDays, art.Title))
	}

	sources, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: in.Scope})
	for _, art := range sources {
		backlinks, _ := s.Proto.Backlinks(ctx, art.ID, parchment.RelCites)
		if len(backlinks) == 0 {
			issues = append(issues, fmt.Sprintf("%-20s %-8s [no cites] %s",
				art.ID, art.Kind, art.Title))
		}
	}

	contexts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindContext, Scope: in.Scope})
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
				art.ID, art.Kind, art.Title))
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
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: kind, Scope: scope})
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
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: kind, Scope: scope})
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
					art.ID, art.Kind, art.Status, art.Title))
			}
		}
	}
	return issues
}

// LintClusterGaps finds source clusters with 3+ citing notes but no synthesis.
func (s *Service) LintClusterGaps(ctx context.Context, scope string) []string {
	sources, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: scope})
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
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: g.kind, Scope: scope})
		if len(arts) == 0 {
			continue
		}
		for i := 1; i < len(arts); i++ {
			for j := i; j > 0; j-- {
				ai, aj := statusOrder[arts[j].Status], statusOrder[arts[j-1].Status]
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
				art.ID, art.Kind, art.Status, labelStr, len(edges))
			if summary != "" {
				fmt.Fprintf(&b, "  %s\n", summary)
			}
			b.WriteByte('\n')
			total++
		}
	}
	return &KnowledgeCatalogResult{Text: b.String(), Total: total}, nil
}
