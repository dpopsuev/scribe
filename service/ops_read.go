package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type edgeInput struct {
	From     string `json:"from"`
	Relation string `json:"relation"`
	To       string `json:"to"`
}

type linkInput struct {
	ID          string      `json:"id"`
	Relation    string      `json:"relation"`
	Targets     []string    `json:"targets,omitempty"`
	Target      string      `json:"target,omitempty"`
	OldTarget   string      `json:"old_target,omitempty"` // edge to replace when mode=replace
	Mode        string      `json:"mode,omitempty"`
	Weight      float64     `json:"weight,omitempty"`
	Edges       []edgeInput `json:"edges,omitempty"`
}

func execEdgeOp(ctx context.Context, svc *Service, in linkInput, unlink bool) (string, error) {
	verb := "linked"
	if unlink {
		verb = "unlinked"
	}
	callLink := func(ctx context.Context, from, rel string, targets []string) ([]parchment.Result, error) {
		if unlink {
			return svc.Proto.UnlinkArtifacts(ctx, from, rel, targets)
		}
		return svc.Proto.LinkArtifacts(ctx, from, rel, targets, in.Weight)
	}
	if len(in.Edges) > 0 {
		var lines []string
		for _, e := range in.Edges {
			results, err := callLink(ctx, e.From, e.Relation, []string{e.To})
			if err != nil {
				lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, err))
				continue
			}
			for _, r := range results {
				if r.OK {
					lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, e.From, e.Relation, e.To))
				} else {
					lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, r.Error))
				}
			}
		}
		return strings.Join(lines, "\n"), nil
	}
	if in.ID == "" || len(in.Targets) == 0 || in.Relation == "" {
		return "", fmt.Errorf("id, relation, and targets required") //nolint:err113 // user-facing hint
	}
	results, err := callLink(ctx, in.ID, in.Relation, in.Targets)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, in.ID, in.Relation, r.ID))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return strings.Join(lines, "\n"), nil
}

var opLink = Op{
	Name: "link",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in linkInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Mode == "remove" || in.Mode == "unlink" {
			return execEdgeOp(ctx, svc, in, true)
		}
		if in.OldTarget != "" {
			if in.ID == "" || in.Relation == "" || in.Target == "" {
				return "", fmt.Errorf("id, relation, replace_from, and target required") //nolint:err113 // user-facing hint
			}
			if _, err := svc.Proto.UnlinkArtifacts(ctx, in.ID, in.Relation, []string{in.OldTarget}); err != nil {
				return "", fmt.Errorf("unlink old: %w", err)
			}
			if _, err := svc.Proto.LinkArtifacts(ctx, in.ID, in.Relation, []string{in.Target}, in.Weight); err != nil {
				return "", fmt.Errorf("link new: %w", err)
			}
			return fmt.Sprintf("replaced %s -[%s]-> %s with %s", in.ID, in.Relation, in.OldTarget, in.Target), nil
		}
		return execEdgeOp(ctx, svc, in, false)
	},
}

type getInput struct {
	ID            string   `json:"id"`
	IDs           []string `json:"ids,omitempty"`
	Name          string   `json:"name,omitempty"`
	Against       string   `json:"against,omitempty"`
	Format        string   `json:"format,omitempty"`
	Depth         int      `json:"depth,omitempty"`
	Relation      string   `json:"relation,omitempty"`
	Direction     string   `json:"direction,omitempty"`
	IncludeEdges  bool     `json:"include_edges,omitempty"`
	SectionFilter []string `json:"section_filter,omitempty"`
}

