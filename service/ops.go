package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opSet, opList, opUpdate, opOrient, opCreate, opGet, opTopoSort, opLink)
}

type edgeInput struct {
	From     string `json:"from"`
	Relation string `json:"relation"`
	To       string `json:"to"`
}

type topoSortInput struct {
	ID        string `json:"id"`
	Unblocked bool   `json:"unblocked,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	Format    string `json:"format,omitempty"`
}

var opTopoSort = Op{
	Name: "topo_sort",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in topoSortInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" {
			return "", fmt.Errorf("id required for topo_sort") //nolint:err113 // user-facing hint
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
			schema := svc.Proto.Schema()
			var ready []parchment.TopoEntry
			for _, e := range entries {
				if schema.IsTerminal(e.Status) {
					continue
				}
				art, _ := svc.Proto.GetArtifact(ctx, e.ID)
				if art == nil {
					continue
				}
				blocked := false
				for _, depID := range art.DependsOn {
					dep, _ := svc.Proto.GetArtifact(ctx, depID)
					if dep != nil && !schema.IsTerminal(dep.Status) {
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
			fmt.Fprintf(&b, "%d. %s [%s] %s", i+1, e.ID, e.Status, e.Title)
			if e.Priority != "" && e.Priority != "none" {
				fmt.Fprintf(&b, " (%s)", e.Priority)
			}
			b.WriteString("\n")
		}
		if err != nil {
			fmt.Fprintf(&b, "\n%s\n", err)
		}
		return b.String(), nil
	},
}

type linkInput struct {
	ID          string      `json:"id"`
	Relation    string      `json:"relation"`
	Targets     []string    `json:"targets,omitempty"`
	Target      string      `json:"target,omitempty"`
	ReplaceFrom string      `json:"replace_from,omitempty"`
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
		if in.ReplaceFrom != "" {
			if in.ID == "" || in.Relation == "" || in.Target == "" {
				return "", fmt.Errorf("id, relation, replace_from, and target required") //nolint:err113 // user-facing hint
			}
			if _, err := svc.Proto.UnlinkArtifacts(ctx, in.ID, in.Relation, []string{in.ReplaceFrom}); err != nil {
				return "", fmt.Errorf("unlink old: %w", err)
			}
			if _, err := svc.Proto.LinkArtifacts(ctx, in.ID, in.Relation, []string{in.Target}, in.Weight); err != nil {
				return "", fmt.Errorf("link new: %w", err)
			}
			return fmt.Sprintf("replaced %s -[%s]-> %s with %s", in.ID, in.Relation, in.ReplaceFrom, in.Target), nil
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
	StashID       string   `json:"stash_id,omitempty"`
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
		if in.StashID != "" {
			return getStash(ctx, svc, in.StashID)
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

func getStash(_ context.Context, svc *Service, stashID string) (string, error) {
	stashed, err := svc.Proto.Stash().Get(stashID)
	if err != nil {
		return "", fmt.Errorf("stash %s: %w", stashID, err)
	}
	data, _ := json.Marshal(stashed.Input)
	ttl := 10 * time.Minute
	age := time.Since(stashed.CreatedAt).Round(time.Second)
	return fmt.Sprintf("stash %s (age: %v, expires in ~%v):\n%s",
		stashID, age, (ttl - age).Round(time.Second), string(data)), nil
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
		{"kind", artifactA.Kind, artifactB.Kind}, {"scope", artifactA.Scope, artifactB.Scope},
		{"status", artifactA.Status, artifactB.Status}, {"title", artifactA.Title, artifactB.Title},
		{"parent", artifactA.Parent, artifactB.Parent}, {"priority", artifactA.Priority, artifactB.Priority},
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
			ID: art.ID, Title: art.Title, Kind: art.Kind, Scope: art.Scope,
			Status: art.Status, Priority: art.Priority, Parent: art.Parent, Sprint: art.Sprint,
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
			r.Artifact.ID, score, title, r.Artifact.ResolvedKind(), r.Artifact.ResolvedStatus())
	}
	return b.String()
}

// renderWithBriefing renders search results with an ArtifactTree chain attached to each.
func renderWithBriefing(ctx context.Context, svc *Service, arts []*parchment.Artifact, depth int) string {
	var b strings.Builder
	for _, art := range arts {
		b.WriteString(art.ID + " " + art.Title + "\n")
		tree, err := svc.Proto.ArtifactTree(ctx, parchment.TreeInput{
			ID: art.ID, Relation: "*", Direction: "both", Depth: depth,
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
	lines = append(lines, fmt.Sprintf("Impact analysis for %s [%s] %s:", id, art.Status, art.Title))
	children, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Parent: id})
	if len(children) > 0 {
		lines = append(lines, fmt.Sprintf("\nChildren (%d):", len(children)))
		for _, ch := range children {
			lines = append(lines, fmt.Sprintf("  %s [%s] %s", ch.ID, ch.Status, ch.Title))
		}
	}
	depEdges, _ := svc.Proto.GetArtifactEdges(ctx, id)
	var dependents, implementors []string
	for _, e := range depEdges {
		if e.Direction == "incoming" { //nolint:goconst // "incoming" is a domain constant defined in parchment
			switch e.Relation {
			case "depends_on":
				dependents = append(dependents, fmt.Sprintf("  %s [%s] %s", e.Target.ID, e.Target.Status, e.Target.Title))
			case "implements":
				implementors = append(implementors, fmt.Sprintf("  %s [%s] %s", e.Target.ID, e.Target.Status, e.Target.Title))
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
	scopeLabel := ""
	if showScope && node.Scope != "" {
		scopeLabel = fmt.Sprintf(" [%s]", node.Scope)
	}
	fmt.Fprintf(b, "%s%s%s%s%s [%s] %s\n", prefix, connector, edgeLabel, node.ID, scopeLabel, node.Status, node.Title)
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
	scopeLabel := ""
	if showScope && node.Scope != "" {
		scopeLabel = fmt.Sprintf(" [%s]", node.Scope)
	}
	kindStatus := node.Status
	if node.Kind != "" {
		kindStatus = node.Kind + "|" + node.Status
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
		if n.Scope != "" {
			scopes[n.Scope] = struct{}{}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(node)
	return len(scopes)
}

type createInput struct {
	Kind      string              `json:"kind,omitempty"`
	Title     string              `json:"title,omitempty"`
	Scope     string              `json:"scope,omitempty"`
	Goal      string              `json:"goal,omitempty"`
	Parent    string              `json:"parent,omitempty"`
	Prefix    string              `json:"prefix,omitempty"`
	ID        string              `json:"id,omitempty"`
	Status    string              `json:"status,omitempty"`
	Priority  string              `json:"priority,omitempty"`
	Labels    []string            `json:"labels,omitempty"`
	DependsOn []string            `json:"depends_on,omitempty"`
	Sections  []map[string]string `json:"sections,omitempty"`
	Links     map[string][]string `json:"links,omitempty"`
	Extra     map[string]any      `json:"extra,omitempty"`
	Patch     map[string]string   `json:"patch,omitempty"`
	SkipHooks bool                `json:"skip_hooks,omitempty"`
	CreatedAt string              `json:"created_at,omitempty"`
	StashID   string              `json:"stash_id,omitempty"`
	CloneFrom string              `json:"clone_from,omitempty"`
	Artifacts []map[string]any    `json:"artifacts,omitempty"`
}

var opCreate = Op{
	Name: "create",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) { //nolint:cyclop // routing: stash|clone|batch|single — each path is simple
		var in createInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.StashID != "" {
			return createFromStash(ctx, svc, &in)
		}
		if in.CloneFrom != "" {
			return createClone(ctx, svc, &in)
		}
		if len(in.Artifacts) > 0 {
			return createBatch(ctx, svc, &in)
		}
		return createSingle(ctx, svc, &in)
	},
}

func parseSections(raw []map[string]string) []parchment.Section {
	var out []parchment.Section
	for _, sec := range raw {
		name := sec["name"]
		if name == "" {
			name = sec["slug"]
		}
		if name != "" {
			t := sec["text"]
			if t == "" {
				t = sec["body"]
			}
			out = append(out, parchment.Section{Name: name, Text: t})
		}
	}
	return out
}

func createSingle(ctx context.Context, svc *Service, in *createInput) (string, error) {
	if in.Title == "" {
		return "", fmt.Errorf("title is required") //nolint:err113 // user-facing hint
	}
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: in.Kind, Title: in.Title, Scope: in.Scope,
		Goal: in.Goal, Parent: in.Parent, Prefix: in.Prefix,
		ExplicitID: in.ID, Status: in.Status, Priority: in.Priority,
		Labels: in.Labels, DependsOn: in.DependsOn, Sections: parseSections(in.Sections),
		Links: in.Links, Extra: in.Extra, Patch: in.Patch, SkipHooks: in.SkipHooks,
		CreatedAt: in.CreatedAt,
	})
	if err != nil {
		var ce *parchment.ConformanceError
		if errors.As(err, &ce) {
			return "", fmt.Errorf("%s [stash_id=%s]", ce.Error(), ce.StashID) //nolint:err113 // structured stash ID
		}
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "created %s [%s|%s] %s", art.ID, art.Kind, art.Status, art.Title)
	if art.Parent != "" {
		fmt.Fprintf(&b, " (parent: %s)", art.Parent)
	}
	schema := svc.Proto.Schema()
	if expected := schema.GetExpectedSections(art.Kind); len(expected) > 0 {
		must := schema.GetMustSections(art.Kind)
		should := schema.GetShouldSections(art.Kind)
		var hints []string
		for _, s := range must {
			hints = append(hints, s+" (must)")
		}
		for _, s := range should {
			hints = append(hints, s+" (should)")
		}
		if len(hints) > 0 {
			fmt.Fprintf(&b, "\nSections: %s", strings.Join(hints, ", "))
		}
	}
	return b.String(), nil
}

func createFromStash(ctx context.Context, svc *Service, in *createInput) (string, error) {
	art, err := svc.Proto.PromoteStash(ctx, in.StashID, parchment.CreateInput{
		Kind: in.Kind, Title: in.Title, Scope: in.Scope,
		Goal: in.Goal, Parent: in.Parent, Status: in.Status,
		Priority: in.Priority, Labels: in.Labels,
		Links: in.Links, Sections: parseSections(in.Sections), Patch: in.Patch,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("promoted stash to %s: %s [%s|%s]", art.ID, art.Title, art.Kind, art.Status), nil
}

func createClone(ctx context.Context, svc *Service, in *createInput) (string, error) {
	source, err := svc.Proto.GetArtifact(ctx, in.CloneFrom)
	if err != nil {
		return "", fmt.Errorf("source %s: %w", in.CloneFrom, err)
	}
	kind := source.Kind
	if in.Kind != "" {
		kind = in.Kind
	}
	scope := source.Scope
	if in.Scope != "" {
		scope = in.Scope
	}
	title := source.Title
	if in.Title != "" {
		title = in.Title
	}
	labels := source.Labels
	if len(in.Labels) > 0 {
		labels = in.Labels
	}
	sections := make([]parchment.Section, 0, len(source.Sections))
	for _, s := range source.Sections {
		sections = append(sections, parchment.Section{Name: s.Name, Text: s.Text})
	}
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: kind, Title: title, Scope: scope, Goal: source.Goal,
		Parent: in.Parent, Status: in.Status, Priority: in.Priority,
		Labels: labels, Sections: sections,
	})
	if err != nil {
		return "", fmt.Errorf("clone: %w", err)
	}
	data, _ := json.Marshal(art)
	return fmt.Sprintf("cloned %s → %s\n%s", in.CloneFrom, art.ID, string(data)), nil
}

func createBatch(ctx context.Context, svc *Service, in *createInput) (string, error) {
	if len(in.Artifacts) == 0 {
		return "", fmt.Errorf("artifacts array is required for batch create") //nolint:err113 // user-facing hint
	}
	idRefs := make(map[string]string)
	var b strings.Builder
	fmt.Fprintf(&b, "created %d artifacts:\n", len(in.Artifacts))
	for i, rawArt := range in.Artifacts {
		data, _ := json.Marshal(rawArt)
		var ci createInput
		if err := json.Unmarshal(data, &ci); err != nil {
			return "", fmt.Errorf("artifact[%d]: %w", i, err)
		}
		if parent := ci.Parent; parent != "" && parent[0] == '$' {
			if resolved, ok := idRefs[parent]; ok {
				ci.Parent = resolved
			} else {
				return "", fmt.Errorf("artifact[%d]: unresolved parent reference %q", i, parent) //nolint:err113 // batch parent resolution error contains dynamic context
			}
		}
		art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind: ci.Kind, Title: ci.Title, Scope: ci.Scope,
			Goal: ci.Goal, Parent: ci.Parent, Status: ci.Status,
			Priority: ci.Priority, Labels: ci.Labels, Prefix: ci.Prefix,
			Links: ci.Links, Extra: ci.Extra, Sections: parseSections(ci.Sections),
		})
		if err != nil {
			return "", fmt.Errorf("artifact[%d] %q: %w", i, ci.Title, err)
		}
		idRefs[fmt.Sprintf("$%d", i)] = art.ID
		fmt.Fprintf(&b, "%s [%s] %s", art.ID, art.Kind, art.Title)
		if art.Parent != "" {
			fmt.Fprintf(&b, " (parent: %s)", art.Parent)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
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

type updateInput struct {
	ID             string              `json:"id"`
	IDs            []string            `json:"ids,omitempty"`
	Patch          map[string]string   `json:"patch,omitempty"`
	Status         string              `json:"status,omitempty"`
	Title          string              `json:"title,omitempty"`
	Goal           string              `json:"goal,omitempty"`
	Scope          string              `json:"scope,omitempty"`
	Parent         string              `json:"parent,omitempty"`
	Priority       string              `json:"priority,omitempty"`
	Sprint         string              `json:"sprint,omitempty"`
	Kind           string              `json:"kind,omitempty"`
	Sections       []map[string]string `json:"sections,omitempty"`
	SectionsDelete []string            `json:"sections_delete,omitempty"`
	Query          string              `json:"query,omitempty"`
	Text           string              `json:"text,omitempty"`
	Body           string              `json:"body,omitempty"`
	Force          bool                `json:"force,omitempty"`
}

var opUpdate = Op{
	Name: "update",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) { //nolint:cyclop // multi-path: fields+sections+find-replace+sections_delete
		var in updateInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		ids := resolveIDs(in.IDs, in.ID)
		if len(ids) == 0 {
			return "", fmt.Errorf("id or ids required") //nolint:err113 // user-facing hint
		}
		fieldMap := map[string]string{}
		for k, v := range in.Patch {
			fieldMap[k] = v
		}
		for field, value := range map[string]string{
			"status": in.Status, "title": in.Title, "goal": in.Goal,
			"scope": in.Scope, "parent": in.Parent, "priority": in.Priority,
			"sprint": in.Sprint, "kind": in.Kind,
		} {
			if value != "" {
				fieldMap[field] = value
			}
		}
		hasSectionReplace := in.Query != "" && (in.Text != "" || in.Body != "")
		if len(fieldMap) == 0 && len(in.Sections) == 0 && !hasSectionReplace && len(in.SectionsDelete) == 0 {
			return "", fmt.Errorf("update requires at least one field, section, sections_delete, or query+text for find-replace") //nolint:err113 // user-facing hint
		}
		var lines []string
		for _, id := range ids {
			for field, value := range fieldMap {
				results, err := svc.Proto.SetField(ctx, []string{id}, field, value, parchment.SetFieldOptions{Force: in.Force})
				if err != nil {
					lines = append(lines, fmt.Sprintf("%s -> error: set %s: %v", id, field, err))
					continue
				}
				r := results[0]
				if !r.OK {
					lines = append(lines, fmt.Sprintf("%s -> error: set %s: %s", id, field, r.Error))
					continue
				}
				lines = append(lines, fmt.Sprintf("%s.%s = %s", id, field, value))
			}
			for _, sec := range in.Sections {
				name, ok := sec["name"]
				if !ok || name == "" {
					continue
				}
				t := sec["text"]
				replaced, err := svc.Proto.AttachSection(ctx, id, name, t)
				if err != nil {
					lines = append(lines, fmt.Sprintf("%s -> error: section %q: %v", id, name, err))
					continue
				}
				if t != "" {
					_, _ = svc.Proto.SyncWikilinks(ctx, id)
				}
				action := "added"
				if replaced {
					action = "replaced"
				}
				lines = append(lines, fmt.Sprintf("%s: section %q %s", id, name, action))
			}
			if hasSectionReplace { //nolint:nestif // find-replace path is inherently branchy
				replacement := in.Text
				if replacement == "" {
					replacement = in.Body
				}
				art, err := svc.Proto.GetArtifact(ctx, id)
				if err != nil {
					lines = append(lines, fmt.Sprintf("%s -> error: %v", id, err))
					continue
				}
				updated := 0
				for _, sec := range art.Sections {
					if strings.Contains(sec.Text, in.Query) {
						newText := strings.ReplaceAll(sec.Text, in.Query, replacement)
						if _, err := svc.Proto.AttachSection(ctx, id, sec.Name, newText); err != nil {
							lines = append(lines, fmt.Sprintf("%s -> error: section %q: %v", id, sec.Name, err))
							continue
						}
						updated++
					}
				}
				lines = append(lines, fmt.Sprintf("%s: %d section(s) updated", id, updated))
			}
			for _, name := range in.SectionsDelete {
				removed, err := svc.Proto.DetachSection(ctx, id, name)
				if err != nil {
					lines = append(lines, fmt.Sprintf("%s -> error: detach %q: %v", id, name, err))
					continue
				}
				if removed {
					lines = append(lines, fmt.Sprintf("%s: section %q removed", id, name))
				} else {
					lines = append(lines, fmt.Sprintf("%s: section %q not found", id, name))
				}
			}
		}
		return strings.Join(lines, "\n"), nil
	},
}

type setInput struct {
	ID           string   `json:"id"`
	IDs          []string `json:"ids,omitempty"`
	Field        string   `json:"field"`
	Value        string   `json:"value"`
	Force        bool     `json:"force,omitempty"`
	BypassGuards bool     `json:"bypass_guards,omitempty"`
	Cascade      bool     `json:"cascade,omitempty"`
	DryRun       bool     `json:"dry_run,omitempty"`
	RenameID     bool     `json:"rename_id,omitempty"`
	Scope        string   `json:"scope,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Status       string   `json:"status,omitempty"`
	IDPrefix     string   `json:"id_prefix,omitempty"`
	ExcludeKind  string   `json:"exclude_kind,omitempty"`
}

