package protocol

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dpopsuev/scribe/lifecycle"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/store"
)

var (
	ErrArchived    = errors.New("artifact is archived and read-only")
	ErrNotArchived = errors.New("only archived artifacts can be deleted; use force to override")
)

// Result is a per-ID outcome for batch operations.
type Result struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// TreeNode is a recursive tree representation.
type TreeNode struct {
	ID       string      `json:"id"`
	Status   string      `json:"status"`
	Title    string      `json:"title"`
	Scope    string      `json:"scope,omitempty"`
	Children []*TreeNode `json:"children,omitempty"`
}

// MotdResult is the message-of-the-day payload.
type MotdResult struct {
	Goals        []*model.Artifact `json:"goals,omitempty"`
	DueReminders []*model.Artifact `json:"due_reminders,omitempty"`
	RecentNotes  []*model.Artifact `json:"recent_notes,omitempty"`
}

// Protocol implements all Scribe business logic.
// Both MCP and CLI are thin wrappers around this.
type Protocol struct {
	store  store.Store
	schema *model.Schema
	scopes []string
	vocab  []string
}

// New creates a Protocol with the given store, schema, home scopes, and
// optional vocabulary for kind enforcement.
func New(s store.Store, schema *model.Schema, scopes, vocab []string) *Protocol {
	if schema == nil {
		schema = model.DefaultSchema()
	}
	return &Protocol{store: s, schema: schema, scopes: scopes, vocab: vocab}
}

func (p *Protocol) Schema() *model.Schema { return p.schema }
func (p *Protocol) Store() store.Store    { return p.store }

// --- CRUD ---

type CreateInput struct {
	Kind      string              `json:"kind"`
	Title     string              `json:"title"`
	Scope     string              `json:"scope,omitempty"`
	Goal      string              `json:"goal,omitempty"`
	Parent    string              `json:"parent,omitempty"`
	Status    string              `json:"status,omitempty"`
	DependsOn []string            `json:"depends_on,omitempty"`
	Labels    []string            `json:"labels,omitempty"`
	Prefix    string              `json:"prefix,omitempty"`
	Links     map[string][]string `json:"links,omitempty"`
	Extra     map[string]any      `json:"extra,omitempty"`
}

func (p *Protocol) CreateArtifact(ctx context.Context, in CreateInput) (*model.Artifact, error) {
	if in.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if err := model.ValidateKind(in.Kind, p.vocab); err != nil {
		return nil, err
	}
	scope := in.Scope
	if scope == "" && len(p.scopes) > 0 {
		scope = p.scopes[0]
	}
	prefix := in.Prefix
	if prefix == "" {
		prefix = p.schema.Prefix(in.Kind)
	}
	id, err := p.store.NextID(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("generate ID: %w", err)
	}
	status := in.Status
	if status == "" {
		status = "draft"
	}
	art := &model.Artifact{
		ID: id, Kind: in.Kind, Scope: scope,
		Status: status, Parent: in.Parent,
		Title: in.Title, Goal: in.Goal,
		DependsOn: in.DependsOn, Labels: in.Labels,
		Links: in.Links, Extra: in.Extra,
	}
	if err := p.store.Put(ctx, art); err != nil {
		return nil, err
	}
	return art, nil
}

func (p *Protocol) GetArtifact(ctx context.Context, id string) (*model.Artifact, error) {
	return p.store.Get(ctx, id)
}

func (p *Protocol) DeleteArtifact(ctx context.Context, id string, force bool) error {
	if p.schema.Guards.DeleteRequiresArchived && !force {
		art, err := p.store.Get(ctx, id)
		if err != nil {
			return err
		}
		if art.Status != "archived" {
			return fmt.Errorf("%w: %s (status: %s)", ErrNotArchived, id, art.Status)
		}
	}
	return p.store.Delete(ctx, id)
}

