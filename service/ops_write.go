package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

const (
	sectionKeyText    = "text"
	sectionKeyBody    = "body"
	sectionKeyContent = "content"
)

type createInput struct {
	Kind      string              `json:"kind,omitempty"`
	Title     string              `json:"title,omitempty"`
	Scope     string              `json:"scope,omitempty"`
	Goal      string              `json:"goal,omitempty"`
	Parent    string              `json:"parent,omitempty"`
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
	CloneFrom string              `json:"clone_from,omitempty"`
	Artifacts []map[string]any    `json:"artifacts,omitempty"`
}

var opCreate = Op{
	Name: "create",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) { //nolint:cyclop // routing: clone|batch|single — each path is simple
		var in createInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
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
		name := firstNonEmpty(sec, "name", "slug", "title")
		if name != "" {
			text, _ := sectionText(sec)
			out = append(out, parchment.Section{Name: name, Text: text})
		}
	}
	return out
}

func sectionText(sec map[string]string) (string, bool) {
	for _, key := range []string{sectionKeyText, sectionKeyBody, sectionKeyContent} {
		if v, ok := sec[key]; ok {
			return v, true
		}
	}
	return "", false
}

func firstNonEmpty(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
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
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "created %s [%s|%s] %s", art.ID, art.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(art.Labels), art.Title)
	if parentOf(ctx, svc.Proto.Store(), art.ID) != "" {
		fmt.Fprintf(&b, " (parent: %s)", parentOf(ctx, svc.Proto.Store(), art.ID))
	}
	kind := art.Label(parchment.LabelPrefixKind)
	if len(art.Sections) > 0 {
		names := make([]string, len(art.Sections))
		for i, s := range art.Sections {
			names[i] = s.Name
		}
		fmt.Fprintf(&b, "\nStored sections: %s", strings.Join(names, ", "))
	}
	must := svc.Proto.MustSections(kind)
	should := svc.Proto.ShouldSections(kind)
	var missing []string
	have := make(map[string]bool, len(art.Sections))
	for _, s := range art.Sections {
		have[s.Name] = true
	}
	for _, s := range must {
		if !have[s] {
			missing = append(missing, s+" (must)")
		}
	}
	for _, s := range should {
		if !have[s] {
			missing = append(missing, s+" (should)")
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(&b, "\nMissing sections: %s", strings.Join(missing, ", "))
	}
	return b.String(), nil
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
	baseLabels := stripSystemLabels(source.Labels, in.Labels, in.Scope)
	sections := copySections(source.Sections)
	cloneLabels := buildCloneLabels(svc, kind, in.Status, in.Priority, baseLabels)
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
		ci, err := unmarshalBatchItem(rawArt, i)
		if err != nil {
			return "", err
		}
		if err := resolveRefs(&ci, idRefs, i); err != nil {
			return "", err
		}
		art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Title:     ci.Title,
			Goal:      ci.Goal,
			Parent:    ci.Parent,
			DependsOn: ci.DependsOn,
			Labels:    buildBatchLabels(&ci),
			Links:     ci.Links,
			Extra:     ci.Extra,
			Sections:  parseSections(ci.Sections),
		})
		if err != nil {
			return "", fmt.Errorf("artifact[%d] %q: %w", i, ci.Title, err)
		}
		idRefs[fmt.Sprintf("$%d", i)] = art.ID
		writeBatchLine(ctx, svc, &b, art)
	}
	return b.String(), nil
}

func unmarshalBatchItem(raw any, index int) (createInput, error) {
	data, _ := json.Marshal(raw)
	var ci createInput
	if err := json.Unmarshal(data, &ci); err != nil {
		return ci, fmt.Errorf("artifact[%d]: %w", index, err)
	}
	return ci, nil
}

func resolveRefs(ci *createInput, refs map[string]string, index int) error {
	if ci.Parent != "" && ci.Parent[0] == '$' {
		resolved, ok := refs[ci.Parent]
		if !ok {
			return fmt.Errorf("artifact[%d]: unresolved parent reference %q", index, ci.Parent) //nolint:err113 // dynamic context
		}
		ci.Parent = resolved
	}
	for i, dep := range ci.DependsOn {
		if dep != "" && dep[0] == '$' {
			resolved, ok := refs[dep]
			if !ok {
				return fmt.Errorf("artifact[%d]: unresolved depends_on reference %q", index, dep) //nolint:err113 // dynamic context
			}
			ci.DependsOn[i] = resolved
		}
	}
	for rel, targets := range ci.Links {
		for i, tid := range targets {
			if tid != "" && tid[0] == '$' {
				resolved, ok := refs[tid]
				if !ok {
					return fmt.Errorf("artifact[%d]: unresolved links[%s] reference %q", index, rel, tid) //nolint:err113 // dynamic context
				}
				ci.Links[rel][i] = resolved
			}
		}
	}
	return nil
}

