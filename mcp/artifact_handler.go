package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleArtifact(ctx context.Context, req *sdkmcp.CallToolRequest, in artifactInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocyclo,cyclop,funlen,gocritic // dispatch switch; hugeParam: value semantics intentional
	switch in.Action {
	case "create":
		// create with artifacts[] routes to batch_create semantics.
		if len(in.Artifacts) > 0 {
			return h.handleBatchCreate(ctx, in)
		}
		// Convert MCP sections format to parchment.Section
		var sections []parchment.Section
		for _, sec := range in.Sections {
			name := sec["name"]
			if name == "" {
				name = sec["slug"]
			}
			if name != "" {
				text := sec["text"]
				if text == "" {
					text = sec["body"]
				}
				sections = append(sections, parchment.Section{Name: name, Text: text})
			}
		}

		return h.handleCreate(ctx, req, parchment.CreateInput{
			Kind: in.Kind, Title: in.Title, Scope: in.Scope,
			Goal: in.Goal, Parent: in.Parent, Status: in.Status,
			Priority:  in.Priority,
			DependsOn: in.DependsOn, Labels: in.Labels, Prefix: in.Prefix,
			Links: in.Links, Extra: in.Extra, CreatedAt: in.CreatedAt,
			Sections: sections, Patch: in.Patch, SkipHooks: in.SkipHooks,
		})
	case "batch_create":
		return h.handleBatchCreate(ctx, in)
	case "clone":
		return h.handleClone(ctx, in)
	case "get":
		ids := in.IDs
		if len(ids) == 0 && in.ID != "" {
			ids = []string{in.ID}
		}
		if len(ids) == 0 {
			return nil, nil, fmt.Errorf("id or ids required for get action") //nolint:err113 // agent-facing input validation
		}
		if in.Format == "summary" {
			return h.handleGetSummary(ctx, ids)
		}
		if len(ids) == 1 {
			return h.handleGet(ctx, req, getInput{ID: ids[0], IncludeEdges: in.IncludeEdges, SectionFilter: in.SectionFilter})
		}
		return h.handleBulkGet(ctx, ids, in.SectionFilter)
	case "search":
		// search is an alias for list(query=...) — kept for backward compat, not advertised.
		if in.Query == "" {
			return nil, nil, fmt.Errorf("use list(query=...) to search artifacts") //nolint:err113 // migration hint
		}
		fallthrough
	case "list":
		li := parchment.ListInput{
			Kind: in.Kind, Scope: in.Scope, Status: in.Status,
			Parent: in.Parent, Sprint: in.Sprint, IDPrefix: in.IDPrefix,
			ExcludeKind: in.ExcludeKind, ExcludeStatus: in.ExcludeStatus,
			Labels: in.Labels, LabelsOr: in.LabelsOr, ExcludeLabels: in.ExcludeLabels,
			GroupBy: in.GroupBy, Sort: in.Sort, Limit: in.Limit, Query: in.Query,
			TitleContains: in.TitleContains,
			CreatedAfter:  in.CreatedAfter, CreatedBefore: in.CreatedBefore,
			UpdatedAfter: in.UpdatedAfter, UpdatedBefore: in.UpdatedBefore,
			InsertedAfter: in.InsertedAfter, InsertedBefore: in.InsertedBefore,
		}
		if in.Count {
			return h.handleListCount(ctx, li)
		}
		if in.Top > 0 {
			return h.handleListTop(ctx, li, in.Top)
		}
		if len(in.Fields) > 0 {
			return h.handleListCompact(ctx, li, in.Fields, in.Offset)
		}
		return h.handleList(ctx, req, li, in.Offset)
	case "set":
		ids := in.IDs
		if len(ids) == 0 && in.ID != "" {
			ids = []string{in.ID}
		}
		if len(ids) == 0 {
			return nil, nil, fmt.Errorf("id is required (artifact ID) or ids (array of artifact IDs) for set action") //nolint:err113 // agent-facing hint
		}
		return h.handleBulkSetField(ctx, ids, in.Field, in.Value, in.Force)
	case "update":
		ids := in.IDs
		if len(ids) == 0 && in.ID != "" {
			ids = []string{in.ID}
		}
		if len(ids) == 0 {
			return nil, nil, fmt.Errorf("id is required (artifact ID) or ids (array of artifact IDs) for update action") //nolint:err113 // agent-facing hint
		}
		return h.handleBatchUpdate(ctx, in, ids)
	case "archive":
		archiveIDs := in.IDs
		if len(archiveIDs) == 0 && in.ID != "" {
			archiveIDs = []string{in.ID}
		}
		return h.handleArchive(ctx, req, archiveInput{
			IDs: archiveIDs, Scope: in.Scope,
			Kind: in.Kind, Status: in.Status, IDPrefix: in.IDPrefix,
			ExcludeKind: in.ExcludeKind, DryRun: in.DryRun,
		})
	case "retire":
		retireIDs := resolveIDs(in.IDs, in.ID)
		if len(retireIDs) == 0 {
			return nil, nil, fmt.Errorf("id or ids required for retire") //nolint:err113 // agent-facing input validation
		}
		results, err := h.proto.RetireArtifact(ctx, retireIDs, in.Cascade)
		if err != nil {
			return nil, nil, err
		}
		return text(renderResults(results, "retired", "restored to draft")), nil, nil
	case "de-archive":
		ids := resolveIDs(in.IDs, in.ID)
		if len(ids) == 0 {
			return nil, nil, fmt.Errorf("id or ids required for de-archive") //nolint:err113 // agent-facing input validation
		}
		results, err := h.proto.DeArchive(ctx, ids, in.Cascade)
		if err != nil {
			return nil, nil, err
		}
		return text(renderResults(results, "restored to draft", "")), nil, nil
	case "attach_section":
		if len(in.Sections) > 0 {
			return h.handleBatchAttachSections(ctx, in.ID, in.Sections)
		}
		sectionText := in.Text
		if sectionText == "" {
			sectionText = in.Body
		}
		return h.handleAttachSection(ctx, req, sectionInput{ID: in.ID, Name: in.Name, Text: sectionText})
	case "get_section":
		return h.handleGetSection(ctx, req, getSectionInput{ID: in.ID, Name: in.Name})
	case "detach_section":
		return h.handleDetachSection(ctx, req, getSectionInput{ID: in.ID, Name: in.Name})
	case "list_sections":
		if in.ID == "" {
			return nil, nil, fmt.Errorf("id is required for list_sections") //nolint:err113 // agent-facing input validation
		}
		art, err := h.proto.GetArtifact(ctx, in.ID)
		if err != nil {
			return nil, nil, err
		}
		if len(art.Sections) == 0 {
			return text(fmt.Sprintf("%s has no sections", in.ID)), nil, nil
		}
		names := make([]string, len(art.Sections))
		for i, s := range art.Sections {
			names[i] = s.Name
		}
		return text(strings.Join(names, "\n")), nil, nil
	case "search_sections":
		if in.Query == "" {
			return nil, nil, fmt.Errorf("query is required for search_sections") //nolint:err113 // agent-facing input validation
		}
		arts, err := h.proto.SearchArtifacts(ctx, in.Query, parchment.ListInput{
			Scope: in.Scope, Kind: in.Kind, Status: in.Status,
		})
		if err != nil {
			return nil, nil, err
		}
		if len(arts) == 0 {
			return text("no artifacts match"), nil, nil
		}
		return text(parchment.RenderTable(arts)), nil, nil
	case "bulk_section_update":
		if in.ID == "" {
			return nil, nil, fmt.Errorf("id is required for bulk_section_update") //nolint:err113 // agent-facing input validation
		}
		if in.Query == "" {
			return nil, nil, fmt.Errorf("query (find text) is required for bulk_section_update") //nolint:err113 // agent-facing input validation
		}
		art, err := h.proto.GetArtifact(ctx, in.ID)
		if err != nil {
			return nil, nil, err
		}
		replacement := in.Text
		if replacement == "" {
			replacement = in.Body
		}
		updated := 0
		for _, sec := range art.Sections {
			if strings.Contains(sec.Text, in.Query) {
				newText := strings.ReplaceAll(sec.Text, in.Query, replacement)
				if _, err := h.proto.AttachSection(ctx, in.ID, sec.Name, newText); err != nil {
					return nil, nil, fmt.Errorf("update section %q: %w", sec.Name, err) //nolint:err113 // agent-facing input validation
				}
				updated++
			}
		}
		return text(fmt.Sprintf("bulk_section_update: %d section(s) updated in %s", updated, in.ID)), nil, nil
	case "batch_update":
		if len(in.IDs) == 0 {
			return nil, nil, fmt.Errorf("ids is required for batch_update") //nolint:err113 // agent-facing input validation
		}
		if len(in.Patch) == 0 && in.Field == "" {
			return nil, nil, fmt.Errorf("patch or field+value is required for batch_update") //nolint:err113 // agent-facing input validation
		}
		var results []parchment.Result
		if len(in.Patch) > 0 {
			for field, value := range in.Patch {
				r, err := h.proto.SetField(ctx, in.IDs, field, value, parchment.SetFieldOptions{Force: in.Force})
				if err != nil {
					return nil, nil, err
				}
				results = r
			}
		} else {
			var err error
			results, err = h.proto.SetField(ctx, in.IDs, in.Field, in.Value, parchment.SetFieldOptions{Force: in.Force})
			if err != nil {
				return nil, nil, err
			}
		}
		ok, failed := 0, 0
		for _, r := range results {
			if r.OK {
				ok++
			} else {
				failed++
			}
		}
		if failed > 0 {
			return text(fmt.Sprintf("batch_update: %d updated, %d failed", ok, failed)), nil, nil
		}
		return text(fmt.Sprintf("batch_update: %d updated", ok)), nil, nil
	case "inspect_stash":
		if in.StashID == "" {
			return nil, nil, fmt.Errorf("stash_id is required for inspect_stash") //nolint:err113 // agent-facing hint
		}
		stashed, err := h.proto.Stash().Get(in.StashID)
		if err != nil {
			return nil, nil, fmt.Errorf("stash %s: %w", in.StashID, err) //nolint:err113 // agent-facing input validation
		}
		data, _ := json.Marshal(stashed.Input)
		ttl := 10 * time.Minute
		age := time.Since(stashed.CreatedAt).Round(time.Second)
		return text(fmt.Sprintf("stash %s (age: %v, expires in ~%v):\n%s",
			in.StashID, age, (ttl - age).Round(time.Second), string(data))), nil, nil
	case "promote_stash":
		if in.StashID == "" {
			return nil, nil, fmt.Errorf("stash_id is required for promote_stash") //nolint:err113 // agent-facing input validation
		}
		var sections []parchment.Section
		for _, sec := range in.Sections {
			name := sec["name"]
			if name == "" {
				name = sec["slug"]
			}
			if name != "" {
				text := sec["text"]
				if text == "" {
					text = sec["body"]
				}
				sections = append(sections, parchment.Section{Name: name, Text: text})
			}
		}
		art, err := h.proto.PromoteStash(ctx, in.StashID, parchment.CreateInput{
			Kind: in.Kind, Title: in.Title, Scope: in.Scope,
			Goal: in.Goal, Parent: in.Parent, Status: in.Status,
			Priority: in.Priority, Labels: in.Labels,
			Links: in.Links, Sections: sections, Patch: in.Patch,
		})
		if err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("promoted stash to %s: %s [%s|%s]", art.ID, art.Title, art.Kind, art.Status)), nil, nil
	case "diff":
		if in.ID == "" || in.Against == "" {
			return nil, nil, fmt.Errorf("id and against required for diff") //nolint:err113 // agent-facing input validation
		}
		return h.handleDiff(ctx, in.ID, in.Against)
	case "move":
		if in.ID == "" || in.Target == "" {
			return nil, nil, fmt.Errorf("id and target are required for move — use move(id=<child>, target=<new-parent>)") //nolint:err113 // agent-facing input validation
		}
		return h.handleMove(ctx, in.ID, in.Target)
	case "recall":
		return h.handleRecall(ctx, knowledgeInput{Query: in.Query, Scope: in.Scope})
	case "orient":
		// orient: alias for list(family=knowledge) with overview formatting. Not advertised — use list.
		return h.handleKnowledgeOrient(ctx, knowledgeInput{Scope: in.Scope})
	case "catalog":
		// catalog: alias for list(family=knowledge, group_by=kind). Not advertised — use list.
		return h.handleKnowledgeCatalog(ctx, knowledgeInput{Scope: in.Scope})
	default:
		return nil, nil, fmt.Errorf("unknown artifact action %q (valid: create, batch_create, clone, get, list, recall, set, update, archive, attach_section, get_section, detach_section, diff, promote_stash, inspect_stash)", in.Action) //nolint:err113 // agent-facing hint
	}
}