var opSet = Op{
	Name: "set",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in setInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		ids := resolveIDs(in.IDs, in.ID)
		hasBulkFilter := in.Scope != "" || in.Kind != "" || in.Status != "" || in.IDPrefix != "" || in.ExcludeKind != ""
		if hasBulkFilter && len(ids) == 0 {
			arts, err := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
				Scope: in.Scope, Kind: in.Kind, Status: in.Status,
				IDPrefix: in.IDPrefix, ExcludeKind: in.ExcludeKind,
			})
			if err != nil {
				return "", err
			}
			if in.DryRun {
				affectedIDs := make([]string, len(arts))
				for i, a := range arts {
					affectedIDs[i] = a.ID
				}
				return fmt.Sprintf("dry run: would set %s=%s on %d artifact(s): %v", in.Field, in.Value, len(arts), affectedIDs), nil
			}
			for _, a := range arts {
				ids = append(ids, a.ID)
			}
			if len(ids) == 0 {
				return "0 artifacts matched filter", nil
			}
		}
		if len(ids) == 0 {
			return "", fmt.Errorf("provide id, ids, or filter params (scope, kind, status)") //nolint:err113 // user-facing hint
		}
		if in.Field == parchment.FieldStatus && in.Value == parchment.StatusActive && !in.Force {
			for _, id := range ids {
				art, err := svc.Proto.GetArtifact(ctx, id)
				if err != nil || art.Kind != parchment.KindTask {
					continue
				}
				if targets, ok := art.Links[parchment.RelImplements]; ok {
					for _, specID := range targets {
						if !svc.ReadLog[specID] {
							lines := make([]string, 0, 1)
							lines = append(lines, fmt.Sprintf("%s -> error: must read %s first (call get on implementing spec before activating)", id, specID))
							return strings.Join(lines, "\n"), nil
						}
					}
				}
			}
		}
		if in.DryRun {
			return fmt.Sprintf("dry run: would set %s=%s on %d artifact(s): %v", in.Field, in.Value, len(ids), ids), nil
		}
		results, err := svc.Proto.SetField(ctx, ids, in.Field, in.Value, parchment.SetFieldOptions{
			Force: in.Force, BypassGuards: in.BypassGuards, Cascade: in.Cascade, RenameID: in.RenameID,
		})
		if err != nil {
			return "", err
		}
		var lines []string
		for _, r := range results {
			if r.OK {
				line := fmt.Sprintf("%s.%s = %s", r.ID, in.Field, in.Value)
				if r.NewID != "" {
					line += fmt.Sprintf(" (renamed → %s)", r.NewID)
				}
				lines = append(lines, line)
			} else {
				lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
			}
		}
		return strings.Join(lines, "\n"), nil
	},
}

