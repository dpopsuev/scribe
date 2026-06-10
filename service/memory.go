package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// BriefMemoryLines returns at most n formatted lines of the most relevant
// evergreen knowledge for the given scope.
func (s *Service) BriefMemoryLines(ctx context.Context, scope string, n int) []string {
	var knowledge []*parchment.Artifact
	for _, kind := range []string{parchment.KindNote, parchment.KindConcept} {
		memLabels := []string{parchment.LabelPrefixKind + kind, "note.evergreen"}
		if scope != "" {
			memLabels = append(memLabels, parchment.LabelPrefixScope+scope)
		}
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: memLabels})
		knowledge = append(knowledge, arts...)
	}
	if len(knowledge) == 0 {
		return nil
	}
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
		line := fmt.Sprintf("  [%s] %s", a.Label(parchment.LabelPrefixKind), a.Title)
		if age != "" {
			line += "  (" + age + ")"
		}
		lines = append(lines, line)
	}
	return lines
}

// OrientSessionLines returns formatted lines describing recently ingested sessions.
func (s *Service) OrientSessionLines(ctx context.Context, scope string, n int) []string {
	sessionLabels := []string{parchment.LabelPrefixKind + parchment.KindSource}
	if scope != "" {
		sessionLabels = append(sessionLabels, parchment.LabelPrefixScope+scope)
	}
	sources, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: sessionLabels})
	type sessionSource struct {
		art        *parchment.Artifact
		provenance string
		summary    string
	}
	var sessions []sessionSource
	for _, src := range sources {
		var prov, sum string
		for _, sec := range src.Sections {
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
			sessions = append(sessions, sessionSource{src, prov, sum})
		}
	}
	if len(sessions) == 0 {
		return nil
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].art.UpdatedAt.After(sessions[j].art.UpdatedAt)
	})
	if len(sessions) > n {
		sessions = sessions[:n]
	}
	lines := make([]string, 0, len(sessions))
	for _, ss := range sessions {
		parts := strings.Split(ss.provenance, "/")
		filename := parts[len(parts)-1]
		if idx := strings.Index(filename, "_"); idx >= 0 {
			filename = filename[idx+1:]
		}
		filename = strings.TrimSuffix(filename, ".jsonl")
		age := ""
		if !ss.art.UpdatedAt.IsZero() {
			days := int(time.Since(ss.art.UpdatedAt).Hours() / 24)
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
		if ss.summary != "" {
			for _, l := range strings.Split(ss.summary, "\n") {
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