func buildBatchLabels(ci *createInput) []string {
	labels := ci.Labels
	for _, pair := range []struct{ prefix, value string }{
		{parchment.LabelPrefixKind, ci.Kind},
		{parchment.LabelPrefixStatus, ci.Status},
		{parchment.LabelPrefixScope, ci.Scope},
		{parchment.LabelPrefixPriority, ci.Priority},
	} {
		if pair.value != "" {
			labels = append(labels, pair.prefix+pair.value)
		}
	}
	return labels
}

func writeBatchLine(ctx context.Context, svc *Service, b *strings.Builder, art *parchment.Artifact) {
	fmt.Fprintf(b, "%s [%s] %s", art.ID, art.Label(parchment.LabelPrefixKind), art.Title)
	if p := parentOf(ctx, svc.Proto.Store(), art.ID); p != "" {
		fmt.Fprintf(b, " (parent: %s)", p)
	}
	b.WriteString("\n")
}

type updateInput struct {
	ID             string              `json:"id"`
	IDs            []string            `json:"ids,omitempty"`
	Artifacts      []json.RawMessage   `json:"artifacts,omitempty"`
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
	Extra          map[string]any      `json:"extra,omitempty"`
	Query          string              `json:"query,omitempty"`
	Text           string              `json:"text,omitempty"`
	Body           string              `json:"body,omitempty"`
	Force          bool                `json:"force,omitempty"`
}

var opUpdate = Op{
	Name: "update",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) { //nolint:cyclop // multi-path: fields+sections+find-replace+sections_delete+batch
		var in updateInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if len(in.Artifacts) > 0 {
			return updateBatch(ctx, svc, in.Artifacts)
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
		if len(fieldMap) == 0 && len(in.Sections) == 0 && !hasSectionReplace && len(in.SectionsDelete) == 0 && len(in.Extra) == 0 {
			return "", fmt.Errorf("update requires at least one field, section, sections_delete, extra, or query+text for find-replace") //nolint:err113 // user-facing hint
		}
		ops := []updateOp{
			{active: len(fieldMap) > 0, run: func(ctx context.Context, svc *Service, id string) []string {
				return updateFields(ctx, svc, id, fieldMap, in.Force)
			}},
			{active: len(in.Sections) > 0, run: func(ctx context.Context, svc *Service, id string) []string {
				return updateSections(ctx, svc, id, in.Sections)
			}},
			{active: hasSectionReplace, run: func(ctx context.Context, svc *Service, id string) []string {
				replacement := in.Text
				if replacement == "" {
					replacement = in.Body
				}
				lines, err := findReplaceInSections(ctx, svc, id, in.Query, replacement)
				if err != nil {
					return []string{fmt.Sprintf("%s -> error: %v", id, err)}
				}
				return lines
			}},
			{active: len(in.SectionsDelete) > 0, run: func(ctx context.Context, svc *Service, id string) []string {
				return deleteSections(ctx, svc, id, in.SectionsDelete)
			}},
			{active: len(in.Extra) > 0, run: func(ctx context.Context, svc *Service, id string) []string {
				return patchExtra(ctx, svc, id, in.Extra)
			}},
		}
		var lines []string
		for _, id := range ids {
			for _, op := range ops {
				if op.active {
					lines = append(lines, op.run(ctx, svc, id)...)
				}
			}
		}
		return strings.Join(lines, "\n"), nil
	},
}

type updateOp struct {
	active bool
	run    func(ctx context.Context, svc *Service, id string) []string
}

func findReplaceInSections(ctx context.Context, svc *Service, id, query, replacement string) ([]string, error) {
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return nil, err
	}
	var lines []string
	updated := 0
	for _, sec := range art.Sections {
		if !strings.Contains(sec.Text, query) {
			continue
		}
		newText := strings.ReplaceAll(sec.Text, query, replacement)
		if _, err := svc.Proto.AttachSection(ctx, id, sec.Name, newText); err != nil {
			lines = append(lines, fmt.Sprintf("%s -> error: section %q: %v", id, sec.Name, err))
			continue
		}
		updated++
	}
	lines = append(lines, fmt.Sprintf("%s: %d section(s) updated", id, updated))
	return lines, nil
}

func updateFields(ctx context.Context, svc *Service, id string, fieldMap map[string]string, force bool) []string {
	var lines []string
	for field, value := range fieldMap {
		results, err := svc.Proto.SetField(ctx, []string{id}, field, value, parchment.SetFieldOptions{Force: force})
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s -> error: set %s: %v", id, field, err))
			continue
		}
		if r := results[0]; !r.OK {
			lines = append(lines, fmt.Sprintf("%s -> error: set %s: %s", id, field, r.Error))
		} else {
			lines = append(lines, fmt.Sprintf("%s.%s = %s", id, field, value))
		}
	}
	return lines
}