func (h *handler) handleCreate(ctx context.Context, _ *sdkmcp.CallToolRequest, in parchment.CreateInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional, changing to pointer would require updating all callers
	art, err := h.proto.CreateArtifact(ctx, in)
	if err != nil {
		var ce *parchment.ConformanceError
		if errors.As(err, &ce) {
			return nil, nil, fmt.Errorf("%s [stash_id=%s]", ce.Error(), ce.StashID) //nolint:err113 // structured stash ID
		}
		return nil, nil, err
	}
	// Lean response: id + kind + status + title + hints
	var b strings.Builder
	fmt.Fprintf(&b, "created %s [%s|%s] %s", art.ID, art.Kind, art.Status, art.Title)
	if art.Parent != "" {
		fmt.Fprintf(&b, " (parent: %s)", art.Parent)
	}
	schema := h.proto.Schema()
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
	return text(b.String()), nil, nil
}

type getInput struct {
	ID            string   `json:"id"`
	IncludeEdges  bool     `json:"include_edges,omitempty"`
	SectionFilter []string `json:"section_filter,omitempty"`
}

func filterSections(art *parchment.Artifact, filter []string) {
	if len(filter) == 0 {
		return
	}
	keep := make(map[string]bool, len(filter))
	for _, f := range filter {
		keep[f] = true
	}
	filtered := art.Sections[:0]
	for _, s := range art.Sections {
		if keep[s.Name] {
			filtered = append(filtered, s)
		}
	}
	art.Sections = filtered
}