type ListInput struct {
	Kind          string `json:"kind,omitempty"`
	Scope         string `json:"scope,omitempty"`
	Status        string `json:"status,omitempty"`
	Parent        string `json:"parent,omitempty"`
	Sprint        string `json:"sprint,omitempty"`
	IDPrefix      string `json:"id_prefix,omitempty"`
	ExcludeKind   string `json:"exclude_kind,omitempty"`
	ExcludeStatus string `json:"exclude_status,omitempty"`
	GroupBy       string `json:"group_by,omitempty"`
	Sort          string `json:"sort,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	Query         string `json:"query,omitempty"`
}

func (p *Protocol) ListArtifacts(ctx context.Context, in ListInput) ([]*model.Artifact, error) {
	f := model.Filter{
		Kind: in.Kind, Status: in.Status,
		Parent: in.Parent, Sprint: in.Sprint,
		IDPrefix: in.IDPrefix,
		ExcludeKind: in.ExcludeKind,
		ExcludeStatus: in.ExcludeStatus,
	}
	if in.Kind == "" {
		f.ExcludeKinds = p.schema.ExcludedKinds()
	}
	if in.Scope != "" {
		f.Scope = in.Scope
	} else if len(p.scopes) > 0 {
		f.Scopes = p.scopes
	}
	return p.store.List(ctx, f)
}

func (p *Protocol) SearchArtifacts(ctx context.Context, query string, in ListInput) ([]*model.Artifact, error) {
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	f := model.Filter{Kind: in.Kind, Status: in.Status}
	if in.Scope != "" {
		f.Scope = in.Scope
	} else if len(p.scopes) > 0 {
		f.Scopes = p.scopes
	}
	arts, err := p.store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var matched []*model.Artifact
	for _, art := range arts {
		if matchesQuery(art, q) {
			matched = append(matched, art)
		}
	}
	return matched, nil
}

func matchesQuery(art *model.Artifact, q string) bool {
	if strings.Contains(strings.ToLower(art.Title), q) {
		return true
	}
	if strings.Contains(strings.ToLower(art.Goal), q) {
		return true
	}
	for _, sec := range art.Sections {
		if strings.Contains(strings.ToLower(sec.Text), q) {
			return true
		}
	}
	for _, v := range art.Extra {
		if strings.Contains(strings.ToLower(fmt.Sprint(v)), q) {
			return true
		}
	}
	return false
}

// --- SetField (universal mutation) ---

func (p *Protocol) SetField(ctx context.Context, ids []string, field, value string) ([]Result, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one ID is required")
	}
	if field == "" {
		return nil, fmt.Errorf("field is required")
	}

	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		r := p.setFieldSingle(ctx, id, field, value)
		results = append(results, r)
	}
	return results, nil
}

func (p *Protocol) setFieldSingle(ctx context.Context, id, field, value string) Result {
	art, err := p.store.Get(ctx, id)
	if err != nil {
		return Result{ID: id, Error: err.Error()}
	}

	if p.schema.Guards.ArchivedReadonly && art.Status == "archived" {
		return Result{ID: id, Error: fmt.Sprintf("%s: %s", ErrArchived, id)}
	}

	switch field {
	case "title":
		art.Title = value
	case "goal":
		art.Goal = value
	case "scope":
		art.Scope = value
	case "status":
		return p.setStatus(ctx, art, value)
	case "parent":
		art.Parent = value
	case "priority":
		art.Priority = value
	case "sprint":
		art.Sprint = value
	case "kind":
		if err := model.ValidateKind(value, p.vocab); err != nil {
			return Result{ID: id, Error: err.Error()}
		}
		art.Kind = value
	case "depends_on":
		if value == "" {
			art.DependsOn = nil
		} else {
			newDeps := strings.Split(value, ",")
			for i := range newDeps {
				newDeps[i] = strings.TrimSpace(newDeps[i])
			}
			for _, dep := range newDeps {
				if cycle, path := p.wouldCycle(ctx, id, dep); cycle {
					return Result{ID: id, Error: fmt.Sprintf("depends_on cycle detected: %s", strings.Join(path, " → "))}
				}
			}
			art.DependsOn = newDeps
		}
	case "labels":
		if value == "" {
			art.Labels = nil
		} else {
			art.Labels = strings.Split(value, ",")
		}
	default:
		if art.Extra == nil {
			art.Extra = make(map[string]any)
		}
		art.Extra[field] = value
	}

	if err := p.store.Put(ctx, art); err != nil {
		return Result{ID: id, Error: err.Error()}
	}
	return Result{ID: id, OK: true}
}

func (p *Protocol) setStatus(ctx context.Context, art *model.Artifact, status string) Result {
	if p.schema.Guards.CompletionRequiresChildrenComplete && status == "complete" {
		if err := p.guardChildrenComplete(ctx, art); err != nil {
			return Result{ID: art.ID, Error: err.Error()}
		}
	}

	if status == "archived" {
		children, err := p.store.Children(ctx, art.ID)
		if err != nil {
			return Result{ID: art.ID, Error: err.Error()}
		}
		for _, ch := range children {
			if ch.Status != "archived" {
				return Result{ID: art.ID, Error: fmt.Sprintf("cannot archive %s: child %s is %s (use archive_artifact with cascade)", art.ID, ch.ID, ch.Status)}
			}
		}
	}

	if status == "active" && (art.Kind == "task" || art.Kind == "contract") && os.Getenv("SCRIBE_GATE_REQUIRE_COMPONENT_LABELS") == "true" {
		if sec := triggerSection(art); sec != "" && !hasComponentLabels(art.Labels) {
			return Result{
				ID: art.ID,
				Error: fmt.Sprintf("Gate: require_component_labels\n\n  %s has a %q section but no component labels.\n\n  Add labels declaring which components this contract touches:\n    scribe set %s labels \"project:path/to/component, ...\"\n\n  To skip this gate, remove the section or set SCRIBE_GATE_REQUIRE_COMPONENT_LABELS=false.",
					art.ID, sec, art.ID),
			}
		}
	}

	art.Status = status
	if err := p.store.Put(ctx, art); err != nil {
		return Result{ID: art.ID, Error: err.Error()}
	}

	r := Result{ID: art.ID, OK: true}
	var info []string
	if p.schema.Guards.AutoArchiveGoalOnJustifyComplete && status == "complete" {
		if extra := p.autoArchiveGoal(ctx, art); extra != "" {
			info = append(info, extra)
		}
	}
	if p.schema.Guards.AutoCompleteParentOnChildrenTerminal && isTerminal(status) {
		if extra := p.autoCompleteParent(ctx, art); extra != "" {
			info = append(info, extra)
		}
	}
	if p.schema.Guards.AutoActivateNextDraftSprint && art.Kind == "sprint" && status == "complete" {
		if extra := p.autoActivateNextSprint(ctx, art); extra != "" {
			info = append(info, extra)
		}
	}
	if len(info) > 0 {
		r.Error = strings.Join(info, "\n")
	}
	return r
}

func (p *Protocol) guardChildrenComplete(ctx context.Context, art *model.Artifact) error {
	children, err := p.store.Children(ctx, art.ID)
	if err != nil {
		return err
	}
	var incomplete []string
	for _, ch := range children {
		if ch.Status != "complete" {
			incomplete = append(incomplete, fmt.Sprintf("%s [%s]", ch.ID, ch.Status))
		}
	}
	if len(incomplete) > 0 {
		return fmt.Errorf("cannot complete %s: %d incomplete children: %s",
			art.ID, len(incomplete), strings.Join(incomplete, ", "))
	}
	return nil
}

func (p *Protocol) autoArchiveGoal(ctx context.Context, art *model.Artifact) string {
	goalIDs := art.Links[model.RelJustifies]
	if len(goalIDs) == 0 {
		return ""
	}
	var parts []string
	for _, gid := range goalIDs {
		goal, err := p.store.Get(ctx, gid)
		if err != nil || goal.Kind != "goal" || goal.Status != "current" {
			continue
		}
		goal.Status = "archived"
		if err := p.store.Put(ctx, goal); err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("archived %s: %s", goal.ID, goal.Title))
	}
	return strings.Join(parts, "\n")
}

func (p *Protocol) autoCompleteParent(ctx context.Context, art *model.Artifact) string {
	if art.Parent == "" {
		return ""
	}
	parent, err := p.store.Get(ctx, art.Parent)
	if err != nil || isTerminal(parent.Status) {
		return ""
	}
	children, err := p.store.Children(ctx, parent.ID)
	if err != nil || len(children) == 0 {
		return ""
	}
	for _, ch := range children {
		if !isTerminal(ch.Status) {
			return ""
		}
	}
	r := p.setStatus(ctx, parent, "complete")
	if r.OK {
		msg := fmt.Sprintf("auto-completed %s: %s", parent.ID, parent.Title)
		if r.Error != "" {
			msg += "\n" + r.Error
		}
		return msg
	}
	return ""
}

func (p *Protocol) autoActivateNextSprint(ctx context.Context, completed *model.Artifact) string {
	drafts, err := p.store.List(ctx, model.Filter{Kind: "sprint", Status: "draft"})
	if err != nil || len(drafts) == 0 {
		return ""
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].ID < drafts[j].ID })
	next := drafts[0]
	next.Status = "active"
	if err := p.store.Put(ctx, next); err != nil {
		return ""
	}
	return fmt.Sprintf("activated %s: %s", next.ID, next.Title)
}

func isTerminal(status string) bool {
	return status == "complete" || status == "cancelled" || status == "dismissed" || status == "retired" || status == "archived"
}

// --- Sections ---

func (p *Protocol) AttachSection(ctx context.Context, id, name, text string) (bool, error) {
	if id == "" || name == "" {
		return false, fmt.Errorf("id and name are required")
	}
	art, err := p.store.Get(ctx, id)
	if err != nil {
		return false, err
	}
	if p.schema.Guards.ArchivedReadonly && art.Status == "archived" {
		return false, fmt.Errorf("%w: %s", ErrArchived, art.ID)
	}
	replaced := false
	for i, sec := range art.Sections {
		if sec.Name == name {
			art.Sections[i].Text = text
			replaced = true
			break
		}
	}
	if !replaced {
		art.Sections = append(art.Sections, model.Section{Name: name, Text: text})
	}
	if err := p.store.Put(ctx, art); err != nil {
		return false, err
	}
	return replaced, nil
}

func (p *Protocol) GetSection(ctx context.Context, id, name string) (string, error) {
	if id == "" || name == "" {
		return "", fmt.Errorf("id and name are required")
	}
	art, err := p.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	for _, sec := range art.Sections {
		if sec.Name == name {
			return sec.Text, nil
		}
	}
	return "", fmt.Errorf("section %q not found on %s", name, id)
}

// --- Links ---

// wouldCycle returns true if adding a depends_on edge from -> to would
// create a cycle. It walks outgoing depends_on edges from 'to'; if 'from'
// is reachable, the edge would close a loop. Returns the cycle path.
func (p *Protocol) wouldCycle(ctx context.Context, from, to string) (bool, []string) {
	if from == to {
		return true, []string{from, from}
	}
	path := []string{to}
	found := false
	_ = p.store.Walk(ctx, to, model.RelDependsOn, store.Outgoing, 0, func(_ int, e model.Edge) bool {
		path = append(path, e.To)
		if e.To == from {
			found = true
			return false
		}
		return true
	})
	if found {
		return true, append([]string{from}, path...)
	}
	return false, nil
}

func (p *Protocol) LinkArtifacts(ctx context.Context, sourceID, relation string, targetIDs []string) ([]Result, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("source ID is required")
	}
	if relation == "" {
		return nil, fmt.Errorf("relation is required")
	}
	if len(targetIDs) == 0 {
		return nil, fmt.Errorf("at least one target ID is required")
	}
	if !p.validRelation(relation) {
		return nil, fmt.Errorf("unknown relation %q; valid: %s", relation, strings.Join(p.schema.Relations, ", "))
	}

	if relation == model.RelDependsOn {
		for _, tid := range targetIDs {
			if cycle, path := p.wouldCycle(ctx, sourceID, tid); cycle {
				return nil, fmt.Errorf("depends_on cycle detected: %s", strings.Join(path, " → "))
			}
		}
	}

	art, err := p.store.Get(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if art.Links == nil {
		art.Links = make(map[string][]string)
	}
	existing := make(map[string]bool, len(art.Links[relation]))
	for _, id := range art.Links[relation] {
		existing[id] = true
	}
	var results []Result
	for _, tid := range targetIDs {
		if existing[tid] {
			results = append(results, Result{ID: tid, OK: true, Error: "already linked"})
			continue
		}
		if err := p.store.AddEdge(ctx, model.Edge{From: sourceID, To: tid, Relation: relation}); err != nil {
			results = append(results, Result{ID: tid, Error: err.Error()})
			continue
		}
		art.Links[relation] = append(art.Links[relation], tid)
		existing[tid] = true
		results = append(results, Result{ID: tid, OK: true})
	}
	_ = p.store.Put(ctx, art)
	return results, nil
}

func (p *Protocol) UnlinkArtifacts(ctx context.Context, sourceID, relation string, targetIDs []string) ([]Result, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("source ID is required")
	}
	if relation == "" {
		return nil, fmt.Errorf("relation is required")
	}
	if len(targetIDs) == 0 {
		return nil, fmt.Errorf("at least one target ID is required")
	}
	art, err := p.store.Get(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	removeSet := make(map[string]bool, len(targetIDs))
	for _, t := range targetIDs {
		removeSet[t] = true
	}
	var results []Result
	for _, tid := range targetIDs {
		if err := p.store.RemoveEdge(ctx, model.Edge{From: sourceID, To: tid, Relation: relation}); err != nil {
			results = append(results, Result{ID: tid, Error: err.Error()})
			continue
		}
		results = append(results, Result{ID: tid, OK: true})
	}
	var kept []string
	for _, id := range art.Links[relation] {
		if !removeSet[id] {
			kept = append(kept, id)
		}
	}
	if len(kept) > 0 {
		art.Links[relation] = kept
	} else {
		delete(art.Links, relation)
	}
	_ = p.store.Put(ctx, art)
	return results, nil
}

func (p *Protocol) validRelation(rel string) bool {
	for _, r := range p.schema.Relations {
		if r == rel {
			return true
		}
	}
	return false
}

// --- Graph ---

func (p *Protocol) ContractTree(ctx context.Context, id string) (*TreeNode, error) {
	root, err := p.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return p.buildTree(ctx, root), nil
}

func (p *Protocol) buildTree(ctx context.Context, art *model.Artifact) *TreeNode {
	node := &TreeNode{ID: art.ID, Status: art.Status, Title: art.Title, Scope: art.Scope}
	children, _ := p.store.Children(ctx, art.ID)
	for _, ch := range children {
		node.Children = append(node.Children, p.buildTree(ctx, ch))
	}
	return node
}

// --- Composite actions ---

type SetGoalInput struct {
	Title string `json:"title"`
	Scope string `json:"scope,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

type SetGoalResult struct {
	Goal     *model.Artifact   `json:"goal"`
	Root     *model.Artifact   `json:"root"`
	Archived []*model.Artifact `json:"archived,omitempty"`
}

func (p *Protocol) SetGoal(ctx context.Context, in SetGoalInput) (*SetGoalResult, error) {
	if in.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	scope := in.Scope
	if scope == "" && len(p.scopes) > 0 {
		scope = p.scopes[0]
	}

	existing, err := p.store.List(ctx, model.Filter{Kind: "goal", Status: "current", Scope: scope})
	if err != nil {
		return nil, err
	}
	var archived []*model.Artifact
	for _, old := range existing {
		old.Status = "archived"
		if err := p.store.Put(ctx, old); err != nil {
			return nil, fmt.Errorf("archive %s: %w", old.ID, err)
		}
		archived = append(archived, old)
	}

	goalID, err := p.store.NextID(ctx, "GOAL")
	if err != nil {
		return nil, err
	}
	goal := &model.Artifact{
		ID: goalID, Kind: "goal", Scope: scope,
		Status: "current", Title: in.Title,
	}
	if err := p.store.Put(ctx, goal); err != nil {
		return nil, err
	}

	rootKind := in.Kind
	if rootKind == "" {
		rootKind = "goal"
	}
	rootPrefix := p.schema.Prefix(rootKind)
	rootID, err := p.store.NextID(ctx, rootPrefix)
	if err != nil {
		return nil, err
	}
	root := &model.Artifact{
		ID: rootID, Kind: rootKind, Scope: scope,
		Status: "draft", Title: in.Title,
		Links: map[string][]string{model.RelJustifies: {goalID}},
	}
	if err := p.store.Put(ctx, root); err != nil {
		return nil, err
	}
	return &SetGoalResult{Goal: goal, Root: root, Archived: archived}, nil
}

func (p *Protocol) ArchiveArtifact(ctx context.Context, ids []string, cascade bool) ([]Result, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("ids is required")
	}
	var results []Result
	for _, id := range ids {
		if err := lifecycle.Archive(ctx, p.store, id, cascade); err != nil {
			results = append(results, Result{ID: id, Error: err.Error()})
			continue
		}
		results = append(results, Result{ID: id, OK: true})
	}
	return results, nil
}

