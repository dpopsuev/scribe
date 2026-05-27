package mcp

// memory.go — agent memory helpers for motd and orient.
//
// motdMemoryLines returns the top-N evergreen knowledge artifacts for a scope,
// formatted as brief bullet lines for injection into motd output.
//
// orientSessionLines returns recent session source artifacts created by
// ingest_session, showing the agent what was worked on in past sessions.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// motdMemoryLines returns at most n formatted lines of the most relevant
// evergreen knowledge for the given scope.
func (h *handler) motdMemoryLines(ctx context.Context, scope string, n int) []string {
	// Collect evergreen notes and concepts — the most authoritative memory.
	var knowledge []*parchment.Artifact
	for _, kind := range []string{parchment.KindNote, parchment.KindConcept} {
		arts, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{
			Kind:   kind,
			Status: parchment.StatusEvergreen,
			Scope:  scope,
		})
		knowledge = append(knowledge, arts...)
	}
	if len(knowledge) == 0 {
		return nil
	}

	// Sort by recency — most recently updated first.
	sort.Slice(knowledge, func(i, j int) bool {
		return knowledge[i].UpdatedAt.After(knowledge[j].UpdatedAt)
	})

	if len(knowledge) > n {
		knowledge = knowledge[:n]
	}

	lines := make([]string, 0, len(knowledge))
	for _, a := range knowledge {
		age := ""
		if !a.UpdatedAt.IsZero() {
			days := int(time.Since(a.UpdatedAt).Hours() / 24)
			switch days {
			case 0:
				age = "today"
			case 1:
				age = "yesterday"
			default:
				age = fmt.Sprintf("%dd ago", days)
			}
		}
		line := fmt.Sprintf("  [%s] %s", a.Kind, a.Title)
		if age != "" {
			line += "  (" + age + ")"
		}
		lines = append(lines, line)
	}
	return lines
}

// orientSessionLines returns formatted lines describing recently ingested
// sessions (source artifacts with a provenance section pointing to a .jsonl file).
func (h *handler) orientSessionLines(ctx context.Context, scope string, n int) []string {
	sources, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{
		Kind:  parchment.KindSource,
		Scope: scope,
	})

	// Filter to session sources: have a provenance section with a .jsonl path.
	type sessionSource struct {
		art        *parchment.Artifact
		provenance string
		summary    string
	}
	var sessions []sessionSource
	for _, s := range sources {
		var prov, sum string
		for _, sec := range s.Sections {
			switch sec.Name {
			case "provenance":
				if strings.HasSuffix(sec.Text, ".jsonl") {
					prov = sec.Text
				}
			case "summary":
				sum = sec.Text
			}
		}
		if prov != "" {
			sessions = append(sessions, sessionSource{s, prov, sum})
		}
	}
	if len(sessions) == 0 {
		return nil
	}

	// Most recent first.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].art.UpdatedAt.After(sessions[j].art.UpdatedAt)
	})
	if len(sessions) > n {
		sessions = sessions[:n]
	}

	lines := make([]string, 0, len(sessions))
	for _, s := range sessions {
		// Extract just the filename from the path.
		parts := strings.Split(s.provenance, "/")
		filename := parts[len(parts)-1]
		// Trim the timestamp prefix for readability: 2026-05-27T10-00-00_abc.jsonl → abc
		if idx := strings.Index(filename, "_"); idx >= 0 {
			filename = filename[idx+1:]
		}
		filename = strings.TrimSuffix(filename, ".jsonl")

		age := ""
		if !s.art.UpdatedAt.IsZero() {
			days := int(time.Since(s.art.UpdatedAt).Hours() / 24)
			switch days {
			case 0:
				age = "today"
			case 1:
				age = "yesterday"
			default:
				age = fmt.Sprintf("%dd ago", days)
			}
		}

		line := fmt.Sprintf("  %s", filename)
		if age != "" {
			line += "  (" + age + ")"
		}

		// Include the first meaningful line of summary.
		if s.summary != "" {
			for _, l := range strings.Split(s.summary, "\n") {
				l = strings.TrimSpace(l)
				if l != "" && !strings.HasPrefix(l, "Format:") {
					line += "\n    " + l
					break
				}
			}
		}
		lines = append(lines, line)
	}
	return lines
}
