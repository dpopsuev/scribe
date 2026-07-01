package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type changelogInput struct {
	ID    string `json:"id"`
	Limit int    `json:"limit,omitempty"`
}

var opChangelog = Op{
	Name: "changelog",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in changelogInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" {
			return "", fmt.Errorf("id required") //nolint:err113 // agent-facing
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}

		revs, err := svc.Proto.ListRevisions(ctx, in.ID, limit)
		if err != nil {
			return "", err
		}
		if len(revs) == 0 {
			return "no revisions found", nil
		}

		current, err := svc.Proto.GetArtifact(ctx, in.ID)
		if err != nil {
			return renderRevisionsOnly(revs), nil
		}

		return renderChangelog(current, revs), nil
	},
}

func renderRevisionsOnly(revs []parchment.Revision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d revision(s) found (artifact deleted)\n", len(revs))
	for i := range revs {
		fmt.Fprintf(&b, "  rev %d  %s  %q\n", revs[i].Rev, revs[i].UpdatedAt.Format("2006-01-02 15:04"), revs[i].Title)
	}
	return b.String()
}

func renderChangelog(current *parchment.Artifact, revs []parchment.Revision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Changelog for %s (%d revision(s))\n", current.ID, len(revs))

	latest := revToSnapshot(&revs[0])
	diffs := diffArtifacts(latest, current)
	if len(diffs) > 0 {
		fmt.Fprintf(&b, "\n### rev %d → current (%s → %s)\n",
			revs[0].Rev,
			revs[0].UpdatedAt.Format("2006-01-02 15:04"),
			current.UpdatedAt.Format("2006-01-02 15:04"))
		for _, d := range diffs {
			fmt.Fprintf(&b, "  %s\n", d)
		}
	}

	for i := 0; i < len(revs)-1; i++ {
		older := revToSnapshot(&revs[i+1])
		newer := revToSnapshot(&revs[i])
		diffs := diffSnapshots(older, newer)
		if len(diffs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n### rev %d → rev %d (%s → %s)\n",
			revs[i+1].Rev, revs[i].Rev,
			revs[i+1].UpdatedAt.Format("2006-01-02 15:04"),
			revs[i].UpdatedAt.Format("2006-01-02 15:04"))
		for _, d := range diffs {
			fmt.Fprintf(&b, "  %s\n", d)
		}
	}

	return b.String()
}

type snapshot struct {
	Title    string
	Kind     string
	Scope    string
	Status   string
	Priority string
	Sprint   string
	Goal     string
	Labels   []string
	Sections []parchment.Section
}

func revToSnapshot(r *parchment.Revision) snapshot {
	return snapshot{
		Title:    r.Title,
		Kind:     r.Kind,
		Scope:    r.Scope,
		Status:   r.Status,
		Priority: r.Priority,
		Sprint:   r.Sprint,
		Goal:     r.Goal,
		Labels:   r.Labels,
		Sections: r.Sections,
	}
}

func artToSnapshot(a *parchment.Artifact) snapshot {
	return snapshot{
		Title:    a.Title,
		Kind:     a.Label(parchment.LabelPrefixKind),
		Scope:    a.Label(parchment.LabelPrefixScope),
		Status:   parchment.StatusFromLabels(a.Labels),
		Priority: a.Label(parchment.LabelPrefixPriority),
		Sprint:   a.Label(parchment.LabelPrefixSprint),
		Goal:     a.Goal(),
		Labels:   a.Labels,
		Sections: a.Sections,
	}
}

func diffArtifacts(old snapshot, newArt *parchment.Artifact) []string {
	newer := artToSnapshot(newArt)
	return diffSnapshots(old, newer)
}

func diffSnapshots(old, newer snapshot) []string {
	var lines []string

	for _, f := range []struct{ name, va, vb string }{
		{sortFieldTitle, old.Title, newer.Title},
		{sortFieldKind, old.Kind, newer.Kind},
		{sortFieldScope, old.Scope, newer.Scope},
		{sortFieldStatus, old.Status, newer.Status},
		{sortFieldPriority, old.Priority, newer.Priority},
		{sortFieldSprint, old.Sprint, newer.Sprint},
		{"goal", old.Goal, newer.Goal},
	} {
		if f.va != f.vb {
			lines = append(lines, fmt.Sprintf("%s: %q → %q", f.name, f.va, f.vb))
		}
	}

	lines = append(lines, diffSections(old.Sections, newer.Sections)...)
	lines = append(lines, diffLabels(old.Labels, newer.Labels)...)
	return lines
}

func diffSections(oldSecs, newSecs []parchment.Section) []string {
	oldMap := make(map[string]string, len(oldSecs))
	for _, s := range oldSecs {
		oldMap[s.Name] = s.Text
	}
	newMap := make(map[string]string, len(newSecs))
	for _, s := range newSecs {
		newMap[s.Name] = s.Text
	}

	var lines []string
	for name, textOld := range oldMap {
		if textNew, ok := newMap[name]; !ok {
			lines = append(lines, fmt.Sprintf("section %q: removed", name))
		} else if textOld != textNew {
			lines = append(lines, fmt.Sprintf("section %q: modified (%d → %d bytes)", name, len(textOld), len(textNew)))
		}
	}
	for name := range newMap {
		if _, ok := oldMap[name]; !ok {
			lines = append(lines, fmt.Sprintf("section %q: added", name))
		}
	}
	sort.Strings(lines)
	return lines
}

func diffLabels(oldLabels, newLabels []string) []string {
	oldSet := make(map[string]bool, len(oldLabels))
	for _, l := range oldLabels {
		oldSet[l] = true
	}
	newSet := make(map[string]bool, len(newLabels))
	for _, l := range newLabels {
		newSet[l] = true
	}

	// Skip structural labels (kind:, status:, project:, priority:, sprint:)
	// since they're already covered by the field-level diff above.
	isStructural := func(l string) bool {
		for _, p := range []string{
			parchment.LabelPrefixKind,
			parchment.LabelPrefixScope,
			parchment.LabelPrefixPriority,
			parchment.LabelPrefixSprint,
		} {
			if strings.HasPrefix(l, p) {
				return true
			}
		}
		return parchment.IsDomainStatusLabel(l) || strings.HasPrefix(l, "status:")
	}

	var lines []string
	for l := range newSet {
		if !oldSet[l] && !isStructural(l) {
			lines = append(lines, fmt.Sprintf("label %q added", l))
		}
	}
	for l := range oldSet {
		if !newSet[l] && !isStructural(l) {
			lines = append(lines, fmt.Sprintf("label %q removed", l))
		}
	}
	sort.Strings(lines)
	return lines
}