// BulkMutationInput filters artifacts for bulk operations.
type BulkMutationInput struct {
	Scope       string `json:"scope,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Status      string `json:"status,omitempty"`
	IDPrefix    string `json:"id_prefix,omitempty"`
	ExcludeKind string `json:"exclude_kind,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
}

// BulkMutationResult reports affected artifacts from a bulk operation.
type BulkMutationResult struct {
	AffectedIDs []string `json:"affected_ids"`
	Count       int      `json:"count"`
	DryRun      bool     `json:"dry_run"`
}

// BulkArchive archives all artifacts matching the filter.
func (p *Protocol) BulkArchive(ctx context.Context, in BulkMutationInput) (*BulkMutationResult, error) {
	li := ListInput{
		Scope: in.Scope, Kind: in.Kind, Status: in.Status,
		IDPrefix: in.IDPrefix, ExcludeKind: in.ExcludeKind,
	}
	arts, err := p.ListArtifacts(ctx, li)
	if err != nil {
		return nil, err
	}
	result := &BulkMutationResult{DryRun: in.DryRun}
	for _, art := range arts {
		result.AffectedIDs = append(result.AffectedIDs, art.ID)
	}
	result.Count = len(result.AffectedIDs)
	if in.DryRun {
		return result, nil
	}
	if len(result.AffectedIDs) == 0 {
		return result, nil
	}
	_, err = p.ArchiveArtifact(ctx, result.AffectedIDs, false)
	return result, err
}