func (h *handler) handleGet(ctx context.Context, _ *sdkmcp.CallToolRequest, in getInput) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	h.readLog[in.ID] = true
	filterSections(art, in.SectionFilter)

	score := h.proto.CompletionScore(ctx, art)

	if !in.IncludeEdges {
		md := parchment.RenderMarkdown(art)
		if score > 0 {
			md += fmt.Sprintf("\n\n**Completion Score:** %.0f%%", score*100)
		}
		return text(md), nil, nil
	}
	edges, err := h.proto.GetArtifactEdges(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	type artWithEdges struct {
		*parchment.Artifact
		Edges           []parchment.EdgeSummary `json:"edges"`
		CompletionScore float64                 `json:"completion_score"`
	}
	data, _ := json.Marshal(artWithEdges{Artifact: art, Edges: edges, CompletionScore: score})
	return text(string(data)), nil, nil
}

func (h *handler) handleDiff(ctx context.Context, idA, idB string) (*sdkmcp.CallToolResult, any, error) {
	a, err := h.proto.GetArtifact(ctx, idA)
	if err != nil {
		return nil, nil, err
	}
	b, err := h.proto.GetArtifact(ctx, idB)
	if err != nil {
		return nil, nil, err
	}

	var lines []string
	// Field diffs
	fields := []struct{ name, va, vb string }{
		{"kind", a.Kind, b.Kind},
		{"scope", a.Scope, b.Scope},
		{"status", a.Status, b.Status},
		{"title", a.Title, b.Title},
		{"parent", a.Parent, b.Parent},
		{"priority", a.Priority, b.Priority},
	}
	for _, f := range fields {
		if f.va != f.vb {
			lines = append(lines, fmt.Sprintf("  %s: %q → %q", f.name, f.va, f.vb))
		}
	}

	// Section diffs
	secA := make(map[string]string, len(a.Sections))
	for _, s := range a.Sections {
		secA[s.Name] = s.Text
	}
	secB := make(map[string]string, len(b.Sections))
	for _, s := range b.Sections {
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
		return text(fmt.Sprintf("no differences between %s and %s", idA, idB)), nil, nil
	}
	header := fmt.Sprintf("diff %s vs %s:\n", idA, idB)
	return text(header + strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleBulkGet(ctx context.Context, ids, sectionFilter []string) (*sdkmcp.CallToolResult, any, error) {
	arts := make([]*parchment.Artifact, 0, len(ids))
	for _, id := range ids {
		art, err := h.proto.GetArtifact(ctx, id)
		if err != nil {
			return nil, nil, fmt.Errorf("get %s: %w", id, err) //nolint:err113 // agent-facing input validation
		}
		filterSections(art, sectionFilter)
		arts = append(arts, art)
	}
	data, _ := json.Marshal(arts)
	return text(string(data)), nil, nil
}

func (h *handler) handleList(ctx context.Context, _ *sdkmcp.CallToolRequest, in parchment.ListInput, offset ...int) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional, changing to pointer would require updating all callers
	var arts []*parchment.Artifact
	var err error
	if in.Query != "" {
		arts, err = h.proto.SearchArtifacts(ctx, in.Query, in)
	} else {
		arts, err = h.proto.ListArtifacts(ctx, in)
	}
	if err != nil {
		return nil, nil, err
	}
	if in.Query != "" && len(arts) == 0 {
		return text(fmt.Sprintf("no artifacts matching %q", in.Query)), nil, nil
	}
	if in.Sort != "" {
		sortArtifacts(arts, in.Sort)
	}
	total := len(arts)
	off := 0
	if len(offset) > 0 {
		off = offset[0]
	}
	if off > 0 && off < len(arts) {
		arts = arts[off:]
	}
	if in.Limit > 0 && in.Limit < len(arts) {
		arts = arts[:in.Limit]
	}
	if in.GroupBy == "scope_label" {
		scopeLabels := make(map[string][]string)
		infos, err := h.proto.ListScopeInfo(ctx)
		if err == nil {
			for _, info := range infos {
				if len(info.Labels) > 0 {
					scopeLabels[info.Scope] = info.Labels
				}
			}
		}
		return text(parchment.RenderGroupedTableByScopeLabel(arts, scopeLabels)), nil, nil
	}
	if in.GroupBy != "" {
		return text(parchment.RenderGroupedTable(arts, in.GroupBy)), nil, nil
	}
	out := parchment.RenderTable(arts)
	if off > 0 || (in.Limit > 0 && in.Limit < total) {
		out += fmt.Sprintf("\n(showing %d of %d total)", len(arts), total)
	}
	// Truncation hint: warn when no filter was applied so agents don't treat a
	// partial dump as the complete set and burn context on unfiltered responses.
	isUnfiltered := in.Kind == "" && in.Scope == "" && in.Status == "" &&
		in.Query == "" && in.TitleContains == "" && len(in.Labels) == 0 &&
		len(in.LabelsOr) == 0 && in.Parent == "" && in.IDPrefix == "" &&
		in.ExcludeKind == "" && in.ExcludeStatus == "" && in.Limit == 0
	if isUnfiltered && total > 0 {
		out += fmt.Sprintf("\n(%d artifacts — use top=10 for relevance ranking or add scope/kind/status filters to narrow)", total)
	}
	return text(out), nil, nil
}

func (h *handler) handleListCount(ctx context.Context, in parchment.ListInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional, changing to pointer would require updating all callers
	var arts []*parchment.Artifact
	var err error
	if in.Query != "" {
		arts, err = h.proto.SearchArtifacts(ctx, in.Query, in)
	} else {
		arts, err = h.proto.ListArtifacts(ctx, in)
	}
	if err != nil {
		return nil, nil, err
	}

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
		return text(string(data)), nil, nil
	}

	return text(fmt.Sprintf("%d", len(arts))), nil, nil
}

