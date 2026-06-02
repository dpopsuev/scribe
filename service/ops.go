package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opSet, opList, opRetire, opDeArchive, opArchive, opUpdate, opOrient, opCatalog)
}

type catalogInput struct {
	Scope string `json:"scope,omitempty"`
}

var opCatalog = Op{
	Name: "catalog",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in catalogInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		result, err := svc.KnowledgeCatalog(ctx, in.Scope)
		if err != nil {
			return "", err
		}
		if result.Total == 0 {
			return "Vault is empty. Start with knowledge(action=capture) or knowledge(action=ingest).", nil
		}
		return result.Text + fmt.Sprintf("Total: %d artifact(s)", result.Total), nil
	},
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
	ID    string   `json:"id"`
	IDs   []string `json:"ids,omitempty"`
	Field string   `json:"field"`
	Value string   `json:"value"`
	Force bool     `json:"force,omitempty"`
}

var opSet = Op{
	Name: "set",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in setInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		ids := in.IDs
		if len(ids) == 0 && in.ID != "" {
			ids = []string{in.ID}
		}
		if len(ids) == 0 {
			return "", fmt.Errorf("id or ids required") //nolint:err113 // user-facing hint
		}
		results, err := svc.Proto.SetField(ctx, ids, in.Field, in.Value, parchment.SetFieldOptions{Force: in.Force})
		if err != nil {
			return "", err
		}
		var lines []string
		for _, r := range results {
			if r.OK {
				lines = append(lines, fmt.Sprintf("%s.%s = %s", r.ID, in.Field, in.Value))
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
	Offset         int      `json:"offset,omitempty"`
	Top            int      `json:"top,omitempty"`
	Count          bool     `json:"count,omitempty"`
	Fields         []string `json:"fields,omitempty"`
	Format         string   `json:"format,omitempty"`
	Ranked         bool     `json:"ranked,omitempty"`
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

func RenderResults(results []parchment.Result, okLabel string) string {
	lines := make([]string, 0, len(results))
	for _, r := range results {
		if r.OK {
			lines = append(lines, r.ID+" -> "+okLabel)
		} else {
			lines = append(lines, r.ID+" -> error: "+r.Error)
		}
	}
	return strings.Join(lines, "\n")
}

type archiveInput struct {
	ID          string   `json:"id"`
	IDs         []string `json:"ids,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Status      string   `json:"status,omitempty"`
	IDPrefix    string   `json:"id_prefix,omitempty"`
	ExcludeKind string   `json:"exclude_kind,omitempty"`
	DryRun      bool     `json:"dry_run,omitempty"`
}

var opArchive = Op{
	Name: "archive",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in archiveInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		ids := resolveIDs(in.IDs, in.ID)
		hasBulkFilter := in.Scope != "" || in.Kind != "" || in.Status != "" || in.IDPrefix != "" || in.ExcludeKind != ""

		if hasBulkFilter && len(ids) == 0 {
			res, err := svc.Proto.BulkArchive(ctx, parchment.BulkMutationInput{
				Scope: in.Scope, Kind: in.Kind, Status: in.Status,
				IDPrefix: in.IDPrefix, ExcludeKind: in.ExcludeKind, DryRun: in.DryRun,
			})
			if err != nil {
				return "", err
			}
			if in.DryRun {
				return fmt.Sprintf("dry run: would archive %d artifact(s): %v", res.Count, res.AffectedIDs), nil
			}
			return fmt.Sprintf("archived %d artifact(s)", res.Count), nil
		}
		if len(ids) == 0 {
			return "", fmt.Errorf("provide id, ids, or filter flags (scope, kind, status)") //nolint:err113 // user-facing hint
		}
		if in.DryRun {
			return fmt.Sprintf("dry run: would archive %d artifact(s): %v", len(ids), ids), nil
		}
		results, err := svc.Proto.ArchiveArtifact(ctx, ids, false)
		if err != nil {
			return "", err
		}
		return RenderResults(results, "archived"), nil
	},
}

type deArchiveInput struct {
	ID      string   `json:"id"`
	IDs     []string `json:"ids,omitempty"`
	Cascade bool     `json:"cascade,omitempty"`
}

var opDeArchive = Op{ //nolint:dupl // same structure as opRetire by design — both are id-cascade-results mutations
	Name: "de-archive",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in deArchiveInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		ids := resolveIDs(in.IDs, in.ID)
		if len(ids) == 0 {
			return "", fmt.Errorf("id or ids required") //nolint:err113 // user-facing hint
		}
		results, err := svc.Proto.DeArchive(ctx, ids, in.Cascade)
		if err != nil {
			return "", err
		}
		return RenderResults(results, "restored to draft"), nil
	},
}

type retireInput struct {
	ID      string   `json:"id"`
	IDs     []string `json:"ids,omitempty"`
	Cascade bool     `json:"cascade,omitempty"`
}

var opRetire = Op{
	Name: "retire",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in retireInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		ids := resolveIDs(in.IDs, in.ID)
		if len(ids) == 0 {
			return "", fmt.Errorf("id or ids required") //nolint:err113 // user-facing hint
		}
		results, err := svc.Proto.RetireArtifact(ctx, ids, in.Cascade)
		if err != nil {
			return "", err
		}
		return RenderResults(results, "retired"), nil
	},
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

func listCompact(arts []*parchment.Artifact, fields []string, offset int, li *parchment.ListInput) (string, error) {
	getters := make([]func(*parchment.Artifact) string, 0, len(fields))
	for _, f := range fields {
		g, ok := listValidFields[f]
		if !ok {
			return "", fmt.Errorf("unknown field %q (valid: id, kind, scope, status, title, parent, priority, sprint, depends_on, labels)", f) //nolint:err113 // agent-facing hint
		}
		getters = append(getters, g)
	}
	total := len(arts)
	if offset > 0 && offset < len(arts) {
		arts = arts[offset:]
	}
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
	if offset > 0 || (li.Limit > 0 && li.Limit < total) {
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
			GroupBy: in.GroupBy, Sort: in.Sort, Limit: in.Limit, Query: in.Query,
			TitleContains: in.TitleContains,
			CreatedAfter:  in.CreatedAfter, CreatedBefore: in.CreatedBefore,
			UpdatedAfter: in.UpdatedAfter, UpdatedBefore: in.UpdatedBefore,
			InsertedAfter: in.InsertedAfter, InsertedBefore: in.InsertedBefore,
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
			return listCompact(arts, in.Fields, in.Offset, &li)
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
		off := in.Offset
		if off > 0 && off < len(arts) {
			arts = arts[off:]
		}
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
		if off > 0 || (li.Limit > 0 && li.Limit < total) {
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
