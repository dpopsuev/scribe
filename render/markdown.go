package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dpopsuev/scribe/model"
)

// Markdown renders an artifact as a human-readable markdown document.
func Markdown(art *model.Artifact) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# %s: %s\n\n", art.ID, art.Title)

	writeField(&b, "Kind", art.Kind)
	writeField(&b, "Status", art.Status)
	if art.Scope != "" {
		writeField(&b, "Scope", art.Scope)
	}
	if art.Parent != "" {
		writeField(&b, "Parent", art.Parent)
	}
	if art.Priority != "" {
		writeField(&b, "Priority", art.Priority)
	}
	if art.Sprint != "" {
		writeField(&b, "Sprint", art.Sprint)
	}
	if len(art.DependsOn) > 0 {
		writeField(&b, "Depends On", strings.Join(art.DependsOn, ", "))
	}
	if len(art.Labels) > 0 {
		writeField(&b, "Labels", strings.Join(art.Labels, ", "))
	}
	if len(art.Links) > 0 {
		for rel, ids := range art.Links {
			writeField(&b, strings.Title(rel), strings.Join(ids, ", "))
		}
	}
	if len(art.Extra) > 0 {
		keys := sortedKeys(art.Extra)
		for _, k := range keys {
			writeField(&b, k, fmt.Sprint(art.Extra[k]))
		}
	}
	b.WriteByte('\n')

	if art.Goal != "" {
		fmt.Fprintf(&b, "## Goal\n\n%s\n\n", art.Goal)
	}

	for _, s := range art.Sections {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", s.Name, s.Text)
	}

	if len(art.Features) > 0 {
		b.WriteString("## Features\n\n")
		for _, f := range art.Features {
			fmt.Fprintf(&b, "### %s\n\n", f.Name)
			for _, sc := range f.Scenarios {
				status := ""
				if sc.Status != "" {
					status = " (" + sc.Status + ")"
				}
				fmt.Fprintf(&b, "**Scenario: %s%s**\n\n", sc.Name, status)
				for _, step := range sc.Steps {
					fmt.Fprintf(&b, "- **%s** %s\n", step.Keyword, step.Text)
				}
				b.WriteByte('\n')
			}
		}
	}

	if len(art.Criteria) > 0 {
		b.WriteString("## Acceptance Criteria\n\n")
		for _, c := range art.Criteria {
			vb := ""
			if c.VerifiedBy != "" {
				vb = fmt.Sprintf(" (verified by: %s)", c.VerifiedBy)
			}
			fmt.Fprintf(&b, "- **[%s]** %s%s\n", c.ID, c.Description, vb)
		}
		b.WriteByte('\n')
	}

	return b.String()
}

// Table renders a list of artifacts as an aligned text table.
// Columns auto-expand: sprint, parent, and depends_on only appear when at least one row has a value.
func Table(arts []*model.Artifact) string {
	if len(arts) == 0 {
		return "No artifacts found.\n"
	}

	hasSprint := false
	hasParent := false
	hasDeps := false
	for _, a := range arts {
		if a.Sprint != "" {
			hasSprint = true
		}
		if a.Parent != "" {
			hasParent = true
		}
		if len(a.DependsOn) > 0 {
			hasDeps = true
		}
	}

	var b strings.Builder
	writeRow := func(id, kind, scope, status, sprint, parent, deps, title string) {
		fmt.Fprintf(&b, "%-16s %-12s %-10s %-10s", id, kind, scope, status)
		if hasSprint {
			fmt.Fprintf(&b, " %-14s", sprint)
		}
		if hasParent {
			fmt.Fprintf(&b, " %-16s", parent)
		}
		if hasDeps {
			fmt.Fprintf(&b, " %-20s", deps)
		}
		fmt.Fprintf(&b, " %s\n", title)
	}

	writeRow("ID", "KIND", "SCOPE", "STATUS", "SPRINT", "PARENT", "DEPENDS_ON", "TITLE")
	writeRow("----", "----", "-----", "------", "------", "------", "----------", "-----")
	for _, a := range arts {
		deps := strings.Join(a.DependsOn, ",")
		writeRow(a.ID, a.Kind, a.Scope, a.Status, a.Sprint, a.Parent, deps, a.Title)
	}

	fmt.Fprintf(&b, "\n(%d artifacts)\n", len(arts))
	return b.String()
}

// GroupedTable renders artifacts grouped by a field, with counts and one-line summaries.
func GroupedTable(arts []*model.Artifact, field string) string {
	if len(arts) == 0 {
		return "No artifacts found.\n"
	}

	groupOrder := groupOrderForField(field)
	groups := make(map[string][]*model.Artifact)
	for _, a := range arts {
		key := groupKey(a, field)
		groups[key] = append(groups[key], a)
	}

	var ordered []string
	if len(groupOrder) > 0 {
		seen := make(map[string]bool)
		for _, k := range groupOrder {
			if _, ok := groups[k]; ok {
				ordered = append(ordered, k)
				seen[k] = true
			}
		}
		for k := range groups {
			if !seen[k] {
				ordered = append(ordered, k)
			}
		}
	} else {
		for k := range groups {
			ordered = append(ordered, k)
		}
		sortStrings(ordered)
	}

	var b strings.Builder
	total := 0
	for _, key := range ordered {
		items := groups[key]
		total += len(items)
		label := key
		if label == "" {
			label = "(none)"
		}
		fmt.Fprintf(&b, "\n=== %s (%d) ===\n", strings.ToUpper(label), len(items))
		for _, a := range items {
			scope := ""
			if a.Scope != "" {
				scope = "[" + a.Scope + "] "
			}
			parent := ""
			if a.Parent != "" {
				parent = " (parent: " + a.Parent + ")"
			}
			sprint := ""
			if a.Sprint != "" {
				sprint = " (sprint: " + a.Sprint + ")"
			}
			fmt.Fprintf(&b, "  %-20s %-15s %s%s%s%s\n", a.ID, a.Kind, scope, a.Title, parent, sprint)
		}
	}
	fmt.Fprintf(&b, "\n(%d artifacts)\n", total)
	return b.String()
}

func groupKey(a *model.Artifact, field string) string {
	switch field {
	case "status":
		return a.Status
	case "scope":
		return a.Scope
	case "kind":
		return a.Kind
	case "sprint":
		return a.Sprint
	default:
		return a.Status
	}
}

func groupOrderForField(field string) []string {
	if field == "status" {
		return []string{"current", "active", "draft", "complete", "retired", "archived"}
	}
	return nil
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeField(b *strings.Builder, name, value string) {
	fmt.Fprintf(b, "**%s:** %s  \n", name, value)
}