func relevanceScore(a *parchment.Artifact) int {
	score := 0
	// Status weight: active > draft > complete > archived
	switch a.Status {
	case "active", "current", "open":
		score += 100
	case "draft":
		score += 50
	case "complete":
		score += 10
	}
	// Priority weight
	switch a.Priority {
	case "critical":
		score += 40
	case "high":
		score += 30
	case "medium":
		score += 20
	case "low":
		score += 10
	}
	// Recency: more recently updated scores higher (days since update)
	if !a.UpdatedAt.IsZero() {
		daysSince := int(time.Since(a.UpdatedAt).Hours() / 24)
		switch {
		case daysSince < 1:
			score += 30
		case daysSince < 7:
			score += 20
		case daysSince < 30:
			score += 10
		}
	}
	return score
}

func (h *handler) handleListTop(ctx context.Context, in parchment.ListInput, top int) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional, changing to pointer would require updating all callers
	var arts []*parchment.Artifact
	var err error
	if in.Query != "" {
		arts, err = h.proto.SearchArtifacts(ctx, in.Query, in)
	} else {
		arts, err = h.proto.ListArtifacts(ctx, in)
	}
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(arts, func(i, j int) bool {
		return relevanceScore(arts[i]) > relevanceScore(arts[j])
	})
	if top < len(arts) {
		arts = arts[:top]
	}
	data, _ := json.Marshal(arts)
	return text(string(data)), nil, nil
}