type listInput struct {
	Kind           string   `json:"kind,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	Status         string   `json:"status,omitempty"`
	Parent         string   `json:"parent,omitempty"`
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
	Semantic       bool     `json:"semantic,omitempty"` // deprecated: use Mode=semantic
	Mode           string   `json:"mode,omitempty"`     // fts (default) | semantic | hybrid
	Session        string   `json:"session,omitempty"`  // shorthand for labels=["session:<value>"]
	Depth          int      `json:"depth,omitempty"`    // if >0, attach ArtifactTree to each result
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
	"kind":       func(a *parchment.Artifact) string { return a.Kind },
	"scope":      func(a *parchment.Artifact) string { return a.Scope },
	"status":     func(a *parchment.Artifact) string { return a.Status },
	"title":      func(a *parchment.Artifact) string { return a.Title },
	"parent":     func(a *parchment.Artifact) string { return a.Parent },
	"priority":   func(a *parchment.Artifact) string { return a.Priority },
	"sprint":     func(a *parchment.Artifact) string { return a.Sprint },
	"depends_on": func(a *parchment.Artifact) string { return strings.Join(a.DependsOn, ",") },
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

var opList = Op{
	Name: "list",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) { //nolint:cyclop // multi-mode list: count|top|compact|grouped|default
		var in listInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
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
			li := parchment.ListInput{Scope: in.Scope, Kind: in.Kind, Limit: in.Limit, Labels: in.Labels}
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
				return renderWithBriefing(ctx, svc, arts, in.Depth), nil
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

		li := parchment.ListInput{
			Kind: in.Kind, Scope: in.Scope, Status: in.Status,
			Parent: in.Parent, Sprint: in.Sprint, IDPrefix: in.IDPrefix,
			ExcludeKind: in.ExcludeKind, ExcludeStatus: in.ExcludeStatus,
			Labels: in.Labels, LabelsOr: in.LabelsOr, ExcludeLabels: in.ExcludeLabels,
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
						key = a.Status
					case "scope":
						key = a.Scope
					case "kind":
						key = a.Kind
					case "sprint":
						key = a.Sprint
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

		out := parchment.RenderTable(arts)
		if li.Limit > 0 && li.Limit < total {
			out += fmt.Sprintf("\n(showing %d of %d total)", len(arts), total)
		}
		isUnfiltered := li.Kind == "" && li.Scope == "" && li.Status == "" &&
			li.Query == "" && li.TitleContains == "" && len(li.Labels) == 0 &&
			len(li.LabelsOr) == 0 && li.Parent == "" && li.IDPrefix == "" &&
			li.ExcludeKind == "" && li.ExcludeStatus == "" && li.Limit == 0
		if isUnfiltered && total > 0 {
			out += fmt.Sprintf("\n(%d artifacts — use top=10 for relevance ranking or add scope/kind/status filters to narrow)", total)
		}
		return out, nil
	},
}