func updateSections(ctx context.Context, svc *Service, id string, sections []map[string]string) []string {
	var lines []string
	for _, sec := range sections {
		name := firstNonEmpty(sec, "name", "slug", "title")
		if name == "" {
			continue
		}
		t, ok := sectionText(sec)
		if !ok {
			lines = append(lines, fmt.Sprintf("%s -> error: section %q: missing text, body, or content", id, name))
			continue
		}
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
	return lines
}

func deleteSections(ctx context.Context, svc *Service, id string, names []string) []string {
	var lines []string
	for _, name := range names {
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
	return lines
}

func patchExtra(ctx context.Context, svc *Service, id string, extra map[string]any) []string {
	if len(extra) == 0 {
		return nil
	}
	if err := svc.Proto.PatchArtifact(ctx, id, parchment.ArtifactPatch{SetExtra: extra}); err != nil {
		return []string{fmt.Sprintf("%s -> error: extra: %v", id, err)}
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	return []string{fmt.Sprintf("%s: extra keys set: %s", id, strings.Join(keys, ", "))}
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
		if in.Field == parchment.FieldStatus && in.Value == statusWorkActive && !in.Force {
			if msg := checkReadLogGuard(ctx, svc, ids); msg != "" {
				return msg, nil
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

func checkReadLogGuard(ctx context.Context, svc *Service, ids []string) string {
	for _, id := range ids {
		art, err := svc.Proto.GetArtifact(ctx, id)
		if err != nil || art.Label(parchment.LabelPrefixKind) != kindTask {
			continue
		}
		implEdges, _ := svc.Proto.Neighbors(ctx, id, parchment.RelImplements, parchment.Outgoing)
		for _, e := range implEdges {
			if !svc.ReadLog[e.To] {
				return fmt.Sprintf("%s -> error: must read %s first (call get on implementing spec before activating)", id, e.To)
			}
		}
	}
	return ""
}

func stripSystemLabels(source, override []string, scope string) []string {
	if len(override) > 0 {
		if scope != "" {
			return append(override, parchment.LabelPrefixScope+scope)
		}
		return override
	}
	strip := []string{parchment.LabelPrefixKind, parchment.LabelPrefixStatus}
	if scope != "" {
		strip = append(strip, parchment.LabelPrefixScope)
	}
	var out []string
	for _, l := range source {
		keep := true
		for _, prefix := range strip {
			if strings.HasPrefix(l, prefix) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, l)
		}
	}
	if scope != "" {
		out = append(out, parchment.LabelPrefixScope+scope)
	}
	return out
}

func copySections(src []parchment.Section) []parchment.Section {
	out := make([]parchment.Section, len(src))
	copy(out, src)
	return out
}

func buildCloneLabels(svc *Service, kind, status, priority string, base []string) []string {
	labels := make([]string, 0, len(base)+3)
	if kind != "" {
		labels = append(labels, parchment.LabelPrefixKind+kind)
	}
	if status == "" {
		status = svc.Proto.DefaultStatus(kind)
	}
	if parchment.IsDomainStatusLabel(status) {
		labels = append(labels, status)
	} else {
		labels = append(labels, parchment.LabelPrefixStatus+status)
	}
	labels = append(labels, base...)
	if priority != "" {
		labels = append(labels, parchment.LabelPrefixPriority+priority)
	}
	return labels
}

func updateBatch(ctx context.Context, svc *Service, items []json.RawMessage) (string, error) {
	var updated, failed int
	var lines []string
	for _, raw := range items {
		var item updateInput
		if err := json.Unmarshal(raw, &item); err != nil {
			failed++
			lines = append(lines, fmt.Sprintf("parse error: %v", err))
			continue
		}
		if item.ID == "" {
			failed++
			lines = append(lines, "missing id")
			continue
		}
		fieldMap := map[string]string{}
		for k, v := range item.Patch {
			fieldMap[k] = v
		}
		for field, value := range map[string]string{
			"status": item.Status, "title": item.Title, "goal": item.Goal,
			"scope": item.Scope, "parent": item.Parent, "priority": item.Priority,
			"sprint": item.Sprint, "kind": item.Kind,
		} {
			if value != "" {
				fieldMap[field] = value
			}
		}
		fieldLines := updateFields(ctx, svc, item.ID, fieldMap, item.Force)
		secLines := updateSections(ctx, svc, item.ID, item.Sections)
		errored := false
		for _, l := range fieldLines {
			lines = append(lines, l)
			if strings.Contains(l, "error") {
				errored = true
			}
		}
		for _, l := range secLines {
			lines = append(lines, l)
			if strings.Contains(l, "error") {
				errored = true
			}
		}
		if errored {
			failed++
		} else {
			updated++
		}
	}
	summary := fmt.Sprintf("batch update: %d updated, %d failed", updated, failed)
	if len(lines) > 0 {
		summary += "\n" + strings.Join(lines, "\n")
	}
	return summary, nil
}