var validFields = map[string]func(*parchment.Artifact) string{
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

func (h *handler) handleListCompact(ctx context.Context, in parchment.ListInput, fields []string, offset ...int) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional, changing to pointer would require updating all callers
	// Validate fields
	getters := make([]func(*parchment.Artifact) string, 0, len(fields))
	for _, f := range fields {
		g, ok := validFields[f]
		if !ok {
			return nil, nil, fmt.Errorf("unknown field %q (valid: id, kind, scope, status, title, parent, priority, sprint, depends_on, labels)", f) //nolint:err113 // agent-facing hint
		}
		getters = append(getters, g)
	}

	var arts []*parchment.Artifact
	var err error
	if in.Query != "" {
		arts, err = h.proto.SearchArtifacts(ctx, in.Query, in)
	} else {
		arts, err = h.proto.ListArtifacts(ctx, in)
	}
	if err != nil {
		return nil, nil, err
	}
	if in.Sort != "" {
		sortArtifacts(arts, in.Sort)
	}
	total := len(arts)
	off := 0
	if len(offset) > 0 {
		off = offset[0]
	}
	if off > 0 && off < len(arts) {
		arts = arts[off:]
	}
	if in.Limit > 0 && in.Limit < len(arts) {
		arts = arts[:in.Limit]
	}

	var b strings.Builder
	// Header
	for i, f := range fields {
		if i > 0 {
			b.WriteString("\t")
		}
		b.WriteString(strings.ToUpper(f))
	}
	b.WriteString("\n")

	// Rows
	for _, a := range arts {
		for i, g := range getters {
			if i > 0 {
				b.WriteString("\t")
			}
			b.WriteString(g(a))
		}
		b.WriteString("\n")
	}
	if off > 0 || (in.Limit > 0 && in.Limit < total) {
		fmt.Fprintf(&b, "\n(%d of %d artifacts)\n", len(arts), total)
	} else {
		fmt.Fprintf(&b, "\n(%d artifacts)\n", len(arts))
	}
	return text(b.String()), nil, nil
}