var opGet = Op{
	Name: "get",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in getInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Against != "" {
			return getDiff(ctx, svc, in.ID, in.Against)
		}
		if in.Name != "" {
			t, err := svc.Proto.GetSection(ctx, in.ID, in.Name)
			if err != nil {
				return "", err
			}
			return t, nil
		}
		ids := resolveIDs(in.IDs, in.ID)
		if len(ids) == 0 {
			return "", fmt.Errorf("id or ids required") //nolint:err113 // user-facing hint
		}
		switch in.Format {
		case "summary":
			return getSummary(ctx, svc, ids)
		case "briefing":
			return getBriefing(ctx, svc, in.ID, in.Depth)
		case "impact":
			return getImpact(ctx, svc, in.ID)
		case "tree":
			tree, err := svc.Proto.ArtifactTree(ctx, parchment.TreeInput{
				ID: in.ID, Relation: in.Relation, Direction: in.Direction, Depth: in.Depth,
			})
			if err != nil {
				return "", err
			}
			return renderTree(tree), nil
		}
		if len(ids) == 1 {
			art, err := svc.Proto.GetArtifact(ctx, ids[0])
			if err != nil {
				return "", err
			}
			FilterSections(art, in.SectionFilter)
			svc.RecordRead(ctx, ids[0])
			if in.IncludeEdges {
				edges, err := svc.Proto.GetArtifactEdges(ctx, ids[0])
				if err != nil {
					return "", err
				}
				score := svc.Proto.CompletionScore(ctx, art)
				type artWithEdges struct {
					*parchment.Artifact
					Edges           []parchment.EdgeSummary `json:"edges"`
					CompletionScore float64                 `json:"completion_score"`
				}
				data, _ := json.Marshal(artWithEdges{Artifact: art, Edges: edges, CompletionScore: score})
				return string(data), nil
			}
			score := svc.Proto.CompletionScore(ctx, art)
			out := parchment.RenderMarkdown(art)
			if score > 0 {
				out += fmt.Sprintf("\n\n**Completion Score:** %.0f%%", score*100)
			}
			return out, nil
		}
		return getBulk(ctx, svc, ids, in.SectionFilter)
	},
}

func getDiff(ctx context.Context, svc *Service, idA, idB string) (string, error) {
	artifactA, err := svc.Proto.GetArtifact(ctx, idA)
	if err != nil {
		return "", err
	}
	artifactB, err := svc.Proto.GetArtifact(ctx, idB)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, f := range []struct{ name, va, vb string }{
		{"kind", artifactA.Label(parchment.LabelPrefixKind), artifactB.Label(parchment.LabelPrefixKind)}, {"scope", artifactA.Label(parchment.LabelPrefixScope), artifactB.Label(parchment.LabelPrefixScope)},
		{"status", parchment.StatusFromLabels(artifactA.Labels), parchment.StatusFromLabels(artifactB.Labels)}, {"title", artifactA.Title, artifactB.Title},
		{"parent", parentOf(ctx, svc.Proto.Store(), artifactA.ID), parentOf(ctx, svc.Proto.Store(), artifactB.ID)}, {"priority", artifactA.Label(parchment.LabelPrefixPriority), artifactB.Label(parchment.LabelPrefixPriority)},
	} {
		if f.va != f.vb {
			lines = append(lines, fmt.Sprintf("  %s: %q → %q", f.name, f.va, f.vb))
		}
	}
	secA := make(map[string]string, len(artifactA.Sections))
	for _, s := range artifactA.Sections {
		secA[s.Name] = s.Text
	}
	secB := make(map[string]string, len(artifactB.Sections))
	for _, s := range artifactB.Sections {
		secB[s.Name] = s.Text
	}
	for name, textA := range secA {
		if textB, ok := secB[name]; !ok {
			lines = append(lines, fmt.Sprintf("  section %q: removed", name))
		} else if textA != textB {
			lines = append(lines, fmt.Sprintf("  section %q: modified (%d → %d bytes)", name, len(textA), len(textB)))
		}
	}
	for name := range secB {
		if _, ok := secA[name]; !ok {
			lines = append(lines, fmt.Sprintf("  section %q: added", name))
		}
	}
	if len(lines) == 0 {
		return fmt.Sprintf("no differences between %s and %s", idA, idB), nil
	}
	return fmt.Sprintf("diff %s vs %s:\n%s", idA, idB, strings.Join(lines, "\n")), nil
}

