package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

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

var opReplace = Op{
	Name: "replace",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in createInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" {
			return "", fmt.Errorf("id is required for replace") //nolint:err113 // user-facing hint
		}
		labels := in.Labels
		if in.Kind != "" {
			labels = append([]string{parchment.LabelPrefixKind + in.Kind}, labels...)
		}
		if in.Status != "" {
			labels = append(labels, statusLabelFor(in.Status))
		}
		if in.Scope != "" {
			labels = append(labels, parchment.LabelPrefixScope+in.Scope)
		}
		if in.Priority != "" {
			labels = append(labels, parchment.LabelPrefixPriority+in.Priority)
		}
		result, err := svc.Proto.UpsertArtifact(ctx, parchment.CreateInput{
			ExplicitID: in.ID,
			Title:      in.Title,
			Goal:       in.Goal,
			Parent:     in.Parent,
			Labels:     labels,
			DependsOn:  in.DependsOn,
			Sections:   parseSections(in.Sections),
			Links:      in.Links,
			Extra:      in.Extra,
		})
		if err != nil {
			return "", err
		}
		art := result.Artifact
		verb := "updated"
		if result.Created {
			verb = "created"
		}
		return fmt.Sprintf("%s %s [%s|%s] %s", verb, art.ID,
			art.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(art.Labels), art.Title), nil
	},
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
	labels := in.Labels
	if in.Kind != "" {
		labels = append([]string{parchment.LabelPrefixKind + in.Kind}, labels...)
	}
	if in.Status != "" {
		labels = append(labels, statusLabelFor(in.Status))
	}
	if in.Scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+in.Scope)
	}
	if in.Priority != "" {
		labels = append(labels, parchment.LabelPrefixPriority+in.Priority)
	}
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: in.Title,
		Goal:  in.Goal, Parent: in.Parent,
		ExplicitID: in.ID,
		Labels:     labels, DependsOn: in.DependsOn, Sections: parseSections(in.Sections),
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
	fmt.Fprintf(&b, "created %s [%s|%s] %s", art.ID, art.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(art.Labels), art.Title)
	if art.Parent != "" {
		fmt.Fprintf(&b, " (parent: %s)", art.Parent)
	}
	schema := svc.Proto.Schema()
	if expected := schema.GetExpectedSections(art.Label(parchment.LabelPrefixKind)); len(expected) > 0 {
		must := schema.GetMustSections(art.Label(parchment.LabelPrefixKind))
		should := schema.GetShouldSections(art.Label(parchment.LabelPrefixKind))
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
	stashLabels := in.Labels
	if in.Kind != "" {
		stashLabels = append([]string{parchment.LabelPrefixKind + in.Kind}, stashLabels...)
	}
	if in.Status != "" {
		stashLabels = append(stashLabels, statusLabelFor(in.Status))
	}
	if in.Scope != "" {
		stashLabels = append(stashLabels, parchment.LabelPrefixScope+in.Scope)
	}
	if in.Priority != "" {
		stashLabels = append(stashLabels, parchment.LabelPrefixPriority+in.Priority)
	}
	art, err := svc.Proto.PromoteStash(ctx, in.StashID, parchment.CreateInput{
		Title: in.Title,
		Goal:  in.Goal, Parent: in.Parent,
		Labels: stashLabels,
		Links:  in.Links, Sections: parseSections(in.Sections), Patch: in.Patch,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("promoted stash to %s: %s [%s|%s]", art.ID, art.Title, art.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(art.Labels)), nil
}

func createClone(ctx context.Context, svc *Service, in *createInput) (string, error) {
	source, err := svc.Proto.GetArtifact(ctx, in.CloneFrom)
	if err != nil {
		return "", fmt.Errorf("source %s: %w", in.CloneFrom, err)
	}
	kind := source.Label(parchment.LabelPrefixKind)
	if in.Kind != "" {
		kind = in.Kind
	}
	title := source.Title
	if in.Title != "" {
		title = in.Title
	}
	// Strip system labels (kind:, status:) from source — clone starts fresh with kind and draft status.
	// If in.Scope overrides, also strip scope: from source labels.
	var baseLabels []string
	for _, l := range source.Labels {
		if !strings.HasPrefix(l, parchment.LabelPrefixKind) && !strings.HasPrefix(l, parchment.LabelPrefixStatus) {
			if in.Scope == "" || !strings.HasPrefix(l, parchment.LabelPrefixScope) {
				baseLabels = append(baseLabels, l)
			}
		}
	}
	if len(in.Labels) > 0 {
		baseLabels = in.Labels
	}
	if in.Scope != "" {
		baseLabels = append(baseLabels, parchment.LabelPrefixScope+in.Scope)
	}
	sections := make([]parchment.Section, 0, len(source.Sections))
	for _, s := range source.Sections {
		sections = append(sections, parchment.Section{Name: s.Name, Text: s.Text})
	}
	cloneLabels := make([]string, 0, len(baseLabels)+2)
	if kind != "" {
		cloneLabels = append(cloneLabels, parchment.LabelPrefixKind+kind)
	}
	cloneStatus := in.Status
	if cloneStatus == "" {
		cloneStatus = "work.draft"
	}
	if parchment.IsDomainStatusLabel(cloneStatus) {
		cloneLabels = append(cloneLabels, cloneStatus)
	} else {
		cloneLabels = append(cloneLabels, parchment.LabelPrefixStatus+cloneStatus)
	}
	cloneLabels = append(cloneLabels, baseLabels...)
	if in.Priority != "" {
		cloneLabels = append(cloneLabels, parchment.LabelPrefixPriority+in.Priority)
	}
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:  title,
		Goal:   source.Goal(),
		Parent: in.Parent,
		Labels: cloneLabels, Sections: sections,
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
		batchLabels := ci.Labels
		if ci.Kind != "" {
			batchLabels = append([]string{parchment.LabelPrefixKind + ci.Kind}, batchLabels...)
		}
		if ci.Status != "" {
			batchLabels = append(batchLabels, parchment.LabelPrefixStatus+ci.Status)
		}
		if ci.Scope != "" {
			batchLabels = append(batchLabels, parchment.LabelPrefixScope+ci.Scope)
		}
		if ci.Priority != "" {
			batchLabels = append(batchLabels, parchment.LabelPrefixPriority+ci.Priority)
		}
		art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Title: ci.Title,
			Goal:  ci.Goal, Parent: ci.Parent,
			Labels: batchLabels,
			Links:  ci.Links, Extra: ci.Extra, Sections: parseSections(ci.Sections),
		})
		if err != nil {
			return "", fmt.Errorf("artifact[%d] %q: %w", i, ci.Title, err)
		}
		idRefs[fmt.Sprintf("$%d", i)] = art.ID
		fmt.Fprintf(&b, "%s [%s] %s", art.ID, art.Label(parchment.LabelPrefixKind), art.Title)
		if art.Parent != "" {
			fmt.Fprintf(&b, " (parent: %s)", art.Parent)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
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
			var bulkLabels, bulkExclude []string
			if in.Kind != "" {
				bulkLabels = append(bulkLabels, parchment.LabelPrefixKind+in.Kind)
			}
			if in.Status != "" {
				bulkLabels = append(bulkLabels, statusLabelFor(in.Status))
			}
			if in.Scope != "" {
				bulkLabels = append(bulkLabels, parchment.LabelPrefixScope+in.Scope)
			}
			if in.ExcludeKind != "" {
				bulkExclude = append(bulkExclude, parchment.LabelPrefixKind+in.ExcludeKind)
			}
			arts, err := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
				IDPrefix: in.IDPrefix, Labels: bulkLabels, ExcludeLabels: bulkExclude,
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
		if in.Field == parchment.FieldStatus && in.Value == "work.active" && !in.Force {
			for _, id := range ids {
				art, err := svc.Proto.GetArtifact(ctx, id)
				if err != nil || art.Label(parchment.LabelPrefixKind) != parchment.KindTask {
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