func (h *handler) handleBulkSetField(ctx context.Context, ids []string, field, value string, force bool) (*sdkmcp.CallToolResult, any, error) {
	// Activation prerequisite: must read implementing spec before activating task
	if field == parchment.FieldStatus && value == parchment.StatusActive && !force {
		for _, id := range ids {
			art, err := h.proto.GetArtifact(ctx, id)
			if err != nil || art.Kind != parchment.KindTask {
				continue
			}
			if targets, ok := art.Links[parchment.RelImplements]; ok {
				for _, specID := range targets {
					if !h.readLog[specID] {
						return nil, nil, fmt.Errorf("cannot activate %s: must read %s first (call get on implementing spec before activating)", id, specID) //nolint:err113 // agent-facing hint
					}
				}
			}
		}
	}
	results, err := h.proto.SetField(ctx, ids, field, value, parchment.SetFieldOptions{Force: force})
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s.%s = %s", r.ID, field, value))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleBatchAttachSections(ctx context.Context, id string, sections []map[string]string) (*sdkmcp.CallToolResult, any, error) {
	if id == "" {
		return nil, nil, fmt.Errorf("id is required for batch attach_section") //nolint:err113 // agent-facing input validation
	}
	var added, replaced int
	for _, sec := range sections {
		name := sec["name"]
		if name == "" {
			name = sec["slug"]
		}
		if name == "" {
			return nil, nil, fmt.Errorf("%w: each section must have a 'name' or 'slug' field", parchment.ErrMissingRequiredFields) //nolint:err113 // agent-facing input validation
		}
		t := sec["text"]
		if t == "" {
			t = sec["body"]
		}
		wasReplaced, err := h.proto.AttachSection(ctx, id, name, t)
		if err != nil {
			return nil, nil, fmt.Errorf("section %q: %w", name, err) //nolint:err113 // agent-facing input validation
		}
		if wasReplaced {
			replaced++
		} else {
			added++
		}
	}
	return text(fmt.Sprintf("%s: %d sections added, %d replaced", id, added, replaced)), nil, nil
}