// BulkSetField sets a field on all artifacts matching the filter.
func (p *Protocol) BulkSetField(ctx context.Context, in BulkMutationInput, field, value string) (*BulkMutationResult, error) {
	li := ListInput{
		Scope: in.Scope, Kind: in.Kind, Status: in.Status,
		IDPrefix: in.IDPrefix, ExcludeKind: in.ExcludeKind,
	}
	arts, err := p.ListArtifacts(ctx, li)
	if err != nil {
		return nil, err
	}
	result := &BulkMutationResult{DryRun: in.DryRun}
	for _, art := range arts {
		result.AffectedIDs = append(result.AffectedIDs, art.ID)
	}
	result.Count = len(result.AffectedIDs)
	if in.DryRun {
		return result, nil
	}
	if len(result.AffectedIDs) == 0 {
		return result, nil
	}
	_, err = p.SetField(ctx, result.AffectedIDs, field, value)
	return result, err
}

func (p *Protocol) Vacuum(ctx context.Context, days int, scope string, force bool) ([]string, error) {
	if days <= 0 {
		days = 90
	}
	maxAge := time.Duration(days) * 24 * time.Hour
	f := model.Filter{Status: "archived"}
	if scope != "" {
		f.Scope = scope
	}
	arts, err := p.store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().UTC().Add(-maxAge)
	var deleted []string
	for _, art := range arts {
		if !art.UpdatedAt.Before(cutoff) {
			continue
		}
		if !force && p.schema.IsProtected(art.Kind) {
			continue
		}
		if err := p.store.Delete(ctx, art.ID); err != nil {
			return deleted, fmt.Errorf("vacuum %s: %w", art.ID, err)
		}
		deleted = append(deleted, art.ID)
	}
	return deleted, nil
}

