//nolint:goconst,gocognit // mutation action/status literals; batch apply is inherently branched
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	parchment "github.com/dpopsuev/parchment"
)

const (
	mutationModePlan  = "plan"
	mutationModeApply = "apply"
)

var (
	mutationCacheMu sync.Mutex
	mutationCache   = map[string]MutationResult{}
)

type createInputExt struct {
	createInput
	DryRun     bool   `json:"dry_run,omitempty"`
	Mode       string `json:"mode,omitempty"` // plan | apply | ""
	MutationID string `json:"mutation_id,omitempty"`
}

var opCreate = Op{
	Name:       "create",
	Structured: runCreateStructured,
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		r, err := runCreateStructured(ctx, svc, raw)
		return r.Text, err
	},
}

func runCreateStructured(ctx context.Context, svc *Service, raw json.RawMessage) (Result, error) {
	var in createInputExt
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{}, err
	}
	if in.DryRun && in.Mode == "" {
		in.Mode = mutationModePlan
	}
	if in.CloneFrom != "" {
		text, err := createClone(ctx, svc, &in.createInput)
		return TextResult(text), err
	}
	if len(in.Artifacts) > 0 {
		return createBatchStructured(ctx, svc, &in)
	}
	return createSingleStructured(ctx, svc, &in)
}

func validateDependsOnExist(ctx context.Context, svc *Service, deps []string) error {
	for _, dep := range deps {
		if dep == "" || strings.HasPrefix(dep, "$") {
			continue
		}
		if _, err := svc.Proto.GetArtifact(ctx, dep); err != nil {
			return fmt.Errorf("depends_on target %q does not exist — create it first or fix the ID", dep) //nolint:err113 // agent-facing
		}
	}
	return nil
}

func createSingleStructured(ctx context.Context, svc *Service, in *createInputExt) (Result, error) {
	if in.Title == "" {
		return Result{}, fmt.Errorf("title is required") //nolint:err113 // user-facing hint
	}
	if err := validateDependsOnExist(ctx, svc, in.DependsOn); err != nil {
		return Result{}, err
	}
	// Single create uses the same label rules as legacy createSingle.
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
	ref := plannedRef(&in.createInput, "")
	if in.Mode == mutationModePlan {
		mr := MutationResult{
			Action: actionCreate, Status: mutationModePlan, DryRun: true,
			MutationID: in.MutationID, Artifacts: []ArtifactRef{ref}, Count: 1,
			Warnings: sectionWarnings(svc, &in.createInput),
		}
		return Result{Text: formatPlanText(mr), Data: mr}, nil
	}
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title: in.Title, Goal: in.Goal, Parent: in.Parent, ExplicitID: in.ID,
		Labels: labels, DependsOn: in.DependsOn, Sections: parseSections(in.Sections),
		Links: in.Links, Extra: EnrichWriteExtra(in.Extra, ".", true), Patch: in.Patch, SkipHooks: in.SkipHooks,
		CreatedAt: in.CreatedAt,
	})
	if err != nil {
		return Result{}, err
	}
	ref = artifactToRef(art, "")
	mr := MutationResult{
		Action: actionCreate, Status: "applied", MutationID: in.MutationID,
		Artifacts: []ArtifactRef{ref}, IDs: []string{art.ID}, Count: 1,
		Warnings: sectionWarningsFromArt(svc, art),
	}
	return Result{Text: formatCreatedSingle(ctx, svc, art), Data: mr}, nil
}