func (h *handler) handleBatchUpdate(ctx context.Context, in artifactInput, ids []string) (*sdkmcp.CallToolResult, any, error) { //nolint:gocyclo,gocritic // pre-existing complexity; hugeParam: value semantics intentional
	// Build field map — patch map takes precedence, then individual fields
	fieldMap := map[string]string{}
	for k, v := range in.Patch {
		fieldMap[k] = v
	}
	if in.Status != "" {
		fieldMap["status"] = in.Status
	}
	if in.Title != "" {
		fieldMap["title"] = in.Title
	}
	if in.Goal != "" {
		fieldMap["goal"] = in.Goal
	}
	if in.Scope != "" {
		fieldMap["scope"] = in.Scope
	}
	if in.Parent != "" {
		fieldMap["parent"] = in.Parent
	}
	if in.Priority != "" {
		fieldMap["priority"] = in.Priority
	}
	if in.Sprint != "" {
		fieldMap["sprint"] = in.Sprint
	}
	if in.Kind != "" {
		fieldMap["kind"] = in.Kind
	}

	if len(fieldMap) == 0 && len(in.Sections) == 0 {
		return nil, nil, fmt.Errorf("update requires at least one field or section to change") //nolint:err113 // agent-facing input validation
	}

	var lines []string
	for _, id := range ids {
		for field, value := range fieldMap {
			results, err := h.proto.SetField(ctx, []string{id}, field, value, parchment.SetFieldOptions{Force: in.Force})
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
			replaced, err := h.proto.AttachSection(ctx, id, name, t)
			if err != nil {
				lines = append(lines, fmt.Sprintf("%s -> error: section %q: %v", id, name, err))
				continue
			}
			action := "added"
			if replaced {
				action = "replaced"
			}
			lines = append(lines, fmt.Sprintf("%s: section %q %s", id, name, action))
		}
	}

	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleBatchCreate(ctx context.Context, in artifactInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	rawArtifacts := in.Artifacts
	if len(rawArtifacts) == 0 {
		return nil, nil, fmt.Errorf("artifacts array is required for batch_create") //nolint:err113 // agent-facing input validation
	}

	created := make([]*parchment.Artifact, 0, len(rawArtifacts))
	idRefs := make(map[string]string)

	for i, raw := range rawArtifacts {
		// Re-marshal + unmarshal to leverage existing JSON parsing
		data, _ := json.Marshal(raw)
		var ci artifactInput
		if err := json.Unmarshal(data, &ci); err != nil {
			return nil, nil, fmt.Errorf("artifact[%d]: %w", i, err) //nolint:err113 // agent-facing input validation
		}

		if strings.HasPrefix(ci.Parent, "$") {
			if resolved, ok := idRefs[ci.Parent]; ok {
				ci.Parent = resolved
			} else {
				return nil, nil, fmt.Errorf("artifact[%d]: unresolved parent reference %q", i, ci.Parent) //nolint:err113 // agent-facing input validation
			}
		}

		var sections []parchment.Section
		for _, sec := range ci.Sections {
			if name, ok := sec["name"]; ok {
				sections = append(sections, parchment.Section{Name: name, Text: sec["text"]})
			}
		}

		art, err := h.proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind: ci.Kind, Title: ci.Title, Scope: ci.Scope,
			Goal: ci.Goal, Parent: ci.Parent, Status: ci.Status,
			Priority:  ci.Priority,
			DependsOn: ci.DependsOn, Labels: ci.Labels, Prefix: ci.Prefix,
			Links: ci.Links, Extra: ci.Extra, CreatedAt: ci.CreatedAt,
			Sections: sections,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("artifact[%d] %q: %w", i, ci.Title, err) //nolint:err113 // agent-facing input validation
		}
		created = append(created, art)
		idRefs[fmt.Sprintf("$%d", i)] = art.ID
	}

	var b strings.Builder
	fmt.Fprintf(&b, "created %d artifacts:\n", len(created))
	for _, art := range created {
		fmt.Fprintf(&b, "%s [%s] %s", art.ID, art.Kind, art.Title)
		if art.Parent != "" {
			fmt.Fprintf(&b, " (parent: %s)", art.Parent)
		}
		if art.Priority != "" && art.Priority != "none" {
			fmt.Fprintf(&b, " (%s)", art.Priority)
		}
		b.WriteString("\n")
	}
	return text(b.String()), nil, nil
}

func (h *handler) handleClone(ctx context.Context, in artifactInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if in.ID == "" {
		return nil, nil, fmt.Errorf("id is required for clone (source artifact)") //nolint:err113 // agent-facing hint
	}
	source, err := h.proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("source %s: %w", in.ID, err) //nolint:err113 // agent-facing input validation
	}

	// Apply overrides, default to source values
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
	goal := source.Goal
	if in.Goal != "" {
		goal = in.Goal
	}
	labels := source.Labels
	if len(in.Labels) > 0 {
		labels = in.Labels
	}

	// Copy sections from source
	sections := make([]parchment.Section, 0, len(source.Sections))
	for _, s := range source.Sections {
		sections = append(sections, parchment.Section{Name: s.Name, Text: s.Text})
	}

	// Copy extra from source
	var extra map[string]any
	if len(source.Extra) > 0 {
		extra = make(map[string]any)
		for k, v := range source.Extra {
			extra[k] = v
		}
	}

	art, err := h.proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:     kind,
		Title:    title,
		Scope:    scope,
		Goal:     goal,
		Parent:   in.Parent, // must be explicit, not inherited
		Status:   in.Status, // defaults to draft via protocol
		Priority: in.Priority,
		Labels:   labels,
		Extra:    extra,
		Sections: sections,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("clone: %w", err) //nolint:err113 // agent-facing input validation
	}

	data, _ := json.Marshal(art)
	return text(fmt.Sprintf("cloned %s → %s\n%s", in.ID, art.ID, string(data))), nil, nil
}

type sectionInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Text string `json:"text"`
}

func (h *handler) handleAttachSection(ctx context.Context, _ *sdkmcp.CallToolRequest, in sectionInput) (*sdkmcp.CallToolResult, any, error) {
	replaced, err := h.proto.AttachSection(ctx, in.ID, in.Name, in.Text)
	if err != nil {
		return nil, nil, err
	}
	// Sync [[wikilinks]] on every write so backlinks are cheap table lookups,
	// not full-text scans at query time.
	if in.Text != "" {
		_, _ = h.proto.SyncWikilinks(ctx, in.ID)
	}
	action := "added"
	if replaced {
		action = "replaced"
	}
	return text(fmt.Sprintf("%s: section %q %s (%d bytes)", in.ID, in.Name, action, len(in.Text))), nil, nil
}

type getSectionInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *handler) handleGetSection(ctx context.Context, _ *sdkmcp.CallToolRequest, in getSectionInput) (*sdkmcp.CallToolResult, any, error) {
	t, err := h.proto.GetSection(ctx, in.ID, in.Name)
	if err != nil {
		return nil, nil, err
	}
	return text(t), nil, nil
}

func (h *handler) handleDetachSection(ctx context.Context, _ *sdkmcp.CallToolRequest, in getSectionInput) (*sdkmcp.CallToolResult, any, error) {
	removed, err := h.proto.DetachSection(ctx, in.ID, in.Name)
	if err != nil {
		return nil, nil, err
	}
	if !removed {
		return text(fmt.Sprintf("%s: section %q not found", in.ID, in.Name)), nil, nil
	}
	return text(fmt.Sprintf("%s: section %q removed", in.ID, in.Name)), nil, nil
}

func (h *handler) handleArchive(ctx context.Context, _ *sdkmcp.CallToolRequest, in archiveInput) (*sdkmcp.CallToolResult, any, error) {
	if len(in.IDs) == 0 && (in.Scope != "" || in.Kind != "" || in.Status != "" || in.IDPrefix != "" || in.ExcludeKind != "") {
		bulk := parchment.BulkMutationInput{
			Scope: in.Scope, Kind: in.Kind, Status: in.Status,
			IDPrefix: in.IDPrefix, ExcludeKind: in.ExcludeKind, DryRun: in.DryRun,
		}
		res, err := h.proto.BulkArchive(ctx, bulk)
		if err != nil {
			return nil, nil, err
		}
		if in.DryRun {
			return text(fmt.Sprintf("dry run: would archive %d artifacts: %v", res.Count, res.AffectedIDs)), nil, nil
		}
		return text(fmt.Sprintf("archived %d artifacts", res.Count)), nil, nil
	}
	if in.DryRun {
		return text(fmt.Sprintf("dry run: would archive %d artifact(s): %v", len(in.IDs), in.IDs)), nil, nil
	}
	results, err := h.proto.ArchiveArtifact(ctx, in.IDs, in.DryRun)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s -> archived", r.ID))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleGetSummary(ctx context.Context, ids []string) (*sdkmcp.CallToolResult, any, error) {
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
		art, err := h.proto.GetArtifact(ctx, id)
		if err != nil {
			return nil, nil, fmt.Errorf("get %s: %w", id, err) //nolint:err113 // agent-facing input validation
		}
		results = append(results, summary{
			ID: art.ID, Title: art.Title, Kind: art.Kind, Scope: art.Scope,
			Status: art.Status, Priority: art.Priority, Parent: art.Parent, Sprint: art.Sprint,
		})
	}
	if len(results) == 1 {
		data, _ := json.Marshal(results[0])
		return text(string(data)), nil, nil
	}
	data, _ := json.Marshal(results)
	return text(string(data)), nil, nil
}

// unmarshalInput decodes raw JSON into in, returning a populated error result on failure.
