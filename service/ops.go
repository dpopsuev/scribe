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
	Registry = append(Registry, opSet, opList, opDetachSection, opDiff)
}

// --- set ---

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

// --- list ---

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
	CreatedAfter   string   `json:"created_after,omitempty"`
	CreatedBefore  string   `json:"created_before,omitempty"`
	UpdatedAfter   string   `json:"updated_after,omitempty"`
	UpdatedBefore  string   `json:"updated_before,omitempty"`
	InsertedAfter  string   `json:"inserted_after,omitempty"`
	InsertedBefore string   `json:"inserted_before,omitempty"`
}

// --- diff ---

type diffInput struct {
	ID      string `json:"id"`
	Against string `json:"against"`
}

var opDiff = Op{
	Name: "diff",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in diffInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" || in.Against == "" {
			return "", fmt.Errorf("id and against required") //nolint:err113 // user-facing hint
		}
		a, err := svc.Proto.GetArtifact(ctx, in.ID)
		if err != nil {
			return "", err
		}
		b, err := svc.Proto.GetArtifact(ctx, in.Against)
		if err != nil {
			return "", err
		}
		var lines []string
		for _, f := range []struct{ name, va, vb string }{
			{"kind", a.Kind, b.Kind},
			{"scope", a.Scope, b.Scope},
			{"status", a.Status, b.Status},
			{"title", a.Title, b.Title},
			{"parent", a.Parent, b.Parent},
			{"priority", a.Priority, b.Priority},
		} {
			if f.va != f.vb {
				lines = append(lines, fmt.Sprintf("  %s: %q → %q", f.name, f.va, f.vb))
			}
		}
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
			return fmt.Sprintf("no diff between %s and %s", in.ID, in.Against), nil
		}
		return fmt.Sprintf("diff %s vs %s:\n%s", in.ID, in.Against, strings.Join(lines, "\n")), nil
	},
}

// --- detach_section ---

type detachSectionInput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var opDetachSection = Op{
	Name: "detach_section",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in detachSectionInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" || in.Name == "" {
			return "", fmt.Errorf("id and name required") //nolint:err113 // user-facing hint
		}
		removed, err := svc.Proto.DetachSection(ctx, in.ID, in.Name)
		if err != nil {
			return "", err
		}
		if !removed {
			return fmt.Sprintf("%s: section %q not found", in.ID, in.Name), nil
		}
		return fmt.Sprintf("%s: section %q removed", in.ID, in.Name), nil
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
