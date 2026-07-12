package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

const (
	sortFieldTitle     = "title"
	sortFieldStatus    = "status"
	sortFieldScope     = "scope"
	sortFieldKind      = "kind"
	sortFieldSprint    = "sprint"
	sortFieldPriority  = "priority"
	modeWorkingSet     = "working_set"
	severityIndex      = "index"
	severityCritical   = "critical"
	severityPlanning   = "planning"
	kindLabelKnowledge = "kind:knowledge.note"
)

// SortArtifacts sorts a slice of artifacts in-place by the named field.
// Falls back to ID sort for unknown fields.
func SortArtifacts(arts []*parchment.Artifact, field string) {
	sort.Slice(arts, func(i, j int) bool {
		switch field {
		case sortFieldTitle:
			return arts[i].Title < arts[j].Title
		case sortFieldStatus:
			return parchment.StatusFromLabels(arts[i].Labels) < parchment.StatusFromLabels(arts[j].Labels)
		case sortFieldScope:
			return arts[i].Label(parchment.LabelPrefixScope) < arts[j].Label(parchment.LabelPrefixScope)
		case sortFieldKind:
			return arts[i].Label(parchment.LabelPrefixKind) < arts[j].Label(parchment.LabelPrefixKind)
		case sortFieldSprint:
			return arts[i].Label(parchment.LabelPrefixSprint) < arts[j].Label(parchment.LabelPrefixSprint)
		default:
			return arts[i].ID < arts[j].ID
		}
	})
}

type listInput struct {
	ID            string   `json:"id,omitempty"`
	Unblocked     bool     `json:"unblocked,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	Status        string   `json:"status,omitempty"`
	Sprint        string   `json:"sprint,omitempty"`
	IDPrefix      string   `json:"id_prefix,omitempty"`
	ExcludeKind   string   `json:"exclude_kind,omitempty"`
	ExcludeStatus string   `json:"exclude_status,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	LabelsOr      []string `json:"labels_or,omitempty"`
	ExcludeLabels []string `json:"exclude_labels,omitempty"`
	Query         string   `json:"query,omitempty"`
	TitleContains string   `json:"title_contains,omitempty"`
	GroupBy       string   `json:"group_by,omitempty"`
	Sort          string   `json:"sort,omitempty"`
	Limit         int      `json:"limit,omitempty"`
	Cursor        string   `json:"cursor,omitempty"` // pagination cursor from previous list response
	Top           int      `json:"top,omitempty"`
	Count         bool     `json:"count,omitempty"`
	Fields        []string `json:"fields,omitempty"`
	Format        string   `json:"format,omitempty"`
	Ranked        bool     `json:"ranked,omitempty"`

	Mode           string `json:"mode,omitempty"`      // fts (default) | semantic | hybrid | working_set
	Session        string `json:"session,omitempty"`   // shorthand for labels=["session:<value>"]
	LeafOnly       bool   `json:"leaf_only,omitempty"` // topo: return only leaves (no children), excludes parent goals
	Depth          int    `json:"depth,omitempty"`     // if >0, attach ArtifactTree to each result
	Relation       string `json:"relation,omitempty"`  // edge relation to traverse with depth; default "*" (all)
	Direction      string `json:"direction,omitempty"` // inbound | outbound | both (default)
	CreatedAfter   string `json:"created_after,omitempty"`
	CreatedBefore  string `json:"created_before,omitempty"`
	UpdatedAfter   string `json:"updated_after,omitempty"`
	UpdatedBefore  string `json:"updated_before,omitempty"`
	InsertedAfter  string `json:"inserted_after,omitempty"`
	InsertedBefore string `json:"inserted_before,omitempty"`
	ExcerptChars   int    `json:"excerpt_chars,omitempty"`
	IncludeCode    bool   `json:"include_code,omitempty"` // working_set: include index-severity hygiene
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
	"id":              func(a *parchment.Artifact) string { return a.ID },
	sortFieldKind:     func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixKind) },
	sortFieldScope:    func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixScope) },
	sortFieldStatus:   func(a *parchment.Artifact) string { return parchment.StatusFromLabels(a.Labels) },
	sortFieldTitle:    func(a *parchment.Artifact) string { return a.Title },
	sortFieldPriority: func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixPriority) },
	sortFieldSprint:   func(a *parchment.Artifact) string { return a.Label(parchment.LabelPrefixSprint) },
	"depends_on":      func(a *parchment.Artifact) string { return "" },
	"labels":          func(a *parchment.Artifact) string { return strings.Join(a.Labels, ",") },
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