func (p *Protocol) Motd(ctx context.Context) (*MotdResult, error) {
	result := &MotdResult{}

	gf := model.Filter{Kind: "goal", Status: "current"}
	if len(p.scopes) > 0 {
		gf.Scopes = p.scopes
	}
	goals, _ := p.store.List(ctx, gf)
	result.Goals = goals

	nf := model.Filter{Kind: "note", Status: "open"}
	if len(p.scopes) > 0 {
		nf.Scopes = p.scopes
	}
	notes, _ := p.store.List(ctx, nf)
	now := time.Now().UTC()
	cutoff := now.Add(-48 * time.Hour)
	for _, n := range notes {
		if isDue(n, now) {
			result.DueReminders = append(result.DueReminders, n)
		} else if n.CreatedAt.After(cutoff) {
			result.RecentNotes = append(result.RecentNotes, n)
		}
	}
	return result, nil
}

// --- Dashboard ---

type DashboardScope struct {
	Scope    string `json:"scope"`
	Total    int    `json:"total"`
	Active   int    `json:"active"`
	Archived int    `json:"archived"`
	Sections int    `json:"sections"`
	Edges    int    `json:"edges"`
	Stale    int    `json:"stale"`
}

type DashboardResult struct {
	Scopes      []DashboardScope  `json:"scopes"`
	DBSizeBytes int64             `json:"db_size_bytes"`
	StaleArts   []*model.Artifact `json:"stale_artifacts,omitempty"`
}