func createBatchStructured(ctx context.Context, svc *Service, in *createInputExt) (Result, error) {
	if len(in.Artifacts) == 0 {
		return Result{}, fmt.Errorf("artifacts array is required for batch create") //nolint:err113 // user-facing hint
	}
	if in.MutationID != "" {
		if cached, ok := loadMutation(in.MutationID); ok {
			cached.Idempotent = true
			return Result{Text: formatApplyText(cached), Data: cached}, nil
		}
	}

	items := make([]createInput, len(in.Artifacts))
	var planned []ArtifactRef
	var plannedEdges []EdgeRef
	var warnings []string

	planRefs := make(map[string]string)
	for i, rawArt := range in.Artifacts {
		ci, err := unmarshalBatchItem(rawArt, i)
		if err != nil {
			return Result{}, err
		}
		temp := fmt.Sprintf("$%d", i)
		planRefs[temp] = temp
		// Validate $N refs resolve to earlier items without mutating stored parents yet.
		check := ci
		if err := resolveRefs(&check, planRefs, i); err != nil {
			return Result{}, err
		}
		items[i] = ci
		planned = append(planned, plannedRef(&ci, temp))
		warnings = append(warnings, sectionWarnings(svc, &ci)...)
		plannedEdges = append(plannedEdges, collectPlannedEdges(temp, &ci)...)
	}

	if in.Mode != mutationModePlan {
		for i := range items {
			for _, dep := range items[i].DependsOn {
				if strings.HasPrefix(dep, "$") {
					continue
				}
				if err := validateDependsOnExist(ctx, svc, []string{dep}); err != nil {
					return Result{}, fmt.Errorf("artifact[%d]: %w", i, err)
				}
			}
		}
	}

	if in.Mode == mutationModePlan {
		// Resolve edge endpoints to temp refs for the preview.
		mr := MutationResult{
			Action: actionCreate, Status: mutationModePlan, DryRun: true,
			MutationID: in.MutationID, Artifacts: planned, Edges: plannedEdges,
			Warnings: warnings, Count: len(planned),
		}
		return Result{Text: formatPlanText(mr), Data: mr}, nil
	}

	// Apply: create sequentially; on failure delete prior IDs (compensating txn).
	idRefs := make(map[string]string)
	var created []*parchment.Artifact
	var refs []ArtifactRef
	var edges []EdgeRef
	for i := range items {
		ci := items[i]
		if err := resolveRefs(&ci, idRefs, i); err != nil {
			rollbackCreates(ctx, svc, created)
			return Result{}, err
		}
		art, err := svc.Proto.CreateArtifact(ctx, buildCreateInput(&ci))
		if err != nil {
			rollbackCreates(ctx, svc, created)
			mr := MutationResult{
				Action: actionCreate, Status: "error", MutationID: in.MutationID,
				RolledBack: true, Count: 0,
				Warnings: []string{fmt.Sprintf("artifact[%d] %q: %v", i, ci.Title, err)},
			}
			return Result{Text: fmt.Sprintf("create failed (rolled back): %v", err), Data: mr}, err
		}
		temp := fmt.Sprintf("$%d", i)
		idRefs[temp] = art.ID
		created = append(created, art)
		refs = append(refs, artifactToRef(art, temp))
		edges = append(edges, collectAppliedEdges(art.ID, &ci)...)
	}

	mr := MutationResult{
		Action: actionCreate, Status: "applied", MutationID: in.MutationID,
		Artifacts: refs, Edges: edges, Warnings: warnings, Count: len(refs),
	}
	for _, r := range refs {
		mr.IDs = append(mr.IDs, r.ID)
	}
	if in.MutationID != "" {
		storeMutation(in.MutationID, mr)
	}
	return Result{Text: formatApplyText(mr), Data: mr}, nil
}

func rollbackCreates(ctx context.Context, svc *Service, created []*parchment.Artifact) {
	for i := len(created) - 1; i >= 0; i-- {
		_ = svc.Proto.DeleteArtifact(ctx, created[i].ID, true)
	}
}

func buildCreateInput(ci *createInput) parchment.CreateInput {
	return parchment.CreateInput{
		Title:      ci.Title,
		Goal:       ci.Goal,
		Parent:     ci.Parent,
		ExplicitID: ci.ID,
		Labels:     buildBatchLabels(ci),
		DependsOn:  ci.DependsOn,
		Sections:   parseSections(ci.Sections),
		Links:      ci.Links,
		Extra:      EnrichWriteExtra(ci.Extra, ".", true),
		Patch:      ci.Patch,
		SkipHooks:  ci.SkipHooks,
		CreatedAt:  ci.CreatedAt,
	}
}

func plannedRef(ci *createInput, temp string) ArtifactRef {
	id := ci.ID
	if id == "" {
		id = temp
		if id == "" {
			id = "(pending)"
		}
	}
	status := ci.Status
	if status == "" {
		status = "work.draft"
	}
	return ArtifactRef{
		ID: id, Kind: ci.Kind, Status: status, Title: ci.Title,
		Scope: ci.Scope, TempRef: temp, Parent: ci.Parent,
	}
}