func getSummary(ctx context.Context, svc *Service, ids []string) (string, error) {
	type summary struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Kind     string `json:"kind"`
		Scope    string `json:"scope"`
		Status   string `json:"status"`
		Priority string `json:"priority,omitempty"`
		Parent   string `json:"parent,omitempty"`
		Sprint   string `json:"sprint,omitempty"`
	}
	results := make([]summary, 0, len(ids))
	for _, id := range ids {
		art, err := svc.Proto.GetArtifact(ctx, id)
		if err != nil {
			return "", fmt.Errorf("get %s: %w", id, err)
		}
		results = append(results, summary{
			ID: art.ID, Title: art.Title, Kind: art.Label(parchment.LabelPrefixKind), Scope: art.Label(parchment.LabelPrefixScope),
			Status: parchment.StatusFromLabels(art.Labels), Priority: art.Label(parchment.LabelPrefixPriority), Parent: parentOf(ctx, svc.Proto.Store(), art.ID), Sprint: art.Label(parchment.LabelPrefixSprint),
		})
	}
	if len(results) == 1 {
		data, _ := json.Marshal(results[0])
		return string(data), nil
	}
	data, _ := json.Marshal(results)
	return string(data), nil
}

// renderScoredTable renders ScoredArtifact results with a SCORE column.
func renderScoredTable(results []parchment.ScoredArtifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-20s  %5s  %-40s  %-12s  %s\n", "ID", "SCORE", "TITLE", "KIND", "STATUS")
	b.WriteString(strings.Repeat("-", 90) + "\n")
	for _, r := range results {
		title := r.Artifact.Title
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		score := fmt.Sprintf("%.2f", r.Score)
		if r.Score == 0 {
			score = "fts"
		}
		fmt.Fprintf(&b, "%-20s  %5s  %-40s  %-12s  %s\n",
			r.Artifact.ID, score, title, r.Artifact.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(r.Artifact.Labels))
	}
	return b.String()
}

