package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

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
		case "context":
			return getContext(ctx, svc, in.ID)
		}
		if len(ids) == 1 {
			art, err := svc.Proto.GetArtifact(ctx, ids[0])
			if err != nil {
				return "", err
			}
			StampComputedFields(ctx, svc, art)
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
					StaleNeighbors  []StaleNeighbor         `json:"stale_neighbors,omitempty"`
				}
				staleN := NeighborStaleness(ctx, svc.Proto.Store(), art)
				data, _ := json.Marshal(artWithEdges{Artifact: art, Edges: edges, CompletionScore: score, StaleNeighbors: staleN})
				return string(data), nil
			}
			score := svc.Proto.CompletionScore(ctx, art)
			out := parchment.RenderMarkdown(art)
			if score > 0 {
				out += fmt.Sprintf("\n\n**Completion Score:** %.0f%%", score*100)
			}
			if hint := FormatStalenessHint(NeighborStaleness(ctx, svc.Proto.Store(), art)); hint != "" {
				out += hint
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
		{sortFieldKind, artifactA.Label(parchment.LabelPrefixKind), artifactB.Label(parchment.LabelPrefixKind)},
		{sortFieldScope, artifactA.Label(parchment.LabelPrefixScope), artifactB.Label(parchment.LabelPrefixScope)},
		{sortFieldStatus, parchment.StatusFromLabels(artifactA.Labels), parchment.StatusFromLabels(artifactB.Labels)},
		{sortFieldTitle, artifactA.Title, artifactB.Title},
		{"parent", parentOf(ctx, svc.Proto.Store(), artifactA.ID), parentOf(ctx, svc.Proto.Store(), artifactB.ID)},
		{sortFieldPriority, artifactA.Label(parchment.LabelPrefixPriority), artifactB.Label(parchment.LabelPrefixPriority)},
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

// BriefingOpts configures the tree traversal for renderWithBriefing.
type BriefingOpts struct {
	Depth     int
	Relation  string
	Direction string
}

// renderWithBriefing renders search results with an ArtifactTree chain attached to each.
func renderWithBriefing(ctx context.Context, svc *Service, arts []*parchment.Artifact, opts BriefingOpts) string {
	depth, relation, direction := opts.Depth, opts.Relation, opts.Direction
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
	children, _ := svc.Proto.Children(ctx, id)
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
			case parchment.RelDependsOn:
				dependents = append(dependents, fmt.Sprintf("  %s [%s] %s", e.Target.ID, parchment.StatusFromLabels(e.Target.Labels), e.Target.Title))
			case parchment.RelImplements:
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

func getContext(ctx context.Context, svc *Service, id string) (string, error) {
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	kind := art.Label(parchment.LabelPrefixKind)
	status := parchment.StatusFromLabels(art.Labels)
	fmt.Fprintf(&b, "# Context for %s\n**%s** [%s|%s]\n\n", id, art.Title, kind, status)

	writeHeritage(ctx, svc, &b, id)
	writeChildren(ctx, svc, &b, id)
	writeDependencies(ctx, svc, &b, id)
	writeReferences(ctx, svc, &b, id)
	writeMetrics(ctx, svc, &b, id, art)

	return b.String(), nil
}

func writeHeritage(ctx context.Context, svc *Service, b *strings.Builder, id string) {
	parents, _ := svc.Proto.Neighbors(ctx, id, parchment.RelParentOf, parchment.Incoming)
	if len(parents) == 0 {
		return
	}
	fmt.Fprintf(b, "## Heritage\n")
	for _, e := range parents {
		p, err := svc.Proto.GetArtifact(ctx, e.From)
		if err != nil {
			continue
		}
		fmt.Fprintf(b, "  %s [%s] %s\n", p.ID, p.Label(parchment.LabelPrefixKind), p.Title)
		grandparents, _ := svc.Proto.Neighbors(ctx, e.From, parchment.RelParentOf, parchment.Incoming)
		for _, gp := range grandparents {
			g, err := svc.Proto.GetArtifact(ctx, gp.From)
			if err != nil {
				continue
			}
			fmt.Fprintf(b, "    %s [%s] %s\n", g.ID, g.Label(parchment.LabelPrefixKind), g.Title)
		}
	}
	b.WriteString("\n")
}

func writeChildren(ctx context.Context, svc *Service, b *strings.Builder, id string) {
	children, _ := svc.Proto.Children(ctx, id)
	if len(children) == 0 {
		return
	}
	fmt.Fprintf(b, "## Children (%d)\n", len(children))
	for _, ch := range children {
		fmt.Fprintf(b, "  %s [%s|%s] %s\n", ch.ID, ch.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(ch.Labels), ch.Title)
	}
	b.WriteString("\n")
}

func writeDependencies(ctx context.Context, svc *Service, b *strings.Builder, id string) {
	deps, _ := svc.Proto.Neighbors(ctx, id, parchment.RelDependsOn, parchment.Outgoing)
	if len(deps) == 0 {
		return
	}
	fmt.Fprintf(b, "## Depends on (%d)\n", len(deps))
	for _, e := range deps {
		d, err := svc.Proto.GetArtifact(ctx, e.To)
		if err != nil {
			continue
		}
		fmt.Fprintf(b, "  %s [%s|%s] %s\n", d.ID, d.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(d.Labels), d.Title)
	}
	b.WriteString("\n")
}

func writeReferences(ctx context.Context, svc *Service, b *strings.Builder, id string) {
	knowledgeRels := []string{parchment.RelCites, parchment.RelElaborates, parchment.RelDocuments, parchment.RelImplements, parchment.RelJustifies}
	var lines []string
	seen := map[string]bool{}
	for _, rel := range knowledgeRels {
		edges, _ := svc.Proto.Neighbors(ctx, id, rel, parchment.Outgoing)
		for _, e := range edges {
			if seen[e.To] {
				continue
			}
			seen[e.To] = true
			ref, err := svc.Proto.GetArtifact(ctx, e.To)
			if err != nil {
				continue
			}
			lines = append(lines, fmt.Sprintf("  %s ──%s──▶ %s [%s] %s", id, rel, ref.ID, ref.Label(parchment.LabelPrefixKind), ref.Title))
		}
	}
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "## References (%d)\n", len(lines))
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n")
}

func writeMetrics(ctx context.Context, svc *Service, b *strings.Builder, id string, art *parchment.Artifact) {
	fanIn, _ := FanIn(ctx, svc.Proto.Store(), id)
	fanOut, _ := FanOut(ctx, svc.Proto.Store(), id)
	m := ComputeProgress(ctx, svc, art)
	fmt.Fprintf(b, "## Metrics\n  Fan-in: %d  Fan-out: %d\n  Content: %.0f%%  Delivery: %.0f%%  Verified: %.0f%%\n",
		fanIn, fanOut, m.ContentCompleteness*100, m.DeliveryProgress*100, m.VerifiedProgress*100)
	if d, via := ResolveCanonicalDecision(ctx, svc, id); d != nil {
		fmt.Fprintf(b, "## Canonical architecture (%s)\n  %s [%s|%s] %s\n",
			via, d.ID, d.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(d.Labels), d.Title)
	}
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