// Dashboard returns a housekeeping dashboard: storage, staleness, scope health.
func (p *Protocol) Dashboard(ctx context.Context, staleDays int) (*DashboardResult, error) {
	if staleDays <= 0 {
		staleDays = 30
	}
	cutoff := time.Now().UTC().Add(-time.Duration(staleDays) * 24 * time.Hour)
	all, err := p.store.List(ctx, model.Filter{})
	if err != nil {
		return nil, err
	}

	scopeMap := map[string]*DashboardScope{}
	var staleArts []*model.Artifact
	for _, art := range all {
		s := art.Scope
		if s == "" {
			s = "(none)"
		}
		ds, ok := scopeMap[s]
		if !ok {
			ds = &DashboardScope{Scope: s}
			scopeMap[s] = ds
		}
		ds.Total++
		if art.Status == "archived" {
			ds.Archived++
		} else if !isTerminal(art.Status) {
			ds.Active++
			if art.UpdatedAt.Before(cutoff) {
				ds.Stale++
				staleArts = append(staleArts, art)
			}
		}
		ds.Sections += len(art.Sections)
		for _, targets := range art.Links {
			ds.Edges += len(targets)
		}
	}

	sort.Slice(staleArts, func(i, j int) bool {
		return staleArts[i].UpdatedAt.Before(staleArts[j].UpdatedAt)
	})
	if len(staleArts) > 10 {
		staleArts = staleArts[:10]
	}

	result := &DashboardResult{StaleArts: staleArts}
	for _, ds := range scopeMap {
		result.Scopes = append(result.Scopes, *ds)
	}
	sort.Slice(result.Scopes, func(i, j int) bool {
		return result.Scopes[i].Total > result.Scopes[j].Total
	})

	if sizer, ok := p.store.(store.DBSizer); ok {
		result.DBSizeBytes, _ = sizer.DBSizeBytes(ctx)
	}
	return result, nil
}

// --- Inventory ---

type InventoryResult struct {
	Total         int               `json:"total"`
	ByKind        map[string]int    `json:"by_kind"`
	ByStatus      map[string]int    `json:"by_status"`
	ActiveSprints []*model.Artifact `json:"active_sprints,omitempty"`
	Goals         []*model.Artifact `json:"goals,omitempty"`
}

func (p *Protocol) Inventory(ctx context.Context) (*InventoryResult, error) {
	all, err := p.store.List(ctx, model.Filter{})
	if err != nil {
		return nil, err
	}
	r := &InventoryResult{
		Total:    len(all),
		ByKind:   make(map[string]int),
		ByStatus: make(map[string]int),
	}
	for _, art := range all {
		r.ByKind[art.Kind]++
		r.ByStatus[art.Status]++
		if art.Kind == "sprint" && art.Status == "active" {
			r.ActiveSprints = append(r.ActiveSprints, art)
		}
		if art.Kind == "goal" && art.Status == "current" {
			r.Goals = append(r.Goals, art)
		}
	}
	return r, nil
}

// --- FS operations ---

func (p *Protocol) DrainDiscover(ctx context.Context, path string) ([]lifecycle.DrainEntry, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	return lifecycle.DrainDiscover(path)
}