// renderWithBriefing renders search results with an ArtifactTree chain attached to each.
func renderWithBriefing(ctx context.Context, svc *Service, arts []*parchment.Artifact, depth int, relation, direction string) string {
	if relation == "" {
		relation = "*"
	}
	if direction == "" {
		direction = "both"
	}
	var b strings.Builder
	for _, art := range arts {
		b.WriteString(art.ID + " " + art.Title + "\n")
		tree, err := svc.Proto.ArtifactTree(ctx, parchment.TreeInput{
			ID: art.ID, Relation: relation, Direction: direction, Depth: depth,
		})
		if err == nil && tree != nil && len(tree.Children) > 0 {
			b.WriteString(renderBriefing(tree))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func getBriefing(ctx context.Context, svc *Service, id string, depth int) (string, error) {
	tree, err := svc.Proto.ArtifactTree(ctx, parchment.TreeInput{
		ID: id, Relation: "*", Direction: "both", Depth: depth,
	})
	if err != nil {
		return "", err
	}
	return renderBriefing(tree), nil
}

func getImpact(ctx context.Context, svc *Service, id string) (string, error) {
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return "", err
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("Impact analysis for %s [%s] %s:", id, parchment.StatusFromLabels(art.Labels), art.Title))
	children, _ := svc.Proto.Store().Children(ctx, id)
	if len(children) > 0 {
		lines = append(lines, fmt.Sprintf("\nChildren (%d):", len(children)))
		for _, ch := range children {
			lines = append(lines, fmt.Sprintf("  %s [%s] %s", ch.ID, parchment.StatusFromLabels(ch.Labels), ch.Title))
		}
	}
	depEdges, _ := svc.Proto.GetArtifactEdges(ctx, id)
	var dependents, implementors []string
	for _, e := range depEdges {
		if e.Direction == "incoming" { //nolint:goconst // "incoming" is a domain constant defined in parchment
			switch e.Relation {
			case "depends_on":
				dependents = append(dependents, fmt.Sprintf("  %s [%s] %s", e.Target.ID, parchment.StatusFromLabels(e.Target.Labels), e.Target.Title))
			case "implements":
				implementors = append(implementors, fmt.Sprintf("  %s [%s] %s", e.Target.ID, parchment.StatusFromLabels(e.Target.Labels), e.Target.Title))
			}
		}
	}
	if len(dependents) > 0 {
		lines = append(lines, fmt.Sprintf("\nDepends on this (%d):", len(dependents)))
		lines = append(lines, dependents...)
	}
	if len(implementors) > 0 {
		lines = append(lines, fmt.Sprintf("\nImplements this (%d):", len(implementors)))
		lines = append(lines, implementors...)
	}
	if len(children) == 0 && len(dependents) == 0 && len(implementors) == 0 {
		lines = append(lines, "\nNo downstream impact — safe to archive.")
	}
	return strings.Join(lines, "\n"), nil
}

func getBulk(ctx context.Context, svc *Service, ids, sectionFilter []string) (string, error) {
	arts := make([]*parchment.Artifact, 0, len(ids))
	for _, id := range ids {
		art, err := svc.Proto.GetArtifact(ctx, id)
		if err != nil {
			return "", fmt.Errorf("get %s: %w", id, err)
		}
		FilterSections(art, sectionFilter)
		arts = append(arts, art)
	}
	data, _ := json.Marshal(arts)
	return string(data), nil
}

func renderTree(node *parchment.TreeNode) string {
	var b strings.Builder
	renderTreeNode(node, "", true, countDistinctScopes(node) > 1, &b)
	return b.String()
}

func renderTreeNode(node *parchment.TreeNode, prefix string, last, showScope bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	edgeLabel := ""
	if node.Edge != "" {
		arrow := " -> "
		if node.Direction == "incoming" {
			arrow = " <- "
		}
		edgeLabel = node.Edge + arrow
	}
	nodeScope := labelVal(node.Labels, parchment.LabelPrefixScope)
	scopeLabel := ""
	if showScope && nodeScope != "" {
		scopeLabel = fmt.Sprintf(" [%s]", nodeScope)
	}
	fmt.Fprintf(b, "%s%s%s%s%s [%s] %s\n", prefix, connector, edgeLabel, node.ID, scopeLabel, parchment.StatusFromLabels(node.Labels), node.Title)
	childPrefix := prefix
	if prefix != "" {
		if last {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}
	for i, ch := range node.Children {
		renderTreeNode(ch, childPrefix, i == len(node.Children)-1, showScope, b)
	}
}

func renderBriefing(node *parchment.TreeNode) string {
	var b strings.Builder
	renderBriefingNode(node, "", true, countDistinctScopes(node) > 1, &b)
	return b.String()
}

func renderBriefingNode(node *parchment.TreeNode, prefix string, last, showScope bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	edgeLabel := ""
	if node.Edge != "" {
		arrow := " -> "
		if node.Direction == "incoming" {
			arrow = " <- "
		}
		edgeLabel = node.Edge + arrow
	}
	briefingScope := labelVal(node.Labels, parchment.LabelPrefixScope)
	scopeLabel := ""
	if showScope && briefingScope != "" {
		scopeLabel = fmt.Sprintf(" [%s]", briefingScope)
	}
	nodeKind := labelVal(node.Labels, parchment.LabelPrefixKind)
	nodeStatus := parchment.StatusFromLabels(node.Labels)
	kindStatus := nodeStatus
	if nodeKind != "" {
		kindStatus = nodeKind + "|" + nodeStatus
	}
	fmt.Fprintf(b, "%s%s%s%s%s [%s] %s\n", prefix, connector, edgeLabel, node.ID, scopeLabel, kindStatus, node.Title)
	childPrefix := prefix
	if prefix != "" {
		if last {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}
	for i, ch := range node.Children {
		renderBriefingNode(ch, childPrefix, i == len(node.Children)-1, showScope, b)
	}
}

func countDistinctScopes(node *parchment.TreeNode) int {
	scopes := map[string]struct{}{}
	var walk func(n *parchment.TreeNode)
	walk = func(n *parchment.TreeNode) {
		if sc := labelVal(n.Labels, parchment.LabelPrefixScope); sc != "" {
			scopes[sc] = struct{}{}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(node)
	return len(scopes)
}

type orientInput struct {
	Scope string `json:"scope,omitempty"`
}

var opOrient = Op{
	Name: "orient",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in orientInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		return svc.KnowledgeOrient(ctx, in.Scope)
	},
}

type listInput struct {
	ID             string   `json:"id,omitempty"`
	Unblocked      bool     `json:"unblocked,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	Status         string   `json:"status,omitempty"`
	Sprint         string   `json:"sprint,omitempty"`
	IDPrefix       string   `json:"id_prefix,omitempty"`
	ExcludeKind    string   `json:"exclude_kind,omitempty"`
	ExcludeStatus  string   `json:"exclude_status,omitempty"`
	Labels         []string `json:"labels,omitempty"`
	LabelsOr       []string `json:"labels_or,omitempty"`
	ExcludeLabels  []string `json:"exclude_labels,omitempty"`
	Query          string   `json:"query,omitempty"`
	TitleContains  string   `json:"title_contains,omitempty"`
	GroupBy        string   `json:"group_by,omitempty"`
	Sort           string   `json:"sort,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Cursor         string   `json:"cursor,omitempty"` // pagination cursor from previous list response
	Top            int      `json:"top,omitempty"`
	Count          bool     `json:"count,omitempty"`
	Fields         []string `json:"fields,omitempty"`
	Format         string   `json:"format,omitempty"`
	Ranked         bool     `json:"ranked,omitempty"`
	Semantic       bool     `json:"semantic,omitempty"`  // deprecated: use Mode=semantic
	Mode           string   `json:"mode,omitempty"`      // fts (default) | semantic | hybrid
	Session        string   `json:"session,omitempty"`   // shorthand for labels=["session:<value>"]
	Depth          int      `json:"depth,omitempty"`     // if >0, attach ArtifactTree to each result
	Relation       string   `json:"relation,omitempty"`  // edge relation to traverse with depth; default "*" (all)
	Direction      string   `json:"direction,omitempty"` // inbound | outbound | both (default)
	Family         string   `json:"family,omitempty"`
	CreatedAfter   string   `json:"created_after,omitempty"`
	CreatedBefore  string   `json:"created_before,omitempty"`
	UpdatedAfter   string   `json:"updated_after,omitempty"`
	UpdatedBefore  string   `json:"updated_before,omitempty"`
	InsertedAfter  string   `json:"inserted_after,omitempty"`
	InsertedBefore string   `json:"inserted_before,omitempty"`
}

func resolveIDs(ids []string, id string) []string {
	if len(ids) > 0 {
		return ids
	}
	if id != "" {
		return []string{id}
	}
	return nil
}

var listValidFields = map[string]func(*parchment.Artifact) string{
	"id":         func(a *parchment.Artifact) string { return a.ID },
	"kind":       func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixKind) },
	"scope":      func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixScope) },
	"status":     func(a *parchment.Artifact) string { return parchment.StatusFromLabels(a.Labels) },
	"title":      func(a *parchment.Artifact) string { return a.Title },

	"priority":   func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixPriority) },
	"sprint":     func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixSprint) },
	"depends_on": func(a *parchment.Artifact) string { return "" },
	"labels":     func(a *parchment.Artifact) string { return strings.Join(a.Labels, ",") },
}