func artifactToRef(art *parchment.Artifact, temp string) ArtifactRef {
	return ArtifactRef{
		ID: art.ID, Kind: art.Label(parchment.LabelPrefixKind),
		Status: parchment.StatusFromLabels(art.Labels), Title: art.Title,
		Scope: art.Label(parchment.LabelPrefixScope), TempRef: temp,
	}
}

func collectPlannedEdges(temp string, ci *createInput) []EdgeRef {
	return collectEdges(temp, ci)
}

func collectAppliedEdges(id string, ci *createInput) []EdgeRef {
	return collectEdges(id, ci)
}

func collectEdges(id string, ci *createInput) []EdgeRef {
	var edges []EdgeRef
	if ci.Parent != "" {
		edges = append(edges, EdgeRef{From: ci.Parent, Relation: parchment.RelParentOf, To: id})
	}
	for _, dep := range ci.DependsOn {
		edges = append(edges, EdgeRef{From: id, Relation: parchment.RelDependsOn, To: dep})
	}
	for rel, targets := range ci.Links {
		for _, t := range targets {
			edges = append(edges, EdgeRef{From: id, Relation: rel, To: t})
		}
	}
	return edges
}

func sectionWarnings(svc *Service, ci *createInput) []string {
	if ci.Kind == "" || svc == nil || svc.Proto == nil {
		return nil
	}
	have := map[string]bool{}
	for _, s := range parseSections(ci.Sections) {
		have[s.Name] = true
	}
	var missing []string
	for _, s := range svc.Proto.MustSections(ci.Kind) {
		if !have[s] {
			missing = append(missing, s+" (must)")
		}
	}
	for _, s := range svc.Proto.ShouldSections(ci.Kind) {
		if !have[s] {
			missing = append(missing, s+" (should)")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%q missing sections: %s", ci.Title, strings.Join(missing, ", "))}
}

func sectionWarningsFromArt(svc *Service, art *parchment.Artifact) []string {
	ci := createInput{
		Kind:  art.Label(parchment.LabelPrefixKind),
		Title: art.Title,
	}
	for _, s := range art.Sections {
		ci.Sections = append(ci.Sections, map[string]string{"name": s.Name, "text": s.Text})
	}
	return sectionWarnings(svc, &ci)
}

func formatCreatedSingle(ctx context.Context, svc *Service, art *parchment.Artifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "created %s [%s|%s] %s", art.ID, art.Label(parchment.LabelPrefixKind), parchment.StatusFromLabels(art.Labels), art.Title)
	if p := parentOf(ctx, svc.Proto.Store(), art.ID); p != "" {
		fmt.Fprintf(&b, " (parent: %s)", p)
	}
	if len(art.Sections) > 0 {
		names := make([]string, len(art.Sections))
		for i, s := range art.Sections {
			names[i] = s.Name
		}
		fmt.Fprintf(&b, "\nStored sections: %s", strings.Join(names, ", "))
	}
	return b.String()
}

func formatPlanText(mr MutationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "plan: would create %d artifact(s)", mr.Count)
	if mr.MutationID != "" {
		fmt.Fprintf(&b, " mutation_id=%s", mr.MutationID)
	}
	b.WriteByte('\n')
	for _, a := range mr.Artifacts {
		fmt.Fprintf(&b, "  %s %s [%s] %s\n", a.TempRef, a.ID, a.Kind, a.Title)
	}
	for _, w := range mr.Warnings {
		fmt.Fprintf(&b, "warning: %s\n", w)
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatApplyText(mr MutationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "created %d artifacts:\n", mr.Count)
	for _, a := range mr.Artifacts {
		fmt.Fprintf(&b, "%s [%s] %s\n", a.ID, a.Kind, a.Title)
	}
	if mr.Idempotent {
		b.WriteString("(idempotent replay)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func storeMutation(id string, mr MutationResult) {
	mutationCacheMu.Lock()
	defer mutationCacheMu.Unlock()
	mutationCache[id] = mr
}

func loadMutation(id string) (MutationResult, bool) {
	mutationCacheMu.Lock()
	defer mutationCacheMu.Unlock()
	mr, ok := mutationCache[id]
	return mr, ok
}

const actionCreate = "create"