func (p *Protocol) DrainCleanup(ctx context.Context, path string) (int, error) {
	if path == "" {
		return 0, fmt.Errorf("path is required")
	}
	entries, err := lifecycle.DrainDiscover(path)
	if err != nil {
		return 0, err
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	return lifecycle.DrainCleanup(paths)
}

// --- Component label gate ---

var triggerSections = map[string]bool{
	"specification": true,
	"feature":       true,
	"bugfix":        true,
	"arch":          true,
}

func triggerSection(art *model.Artifact) string {
	for _, sec := range art.Sections {
		if triggerSections[sec.Name] {
			return sec.Name
		}
	}
	return ""
}

// --- Component labels ---

var componentLabelRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*:.+/.+$`)

func IsComponentLabel(s string) bool {
	return componentLabelRe.MatchString(strings.TrimSpace(s))
}

func hasComponentLabels(labels []string) bool {
	for _, l := range labels {
		if IsComponentLabel(l) {
			return true
		}
	}
	return false
}

func extractComponentLabels(labels []string, projectPrefix string) []string {
	var out []string
	for _, l := range labels {
		l = strings.TrimSpace(l)
		if !IsComponentLabel(l) {
			continue
		}
		if projectPrefix != "" && !strings.HasPrefix(l, projectPrefix+":") {
			continue
		}
		out = append(out, l)
	}
	return out
}

// --- Overlap detection ---

type ArtifactRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type OverlapEntry struct {
	Label     string        `json:"label"`
	Artifacts []ArtifactRef `json:"artifacts"`
}

type OverlapReport struct {
	Overlaps      []OverlapEntry `json:"overlaps"`
	TotalOverlaps int            `json:"total_overlaps"`
	TotalScanned  int            `json:"total_artifacts_scanned"`
}

type OverlapInput struct {
	Kind    string `json:"kind,omitempty"`
	Status  string `json:"status,omitempty"`
	Project string `json:"project,omitempty"`
}

func (p *Protocol) DetectOverlaps(ctx context.Context, in OverlapInput) (*OverlapReport, error) {
	kind := in.Kind
	if kind == "" {
		kind = "task"
	}
	status := in.Status
	if status == "" {
		status = "active"
	}

	f := model.Filter{Kind: kind, Status: status}
	if len(p.scopes) > 0 {
		f.Scopes = p.scopes
	}
	arts, err := p.store.List(ctx, f)
	if err != nil {
		return nil, err
	}

	index := map[string][]ArtifactRef{}
	for _, art := range arts {
		labels := extractComponentLabels(art.Labels, in.Project)
		for _, l := range labels {
			index[l] = append(index[l], ArtifactRef{ID: art.ID, Title: art.Title})
		}
	}

	report := &OverlapReport{TotalScanned: len(arts)}
	for label, refs := range index {
		if len(refs) < 2 {
			continue
		}
		report.Overlaps = append(report.Overlaps, OverlapEntry{Label: label, Artifacts: refs})
	}
	sort.Slice(report.Overlaps, func(i, j int) bool {
		return report.Overlaps[i].Label < report.Overlaps[j].Label
	})
	report.TotalOverlaps = len(report.Overlaps)
	return report, nil
}

// --- Orphan detection ---

// OrphanEntry describes an artifact missing expected relationship links.
type OrphanEntry struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// OrphanReport summarizes tasks without specs/bugs, and specs/bugs without tasks.
type OrphanReport struct {
	Orphans      []OrphanEntry `json:"orphans"`
	TotalOrphans int           `json:"total_orphans"`
	TotalScanned int           `json:"total_scanned"`
}

type OrphanInput struct {
	Scope  string `json:"scope,omitempty"`
	Status string `json:"status,omitempty"`
}

// DetectOrphans finds tasks that don't implement any spec or bug, and
// specs/bugs that no task implements.
func (p *Protocol) DetectOrphans(ctx context.Context, in OrphanInput) (*OrphanReport, error) {
	f := model.Filter{}
	if in.Scope != "" {
		f.Scope = in.Scope
	} else if len(p.scopes) > 0 {
		f.Scopes = p.scopes
	}

	arts, err := p.store.List(ctx, f)
	if err != nil {
		return nil, err
	}

	report := &OrphanReport{}
	for _, art := range arts {
		if in.Status != "" && art.Status != in.Status {
			continue
		}
		if in.Status == "" && isTerminal(art.Status) {
			continue
		}

		switch art.Kind {
		case "task":
			report.TotalScanned++
			edges, err := p.store.Neighbors(ctx, art.ID, model.RelImplements, store.Outgoing)
			if err != nil {
				continue
			}
			if len(edges) == 0 {
				report.Orphans = append(report.Orphans, OrphanEntry{
					ID: art.ID, Kind: art.Kind, Title: art.Title, Status: art.Status,
					Reason: "task has no implements link to a spec or bug",
				})
			}
		case "spec", "bug":
			report.TotalScanned++
			edges, err := p.store.Neighbors(ctx, art.ID, model.RelImplements, store.Incoming)
			if err != nil {
				continue
			}
			if len(edges) == 0 {
				report.Orphans = append(report.Orphans, OrphanEntry{
					ID: art.ID, Kind: art.Kind, Title: art.Title, Status: art.Status,
					Reason: fmt.Sprintf("%s has no task implementing it", art.Kind),
				})
			}
		}
	}

	sort.Slice(report.Orphans, func(i, j int) bool {
		return report.Orphans[i].ID < report.Orphans[j].ID
	})
	report.TotalOrphans = len(report.Orphans)
	return report, nil
}

// --- Vocabulary ---

// VocabList returns the registered kinds. If no vocabulary is configured,
// returns the canonical defaults.
func (p *Protocol) VocabList() []string {
	if len(p.vocab) > 0 {
		out := make([]string, len(p.vocab))
		copy(out, p.vocab)
		sort.Strings(out)
		return out
	}
	var out []string
	for k := range p.schema.Kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// VocabAdd registers a new kind in the protocol's active vocabulary.
func (p *Protocol) VocabAdd(kind string) error {
	if kind == "" {
		return fmt.Errorf("kind is required")
	}
	for _, v := range p.vocab {
		if v == kind {
			return fmt.Errorf("kind %q is already registered", kind)
		}
	}
	p.vocab = append(p.vocab, kind)
	return nil
}

// VocabRemove removes a kind from the vocabulary, only if no artifacts use it.
func (p *Protocol) VocabRemove(ctx context.Context, kind string) error {
	if kind == "" {
		return fmt.Errorf("kind is required")
	}
	found := false
	for _, v := range p.vocab {
		if v == kind {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("kind %q is not registered", kind)
	}
	arts, err := p.store.List(ctx, model.Filter{Kind: kind})
	if err != nil {
		return err
	}
	if len(arts) > 0 {
		return fmt.Errorf("cannot remove kind %q: %d artifact(s) still use it", kind, len(arts))
	}
	var kept []string
	for _, v := range p.vocab {
		if v != kind {
			kept = append(kept, v)
		}
	}
	p.vocab = kept
	return nil
}

// Vocab returns the current vocabulary slice (for persistence by callers).
func (p *Protocol) Vocab() []string { return p.vocab }

// MigrateResult summarizes a vocabulary migration.
type MigrateResult struct {
	Rewrites map[string]int `json:"rewrites"`
	Archived int            `json:"archived"`
	Total    int            `json:"total"`
}

// VocabMigrate rewrites artifact kinds using the canonical absorption table.
// If dryRun is true, counts are returned without mutating.
func (p *Protocol) VocabMigrate(ctx context.Context, dryRun bool) (*MigrateResult, error) {
	all, err := p.store.List(ctx, model.Filter{})
	if err != nil {
		return nil, err
	}
	result := &MigrateResult{Rewrites: make(map[string]int)}
	for _, art := range all {
		if art.Kind == "rule" {
			result.Archived++
			result.Total++
			if !dryRun {
				art.Status = "archived"
				if err := p.store.Put(ctx, art); err != nil {
					return nil, fmt.Errorf("archive %s: %w", art.ID, err)
				}
			}
			continue
		}
		canonical, ok := model.KindAbsorption[art.Kind]
		if !ok {
			continue
		}
		key := art.Kind + " → " + canonical
		result.Rewrites[key]++
		result.Total++
		if !dryRun {
			art.Kind = canonical
			if err := p.store.Put(ctx, art); err != nil {
				return nil, fmt.Errorf("migrate %s: %w", art.ID, err)
			}
		}
	}
	return result, nil
}

// --- helpers ---

func isDue(art *model.Artifact, now time.Time) bool {
	r, ok := art.Extra["remind_at"].(string)
	if !ok {
		return false
	}
	t, err := time.Parse(time.RFC3339, r)
	if err != nil {
		return false
	}
	return !t.After(now)
}

// ParseRemind converts a human duration string (e.g. "3d", "2w", "1m") to a time.
func ParseRemind(s string) (time.Time, error) {
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("too short: %q", s)
	}
	unit := s[len(s)-1]
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil {
		return time.Time{}, fmt.Errorf("parse number: %w", err)
	}
	now := time.Now().UTC()
	switch unit {
	case 'h':
		return now.Add(time.Duration(n) * time.Hour), nil
	case 'd':
		return now.AddDate(0, 0, n), nil
	case 'w':
		return now.AddDate(0, 0, n*7), nil
	case 'm':
		return now.AddDate(0, n, 0), nil
	default:
		return time.Time{}, fmt.Errorf("unknown unit %q (use h/d/w/m)", string(unit))
	}
}