func listCompact(arts []*parchment.Artifact, fields []string, li *parchment.ListInput) (string, error) {
	getters := make([]func(*parchment.Artifact) string, 0, len(fields))
	for _, f := range fields {
		g, ok := listValidFields[f]
		if !ok {
			return "", fmt.Errorf("unknown field %q (valid: id, kind, scope, status, title, parent, priority, sprint, depends_on, labels)", f) //nolint:err113 // agent-facing hint
		}
		getters = append(getters, g)
	}
	total := len(arts)
	if li.Limit > 0 && li.Limit < len(arts) {
		arts = arts[:li.Limit]
	}
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteString("\t")
		}
		b.WriteString(strings.ToUpper(f))
	}
	b.WriteString("\n")
	for _, a := range arts {
		for i, g := range getters {
			if i > 0 {
				b.WriteString("\t")
			}
			b.WriteString(g(a))
		}
		b.WriteString("\n")
	}
	if li.Limit > 0 && li.Limit < total {
		fmt.Fprintf(&b, "\n(%d of %d artifacts)\n", len(arts), total)
	} else {
		fmt.Fprintf(&b, "\n(%d artifacts)\n", len(arts))
	}
	return b.String(), nil
}

var opQuery = Op{
	Name: "query",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) { //nolint:cyclop // multi-mode list: count|top|compact|grouped|default
		var in listInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		// sort=topo: topological dependency order rooted at id=.
		if in.Sort == "topo" {
			if in.ID == "" {
				return "", fmt.Errorf("id required for sort=topo") //nolint:err113 // user-facing hint
			}
			entries, err := svc.Proto.TopoSort(ctx, in.ID)
			if err != nil && len(entries) == 0 {
				return "", err
			}
			if in.Unblocked {
				limit := in.Depth
				if limit <= 0 {
					limit = 5
				}
				var ready []parchment.TopoEntry
				for _, e := range entries {
					if svc.Proto.IsTerminal(parchment.StatusFromLabels(e.Labels)) {
						continue
					}
					art, _ := svc.Proto.GetArtifact(ctx, e.ID)
					if art == nil {
						continue
					}
					blocked := false
					depEdges, _ := svc.Proto.Store().Neighbors(ctx, art.ID, parchment.RelDependsOn, parchment.Outgoing)
					for _, de := range depEdges {
						dep, _ := svc.Proto.GetArtifact(ctx, de.To)
						if dep != nil && !svc.Proto.IsTerminal(parchment.StatusFromLabels(dep.Labels)) {
							blocked = true
							break
						}
					}
					if !blocked {
						ready = append(ready, e)
						if len(ready) >= limit {
							break
						}
					}
				}
				if len(ready) == 0 {
					return "no unblocked tasks found", nil
				}
				entries = ready
			}
			if in.Format == "json" {
				data, _ := json.Marshal(entries)
				return string(data), nil
			}
			var b strings.Builder
			for i, e := range entries {
				fmt.Fprintf(&b, "%d. %s [%s] %s", i+1, e.ID, parchment.StatusFromLabels(e.Labels), e.Title)
				if e.Priority != "" && e.Priority != "none" {
					fmt.Fprintf(&b, " (%s)", e.Priority)
				}
				b.WriteString("\n")
			}
			if err != nil {
				fmt.Fprintf(&b, "\n%s\n", err)
			}
			return b.String(), nil
		}

		const modeSemantic = "semantic"
		const modeHybrid = "hybrid"
		// Normalize mode: legacy Semantic bool → mode=semantic.
		mode := in.Mode
		if mode == "" && in.Semantic {
			mode = modeSemantic
		}
		// Session shorthand: prepend "session:<value>" to labels filter.
		if in.Session != "" {
			in.Labels = append([]string{"session:" + in.Session}, in.Labels...)
		}

		if mode == modeSemantic || mode == modeHybrid {
			if in.Query == "" {
				return "", fmt.Errorf("query required for %s search", mode) //nolint:err113 // user-facing hint
			}
			semLabels := in.Labels
			if in.Kind != "" {
				semLabels = append([]string{parchment.LabelPrefixKind + in.Kind}, semLabels...)
			}
			if in.Scope != "" {
				semLabels = append(semLabels, parchment.LabelPrefixScope+in.Scope)
			}
			li := parchment.ListInput{Limit: in.Limit, Labels: semLabels}
			var scored []parchment.ScoredArtifact
			var semErr error
			scored, semErr = svc.Proto.SearchSemantic(ctx, in.Query, li)
			if semErr != nil && mode == modeSemantic {
				return "", fmt.Errorf("semantic search requires an embedding backend: %w", semErr)
			}
			if mode == modeHybrid {
				// Merge FTS results as unscored (score=0) — deduplicate by ID.
				ftsResults, _ := svc.Proto.SearchArtifacts(ctx, in.Query, li)
				seen := make(map[string]bool, len(scored))
				for _, s := range scored {
					seen[s.Artifact.ID] = true
				}
				for _, a := range ftsResults {
					if !seen[a.ID] {
						scored = append(scored, parchment.ScoredArtifact{Artifact: a})
						seen[a.ID] = true
					}
				}
			}
			if semErr != nil {
				// Semantic unavailable in hybrid mode — fell back to FTS only.
				results, ferr := svc.Recall(ctx, in.Query, in.Scope, in.Top)
				if ferr != nil {
					return "", fmt.Errorf("hybrid search: FTS fallback failed: %w", ferr)
				}
				scored = make([]parchment.ScoredArtifact, len(results))
				for i, r := range results {
					scored[i] = parchment.ScoredArtifact{Artifact: r.Art}
				}
			}
			if len(scored) == 0 {
				return fmt.Sprintf("no results for %q", in.Query), nil
			}
			if in.Depth > 0 {
				arts := make([]*parchment.Artifact, len(scored))
				for i, s := range scored {
					arts[i] = s.Artifact
				}
				return renderWithBriefing(ctx, svc, arts, in.Depth, in.Relation, in.Direction), nil
			}
			return renderScoredTable(scored), nil
		}
		if in.Ranked {
			if in.Query == "" {
				return "", fmt.Errorf("query required for ranked list") //nolint:err113 // user-facing hint
			}
			results, err := svc.Recall(ctx, in.Query, in.Scope, in.Top)
			if err != nil {
				return "", err
			}
			if len(results) == 0 {
				return fmt.Sprintf("no results for %q", in.Query), nil
			}
			arts := make([]*parchment.Artifact, len(results))
			for i, r := range results {
				arts[i] = r.Art
			}
			return parchment.RenderTable(arts), nil
		}

		listLabels := in.Labels
		if in.Kind != "" {
			listLabels = append([]string{parchment.LabelPrefixKind + in.Kind}, listLabels...)
		}
		if in.Status != "" {
			listLabels = append(listLabels, statusLabelFor(in.Status))
		}
		if in.Scope != "" {
			listLabels = append(listLabels, parchment.LabelPrefixScope+in.Scope)
		}
		if in.Sprint != "" {
			listLabels = append(listLabels, parchment.LabelPrefixSprint+in.Sprint)
		}
		listExclude := in.ExcludeLabels
		if in.ExcludeKind != "" {
			listExclude = append(listExclude, parchment.LabelPrefixKind+in.ExcludeKind)
		}
		if in.ExcludeStatus != "" {
			listExclude = append(listExclude, statusLabelFor(in.ExcludeStatus))
		}
		li := parchment.ListInput{
			IDPrefix: in.IDPrefix,
			Labels: listLabels, LabelsOr: in.LabelsOr, ExcludeLabels: listExclude,
			GroupBy: in.GroupBy, Sort: in.Sort, Limit: in.Limit, Cursor: in.Cursor, Query: in.Query,
			TitleContains: in.TitleContains, Family: in.Family,
			CreatedAfter: in.CreatedAfter, CreatedBefore: in.CreatedBefore,
			UpdatedAfter: in.UpdatedAfter, UpdatedBefore: in.UpdatedBefore,
			InsertedAfter: in.InsertedAfter, InsertedBefore: in.InsertedBefore,
		}

		// Use paginated path when cursor or limit is requested.
		if in.Cursor != "" || (in.Limit > 0 && in.Query == "") {
			page, err := svc.Proto.ListPage(ctx, li)
			if err != nil {
				return "", err
			}
			out := parchment.RenderTable(page.Artifacts)
			if page.NextCursor != "" {
				out += fmt.Sprintf("\nnext_cursor: %s  (pass as cursor= to continue)", page.NextCursor)
			}
			return out, nil
		}

		var arts []*parchment.Artifact
		var err error
		if li.Query != "" {
			arts, err = svc.Proto.SearchArtifacts(ctx, li.Query, li)
		} else {
			arts, err = svc.Proto.ListArtifacts(ctx, li)
		}
		if err != nil {
			return "", err
		}

		if in.Count {
			if in.GroupBy != "" {
				groups := make(map[string]int)
				for _, a := range arts {
					var key string
					switch in.GroupBy {
					case "status":
						key = parchment.StatusFromLabels(a.Labels)
					case "scope":
						key = a.Label(parchment.LabelPrefixScope)
					case "kind":
						key = a.Label(parchment.LabelPrefixKind)
					case "sprint":
						key = a.Label(parchment.LabelPrefixSprint)
					default:
						key = "unknown"
					}
					if key == "" {
						key = "(none)"
					}
					groups[key]++
				}
				data, _ := json.Marshal(groups)
				return string(data), nil
			}
			return fmt.Sprintf("%d", len(arts)), nil
		}

		if len(in.Fields) > 0 {
			return listCompact(arts, in.Fields, &li)
		}

		if in.Top > 0 {
			sort.Slice(arts, func(i, j int) bool {
				return RelevanceScore(arts[i]) > RelevanceScore(arts[j])
			})
			if in.Top < len(arts) {
				arts = arts[:in.Top]
			}
			data, _ := json.Marshal(arts)
			return string(data), nil
		}

		if in.Sort != "" {
			SortArtifacts(arts, in.Sort)
		}
		total := len(arts)
		if li.Limit > 0 && li.Limit < len(arts) {
			arts = arts[:li.Limit]
		}

		if in.Format == "json" {
			data, _ := json.Marshal(arts)
			return string(data), nil
		}

		if in.GroupBy != "" {
			return parchment.RenderGroupedTable(arts, in.GroupBy), nil
		}

		if in.Depth > 0 {
			return renderWithBriefing(ctx, svc, arts, in.Depth, in.Relation, in.Direction), nil
		}

		out := parchment.RenderTable(arts)
		if li.Limit > 0 && li.Limit < total {
			out += fmt.Sprintf("\n(showing %d of %d total)", len(arts), total)
		}
		isUnfiltered := li.Query == "" && li.TitleContains == "" &&
			len(li.Labels) == 0 && len(li.LabelsOr) == 0 && len(li.ExcludeLabels) == 0 &&
			li.IDPrefix == "" && li.Limit == 0
		if isUnfiltered && total > 0 {
			out += fmt.Sprintf("\n(%d artifacts — use top=10 for relevance ranking or add scope/kind/status filters to narrow)", total)
		}
		return out, nil
	},
}

func parentOf(ctx context.Context, store parchment.Store, id string) string {
	edges, _ := store.Neighbors(ctx, id, parchment.RelParentOf, parchment.Incoming)
	if len(edges) > 0 {
		return edges[0].From
	}
	return ""
}