func runTopoQuery(ctx context.Context, svc *Service, in *listInput) (string, error) {
	if in.ID == "" {
		return "", fmt.Errorf("id required for sort=topo") //nolint:err113 // user-facing hint
	}
	entries, err := svc.Proto.TopoSort(ctx, in.ID)
	if err != nil && len(entries) == 0 {
		return "", err
	}
	if in.Unblocked {
		entries = filterUnblocked(ctx, svc, entries, in.Depth)
		if len(entries) == 0 {
			return "no unblocked tasks found", nil
		}
	}
	if in.LeafOnly {
		entries = filterLeaves(ctx, svc, entries)
		if len(entries) == 0 {
			return "no leaf tasks found", nil
		}
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

func filterUnblocked(ctx context.Context, svc *Service, entries []parchment.TopoEntry, depth int) []parchment.TopoEntry {
	limit := depth
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
		depEdges, _ := svc.Proto.Neighbors(ctx, art.ID, parchment.RelDependsOn, parchment.Outgoing)
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
	return ready
}

func filterLeaves(ctx context.Context, svc *Service, entries []parchment.TopoEntry) []parchment.TopoEntry {
	var leaves []parchment.TopoEntry
	for _, e := range entries {
		children, _ := svc.Proto.Neighbors(ctx, e.ID, parchment.RelParentOf, parchment.Outgoing)
		if len(children) == 0 {
			leaves = append(leaves, e)
		}
	}
	return leaves
}

func runSemanticQuery(ctx context.Context, svc *Service, in *listInput) (string, error) {
	if in.Query == "" {
		return "", fmt.Errorf("query required for %s search", in.Mode) //nolint:err113 // user-facing hint
	}
	semLabels := in.Labels
	if in.Kind != "" {
		semLabels = append([]string{parchment.LabelPrefixKind + in.Kind}, semLabels...)
	}
	if in.Scope != "" {
		semLabels = append(semLabels, parchment.LabelPrefixScope+in.Scope)
	}
	li := parchment.ListInput{Limit: in.Limit, Labels: semLabels}
	scored, semErr := svc.Proto.SearchSemantic(ctx, in.Query, li)
	if semErr != nil && in.Mode == "semantic" {
		return "", fmt.Errorf("semantic search requires an embedding backend: %w", semErr)
	}
	if in.Mode == "hybrid" {
		ftsResults, _ := svc.Proto.SearchArtifacts(ctx, in.Query, li)
		scored = ReciprocalRankFusion(scored, ftsResults)
	}
	if semErr != nil {
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
		return renderWithBriefing(ctx, svc, arts, BriefingOpts{Depth: in.Depth, Relation: in.Relation, Direction: in.Direction}), nil
	}
	return renderScoredTable(scored), nil
}

func runRankedQuery(ctx context.Context, svc *Service, in *listInput) (string, error) {
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

var opQuery = Op{
	Name: "query",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in listInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Sort == "topo" {
			return runTopoQuery(ctx, svc, &in)
		}
		if in.Mode == modeWorkingSet {
			return runWorkingSet(ctx, svc, &in)
		}
		if in.Session != "" {
			in.Labels = append([]string{"session:" + in.Session}, in.Labels...)
		}
		if in.Mode == "semantic" || in.Mode == "hybrid" {
			return runSemanticQuery(ctx, svc, &in)
		}
		if in.Ranked {
			return runRankedQuery(ctx, svc, &in)
		}

		listLabels := in.Labels
		var kindPrefix string
		if in.Kind != "" {
			if strings.Contains(in.Kind, ".") {
				listLabels = append([]string{parchment.LabelPrefixKind + in.Kind}, listLabels...)
			} else {
				kindPrefix = in.Kind
			}
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
			IDPrefix:   in.IDPrefix,
			KindPrefix: kindPrefix,
			Labels:     listLabels, LabelsOr: in.LabelsOr, ExcludeLabels: listExclude,
			GroupBy: in.GroupBy, Sort: in.Sort, Limit: in.Limit, Cursor: in.Cursor, Query: in.Query,
			TitleContains: in.TitleContains,
			CreatedAfter:  in.CreatedAfter, CreatedBefore: in.CreatedBefore,
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
			// Synonym expansion: if query matches an alias, prepend that artifact.
			if aliased, aliasErr := svc.Proto.GetArtifact(ctx, li.Query); aliasErr == nil {
				found := false
				for _, a := range arts {
					if a.ID == aliased.ID {
						found = true
						break
					}
				}
				if !found {
					arts = append([]*parchment.Artifact{aliased}, arts...)
				}
			}
		} else {
			arts, err = svc.Proto.ListArtifacts(ctx, li)
		}
		if err != nil {
			return "", err
		}

		if in.Count {
			if in.GroupBy != "" {
				return groupedCount(arts, in.GroupBy)
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
		StampComputedFieldsBatch(ctx, svc, arts)

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
			return renderWithBriefing(ctx, svc, arts, BriefingOpts{Depth: in.Depth, Relation: in.Relation, Direction: in.Direction}), nil
		}

		out := appendExcerpts(parchment.RenderTable(arts), arts, in.ExcerptChars, in.Query)
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

// groupedCount tallies artifacts by the given field and returns JSON.
func groupedCount(arts []*parchment.Artifact, field string) (string, error) {
	groups := make(map[string]int)
	for _, a := range arts {
		key := groupKey(a, field)
		groups[key]++
	}
	data, _ := json.Marshal(groups)
	return string(data), nil
}

func groupKey(a *parchment.Artifact, field string) string {
	var key string
	switch field {
	case sortFieldStatus:
		key = parchment.StatusFromLabels(a.Labels)
	case sortFieldScope:
		key = a.Label(parchment.LabelPrefixScope)
	case sortFieldKind:
		key = a.Label(parchment.LabelPrefixKind)
	case sortFieldSprint:
		key = a.Label(parchment.LabelPrefixSprint)
	default:
		key = "unknown"
	}
	if key == "" {
		key = "(none)"
	}
	return key
}

const rrfK = 60

func ReciprocalRankFusion(semantic []parchment.ScoredArtifact, fts []*parchment.Artifact) []parchment.ScoredArtifact {
	type entry struct {
		art   *parchment.Artifact
		score float64
	}
	merged := make(map[string]*entry)

	for rank, s := range semantic {
		merged[s.Artifact.ID] = &entry{art: s.Artifact, score: 1.0 / float64(rrfK+rank+1)}
	}
	for rank, a := range fts {
		if e, ok := merged[a.ID]; ok {
			e.score += 1.0 / float64(rrfK+rank+1)
		} else {
			merged[a.ID] = &entry{art: a, score: 1.0 / float64(rrfK+rank+1)}
		}
	}

	results := make([]parchment.ScoredArtifact, 0, len(merged))
	for _, e := range merged {
		results = append(results, parchment.ScoredArtifact{Artifact: e.art, Score: float32(e.score)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results
}

func appendExcerpts(table string, arts []*parchment.Artifact, chars int, query string) string {
	if chars <= 0 {
		return table
	}
	var b strings.Builder
	b.WriteString(table)
	terms := strings.Fields(strings.ToLower(query))
	for _, a := range arts {
		excerpt := sectionExcerpt(a, chars, terms)
		if excerpt != "" {
			fmt.Fprintf(&b, "  %s: %s\n", a.ID, excerpt)
		}
	}
	return b.String()
}

func sectionExcerpt(art *parchment.Artifact, chars int, terms []string) string {
	if excerpt := findTermExcerpt(art.Sections, chars, terms); excerpt != "" {
		return excerpt
	}
	return firstSectionPreview(art.Sections, chars)
}

func findTermExcerpt(sections []parchment.Section, chars int, terms []string) string {
	for _, sec := range sections {
		lower := strings.ToLower(sec.Text)
		for _, t := range terms {
			idx := strings.Index(lower, t)
			if idx < 0 {
				continue
			}
			start := max(0, idx-chars/4)
			end := min(len(sec.Text), start+chars)
			excerpt := strings.TrimSpace(sec.Text[start:end])
			if end < len(sec.Text) {
				excerpt += "…"
			}
			return excerpt
		}
	}
	return ""
}

func firstSectionPreview(sections []parchment.Section, chars int) string {
	for _, sec := range sections {
		text := strings.TrimSpace(sec.Text)
		if text == "" {
			continue
		}
		if len(text) > chars {
			return text[:chars] + "…"
		}
		return text
	}
	return ""
}

func parentOf(ctx context.Context, store parchment.Store, id string) string {
	edges, _ := store.Neighbors(ctx, id, parchment.RelParentOf, parchment.Incoming)
	if len(edges) > 0 {
		return edges[0].From
	}
	return ""
}
