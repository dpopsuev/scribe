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
	sortFieldTitle    = "title"
	sortFieldStatus   = "status"
	sortFieldScope    = "scope"
	sortFieldKind     = "kind"
	sortFieldSprint   = "sprint"
	sortFieldPriority = "priority"
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
		mode := in.Mode
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
				return renderWithBriefing(ctx, svc, arts, BriefingOpts{Depth: in.Depth, Relation: in.Relation, Direction: in.Direction}), nil
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
			Labels:   listLabels, LabelsOr: in.LabelsOr, ExcludeLabels: listExclude,
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
					case sortFieldKind:
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
			return renderWithBriefing(ctx, svc, arts, BriefingOpts{Depth: in.Depth, Relation: in.Relation, Direction: in.Direction}), nil
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
