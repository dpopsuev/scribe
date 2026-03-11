package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/dpopsuev/scribe/directive"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/render"
	"github.com/dpopsuev/scribe/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server exposing Scribe tools over the given store.
// Returns both the server and a directive registry for CLI introspection.
func NewServer(s store.Store, homeScopes, vocab []string, idc protocol.IDConfig, version string) (*sdkmcp.Server, *directive.Registry) {
	srv := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "scribe", Version: version},
		&sdkmcp.ServerOptions{
			Instructions: "Scribe is a work graph for AI agents with native DAG support. " +
				"Use it to create, query, and manage structured artifacts (tasks, specs, goals, bugs, campaigns) " +
				"with parent-child trees, dependency edges, named text sections, and lifecycle status tracking. " +
				"Start with admin motd for context, then artifact list to explore.",
		},
	)
	reg := directive.New()
	h := &handler{
		proto: protocol.New(s, nil, homeScopes, vocab, idc),
	}

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name: "artifact",
		Description: "Create, read, update, and manage work artifacts. " +
			"Actions: create (new artifact), get (by ID), list (filter/search), set (update field), " +
			"archive (mark read-only), attach_section (add text), get_section (read text), detach_section (remove text).",
		Keywords:   []string{"create", "get", "list", "set", "archive", "artifact", "section"},
		Categories: []string{"crud"},
	}, noOut(h.handleArtifact))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name: "graph",
		Description: "Navigate and modify artifact relationships. " +
			"Actions: tree (parent-child hierarchy with optional relation/direction/depth), " +
			"briefing (recursive traversal of ALL edges from any artifact, showing full context chain), " +
			"link (add directed relationship), unlink (remove relationship). " +
			"Supported relations: parent_of, depends_on, justifies, implements, documents.",
		Keywords:   []string{"tree", "briefing", "link", "unlink", "relation", "edge", "graph"},
		Categories: []string{"query", "graph"},
	}, noOut(h.handleGraph))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name: "admin",
		Description: "System administration and monitoring. " +
			"Actions: motd (session context with goals/reminders), dashboard (storage/staleness/health), " +
			"set_goal (set north star), vacuum (delete old archived), detect (find orphans/overlaps), " +
			"lint (validate schema consistency).",
		Keywords:   []string{"motd", "dashboard", "goal", "vacuum", "detect", "orphan"},
		Categories: []string{"lifecycle", "maintenance"},
	}, noOut(h.handleAdmin))

	return srv, reg
}

// ToolRegistry returns a populated directive registry without requiring
// a database connection. Useful for CLI introspection (scribe tools).
func ToolRegistry() *directive.Registry {
	_, reg := NewServer(nil, nil, nil, protocol.IDConfig{}, "dev")
	return reg
}

type handler struct {
	proto *protocol.Protocol
}

// --- consolidated input types ---

type artifactInput struct {
	Action string `json:"action" jsonschema:"required,create | get | list | set | archive | attach_section | get_section | detach_section"`

	ID    string `json:"id,omitempty" jsonschema:"artifact ID (required for get, set, archive, *_section)"`
	Kind  string `json:"kind,omitempty" jsonschema:"artifact kind such as task, spec, bug, goal, campaign"`
	Scope string `json:"scope,omitempty" jsonschema:"owning repository or project scope"`

	Title     string              `json:"title,omitempty" jsonschema:"artifact title (required for create)"`
	Goal      string              `json:"goal,omitempty" jsonschema:"goal statement or description"`
	Parent    string              `json:"parent,omitempty" jsonschema:"parent artifact ID for hierarchy"`
	Status    string              `json:"status,omitempty" jsonschema:"lifecycle status, e.g. draft, active, complete, archived"`
	Priority  string              `json:"priority,omitempty" jsonschema:"priority level, e.g. none, low, medium, high, critical"`
	DependsOn []string            `json:"depends_on,omitempty" jsonschema:"IDs of artifacts this depends on"`
	Labels    []string            `json:"labels,omitempty" jsonschema:"freeform labels for categorization"`
	Prefix    string              `json:"prefix,omitempty" jsonschema:"override ID prefix (default derived from kind)"`
	Links     map[string][]string `json:"links,omitempty" jsonschema:"named link groups, e.g. {\"docs\": [\"url1\"]}"`
	Extra     map[string]any      `json:"extra,omitempty" jsonschema:"arbitrary key-value metadata"`
	CreatedAt string              `json:"created_at,omitempty" jsonschema:"RFC 3339 timestamp to backdate creation"`

	IncludeEdges bool `json:"include_edges,omitempty" jsonschema:"if true, get returns resolved neighbor summaries"`

	Sprint         string `json:"sprint,omitempty" jsonschema:"filter by sprint ID (list)"`
	IDPrefix       string `json:"id_prefix,omitempty" jsonschema:"filter by ID prefix (list, archive)"`
	ExcludeKind    string `json:"exclude_kind,omitempty" jsonschema:"exclude artifacts of this kind (list, archive)"`
	ExcludeStatus  string   `json:"exclude_status,omitempty" jsonschema:"exclude artifacts with this status (list)"`
	LabelsOr       []string `json:"labels_or,omitempty" jsonschema:"at least one label must match - OR semantics (list)"`
	ExcludeLabels  []string `json:"exclude_labels,omitempty" jsonschema:"exclude artifacts matching any of these labels (list)"`
	GroupBy        string   `json:"group_by,omitempty" jsonschema:"group results by field: status, scope, kind, sprint, scope_label (list)"`
	Sort           string `json:"sort,omitempty" jsonschema:"sort results by: id, title, status, scope, kind, sprint (list)"`
	Limit          int    `json:"limit,omitempty" jsonschema:"max results to return (list)"`
	Query          string `json:"query,omitempty" jsonschema:"substring search across title, goal, and section text (list)"`
	CreatedAfter   string `json:"created_after,omitempty" jsonschema:"RFC 3339 lower bound on created_at (list)"`
	CreatedBefore  string `json:"created_before,omitempty" jsonschema:"RFC 3339 upper bound on created_at (list)"`
	UpdatedAfter   string `json:"updated_after,omitempty" jsonschema:"RFC 3339 lower bound on updated_at (list)"`
	UpdatedBefore  string `json:"updated_before,omitempty" jsonschema:"RFC 3339 upper bound on updated_at (list)"`
	InsertedAfter  string `json:"inserted_after,omitempty" jsonschema:"RFC 3339 lower bound on inserted_at (list)"`
	InsertedBefore string `json:"inserted_before,omitempty" jsonschema:"RFC 3339 upper bound on inserted_at (list)"`

	Field string `json:"field,omitempty" jsonschema:"field to update: title, goal, scope, status, parent, priority, sprint, kind, depends_on, labels (set)"`
	Value string `json:"value,omitempty" jsonschema:"new value for the field; comma-separated for depends_on/labels (set)"`
	Force bool   `json:"force,omitempty" jsonschema:"bypass transition validation for status changes (set)"`

	IDs     []string `json:"ids,omitempty" jsonschema:"artifact IDs to archive; if empty uses filter params (archive)"`
	Cascade bool     `json:"cascade,omitempty" jsonschema:"recursively archive child subtrees (archive)"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"preview without applying changes (archive)"`

	Name string `json:"name,omitempty" jsonschema:"section name (attach_section, get_section, detach_section)"`
	Text string `json:"text,omitempty" jsonschema:"section body text (attach_section)"`
}

type graphInput struct {
	Action    string   `json:"action" jsonschema:"required,tree | briefing | link | unlink"`
	ID        string   `json:"id,omitempty" jsonschema:"root artifact ID for tree/briefing, or source artifact for link/unlink"`
	Relation  string   `json:"relation,omitempty" jsonschema:"edge type: parent_of, depends_on, justifies, implements, documents"`
	Direction string   `json:"direction,omitempty" jsonschema:"traversal direction for tree: outbound (default) or inbound"`
	Depth     int      `json:"depth,omitempty" jsonschema:"max traversal depth for tree (0 = unlimited)"`
	Targets   []string `json:"targets,omitempty" jsonschema:"target artifact IDs to link/unlink"`
}

type adminInput struct {
	Action string `json:"action" jsonschema:"required,motd | dashboard | set_goal | vacuum | detect | lint | check | set_scope_labels | list_scope_labels"`

	StaleDays int `json:"stale_days,omitempty" jsonschema:"days without update to consider an artifact stale (dashboard)"`

	Title string `json:"title,omitempty" jsonschema:"goal title (set_goal)"`
	Scope string `json:"scope,omitempty" jsonschema:"scope to target (set_goal, vacuum, detect)"`
	Kind  string `json:"kind,omitempty" jsonschema:"root delivery artifact kind, default goal (set_goal); or kind filter (detect)"`

	Days  int  `json:"days,omitempty" jsonschema:"delete archived artifacts older than this many days (vacuum)"`
	Force bool `json:"force,omitempty" jsonschema:"skip confirmation for destructive vacuum (vacuum)"`

	Check   string `json:"check,omitempty" jsonschema:"orphans (default), overlaps, or all (detect)"`
	Status  string `json:"status,omitempty" jsonschema:"filter by status (detect)"`
	Project string `json:"project,omitempty" jsonschema:"filter by project scope (detect)"`

	Labels []string `json:"labels,omitempty" jsonschema:"freeform labels for categorization (set_scope_labels)"`
}

// --- dispatchers ---

func (h *handler) handleArtifact(ctx context.Context, req *sdkmcp.CallToolRequest, in artifactInput) (*sdkmcp.CallToolResult, any, error) {
	switch in.Action {
	case "create":
		return h.handleCreate(ctx, req, protocol.CreateInput{
			Kind: in.Kind, Title: in.Title, Scope: in.Scope,
			Goal: in.Goal, Parent: in.Parent, Status: in.Status,
			Priority: in.Priority,
			DependsOn: in.DependsOn, Labels: in.Labels, Prefix: in.Prefix,
			Links: in.Links, Extra: in.Extra, CreatedAt: in.CreatedAt,
		})
	case "get":
		return h.handleGet(ctx, req, getInput{ID: in.ID, IncludeEdges: in.IncludeEdges})
	case "list":
		return h.handleList(ctx, req, protocol.ListInput{
			Kind: in.Kind, Scope: in.Scope, Status: in.Status,
			Parent: in.Parent, Sprint: in.Sprint, IDPrefix: in.IDPrefix,
			ExcludeKind: in.ExcludeKind, ExcludeStatus: in.ExcludeStatus,
			Labels: in.Labels, LabelsOr: in.LabelsOr, ExcludeLabels: in.ExcludeLabels,
			GroupBy: in.GroupBy, Sort: in.Sort, Limit: in.Limit, Query: in.Query,
			CreatedAfter: in.CreatedAfter, CreatedBefore: in.CreatedBefore,
			UpdatedAfter: in.UpdatedAfter, UpdatedBefore: in.UpdatedBefore,
			InsertedAfter: in.InsertedAfter, InsertedBefore: in.InsertedBefore,
		})
	case "set":
		return h.handleSetField(ctx, req, setFieldInput{ID: in.ID, Field: in.Field, Value: in.Value, Force: in.Force})
	case "archive":
		return h.handleArchive(ctx, req, archiveInput{
			IDs: in.IDs, Cascade: in.Cascade, Scope: in.Scope,
			Kind: in.Kind, Status: in.Status, IDPrefix: in.IDPrefix,
			ExcludeKind: in.ExcludeKind, DryRun: in.DryRun,
		})
	case "attach_section":
		return h.handleAttachSection(ctx, req, sectionInput{ID: in.ID, Name: in.Name, Text: in.Text})
	case "get_section":
		return h.handleGetSection(ctx, req, getSectionInput{ID: in.ID, Name: in.Name})
	case "detach_section":
		return h.handleDetachSection(ctx, req, getSectionInput{ID: in.ID, Name: in.Name})
	default:
		return nil, nil, fmt.Errorf("unknown artifact action %q (valid: create, get, list, set, archive, attach_section, get_section, detach_section)", in.Action)
	}
}

func (h *handler) handleGraph(ctx context.Context, req *sdkmcp.CallToolRequest, in graphInput) (*sdkmcp.CallToolResult, any, error) {
	switch in.Action {
	case "tree":
		return h.handleTree(ctx, req, protocol.TreeInput{
			ID: in.ID, Relation: in.Relation, Direction: in.Direction, Depth: in.Depth,
		})
	case "link":
		return h.handleLink(ctx, req, linkInput{
			ID: in.ID, Relation: in.Relation, Targets: in.Targets,
		})
	case "briefing":
		return h.handleBriefing(ctx, in.ID)
	case "unlink":
		return h.handleLink(ctx, req, linkInput{
			ID: in.ID, Relation: in.Relation, Targets: in.Targets, Unlink: true,
		})
	default:
		return nil, nil, fmt.Errorf("unknown graph action %q (valid: tree, briefing, link, unlink)", in.Action)
	}
}

func (h *handler) handleAdmin(ctx context.Context, req *sdkmcp.CallToolRequest, in adminInput) (*sdkmcp.CallToolResult, any, error) {
	switch in.Action {
	case "motd":
		return h.handleMotd(ctx, req, struct{}{})
	case "dashboard":
		return h.handleDashboard(ctx, req, dashboardInput{StaleDays: in.StaleDays})
	case "set_goal":
		return h.handleSetGoal(ctx, req, protocol.SetGoalInput{
			Title: in.Title, Scope: in.Scope, Kind: in.Kind,
		})
	case "vacuum":
		return h.handleVacuum(ctx, req, vacuumInput{Days: in.Days, Scope: in.Scope, Force: in.Force})
	case "detect":
		return h.handleDetect(ctx, req, detectInput{
			Check: in.Check, Scope: in.Scope, Status: in.Status,
			Kind: in.Kind, Project: in.Project,
		})
	case "lint":
		return h.handleLint(ctx)
	case "check":
		return h.handleCheck(ctx, in.Scope)
	case "set_scope_labels":
		if in.Scope == "" {
			return nil, nil, fmt.Errorf("scope is required for set_scope_labels")
		}
		if err := h.proto.SetScopeLabels(ctx, in.Scope, in.Labels); err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("scope %q labels set to %v", in.Scope, in.Labels)), nil, nil
	case "list_scope_labels":
		infos, err := h.proto.ListScopeInfo(ctx)
		if err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		for _, info := range infos {
			labels := "(none)"
			if len(info.Labels) > 0 {
				labels = strings.Join(info.Labels, ", ")
			}
			fmt.Fprintf(&b, "%-20s %s → %s\n", info.Scope, info.Key, labels)
		}
		return text(b.String()), nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown admin action %q (valid: motd, dashboard, set_goal, vacuum, detect, lint, check, set_scope_labels, list_scope_labels)", in.Action)
	}
}

// --- handlers (thin wrappers) ---

func (h *handler) handleCreate(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.CreateInput) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.CreateArtifact(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.MarshalIndent(art, "", "  ")
	msg := string(data)
	schema := h.proto.Schema()
	if should := schema.MissingShouldSections(art.Kind, art.Sections); len(should) > 0 {
		msg += fmt.Sprintf("\n\nWarning: missing recommended sections: %s", strings.Join(should, ", "))
	}
	if expected := schema.GetExpectedSections(art.Kind); len(expected) > 0 && len(art.Sections) == 0 {
		msg += fmt.Sprintf("\n\nSections hint: %s (use attach_section to populate)", strings.Join(expected, ", "))
	}
	return text(msg), nil, nil
}

type idInput struct {
	ID string `json:"id"`
}

type getInput struct {
	ID           string `json:"id"`
	IncludeEdges bool   `json:"include_edges,omitempty"`
}

func (h *handler) handleGet(ctx context.Context, _ *sdkmcp.CallToolRequest, in getInput) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if !in.IncludeEdges {
		return text(render.Markdown(art)), nil, nil
	}
	edges, err := h.proto.GetArtifactEdges(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	type artWithEdges struct {
		*model.Artifact
		Edges []protocol.EdgeSummary `json:"edges"`
	}
	data, _ := json.MarshalIndent(artWithEdges{Artifact: art, Edges: edges}, "", "  ")
	return text(string(data)), nil, nil
}

func (h *handler) handleList(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.ListInput) (*sdkmcp.CallToolResult, any, error) {
	var arts []*model.Artifact
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
		return text(render.GroupedTableByScopeLabel(arts, scopeLabels)), nil, nil
	}
	if in.GroupBy != "" {
		return text(render.GroupedTable(arts, in.GroupBy)), nil, nil
	}
	return text(render.Table(arts)), nil, nil
}

type setFieldInput struct {
	ID    string `json:"id"`
	Field string `json:"field"`
	Value string `json:"value"`
	Force bool   `json:"force,omitempty"`
}

func (h *handler) handleSetField(ctx context.Context, _ *sdkmcp.CallToolRequest, in setFieldInput) (*sdkmcp.CallToolResult, any, error) {
	results, err := h.proto.SetField(ctx, []string{in.ID}, in.Field, in.Value, protocol.SetFieldOptions{Force: in.Force})
	if err != nil {
		return nil, nil, err
	}
	r := results[0]
	if !r.OK {
		return nil, nil, fmt.Errorf("%s", r.Error)
	}
	msg := fmt.Sprintf("%s.%s = %s", r.ID, in.Field, in.Value)
	if r.Error != "" {
		msg += "\n" + r.Error
	}
	return text(msg), nil, nil
}

type searchInput struct {
	Query  string `json:"query"`
	Scope  string `json:"scope,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status,omitempty"`
}

func (h *handler) handleSearch(ctx context.Context, _ *sdkmcp.CallToolRequest, in searchInput) (*sdkmcp.CallToolResult, any, error) {
	li := protocol.ListInput{Kind: in.Kind, Scope: in.Scope, Status: in.Status}
	matched, err := h.proto.SearchArtifacts(ctx, in.Query, li)
	if err != nil {
		return nil, nil, err
	}
	if len(matched) == 0 {
		return text(fmt.Sprintf("no artifacts matching %q", in.Query)), nil, nil
	}
	return text(render.Table(matched)), nil, nil
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

func (h *handler) handleTree(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.TreeInput) (*sdkmcp.CallToolResult, any, error) {
	tree, err := h.proto.ArtifactTree(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	showScope := countDistinctScopes(tree) > 1
	var b strings.Builder
	renderTree(tree, "", true, showScope, &b)
	return text(b.String()), nil, nil
}

func (h *handler) handleBriefing(ctx context.Context, id string) (*sdkmcp.CallToolResult, any, error) {
	tree, err := h.proto.ArtifactTree(ctx, protocol.TreeInput{
		ID:        id,
		Relation:  "*",
		Direction: "both",
	})
	if err != nil {
		return nil, nil, err
	}
	showScope := countDistinctScopes(tree) > 1
	var b strings.Builder
	renderBriefing(tree, "", true, showScope, &b)
	return text(b.String()), nil, nil
}

func (h *handler) handleSetGoal(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.SetGoalInput) (*sdkmcp.CallToolResult, any, error) {
	res, err := h.proto.SetGoal(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, a := range res.Archived {
		lines = append(lines, fmt.Sprintf("archived %s: %s", a.ID, a.Title))
	}
	lines = append(lines, fmt.Sprintf("%s [current] %s", res.Goal.ID, res.Goal.Title))
	lines = append(lines, fmt.Sprintf("%s [draft] %s (justifies %s)", res.Root.ID, res.Root.Title, res.Goal.ID))
	return text(strings.Join(lines, "\n")), nil, nil
}

type archiveInput struct {
	IDs         []string `json:"ids"`
	Cascade     bool     `json:"cascade,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Status      string   `json:"status,omitempty"`
	IDPrefix    string   `json:"id_prefix,omitempty"`
	ExcludeKind string   `json:"exclude_kind,omitempty"`
	DryRun      bool     `json:"dry_run,omitempty"`
}

func (h *handler) handleArchive(ctx context.Context, _ *sdkmcp.CallToolRequest, in archiveInput) (*sdkmcp.CallToolResult, any, error) {
	if len(in.IDs) == 0 && (in.Scope != "" || in.Kind != "" || in.Status != "" || in.IDPrefix != "" || in.ExcludeKind != "") {
		bulk := protocol.BulkMutationInput{
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
	results, err := h.proto.ArchiveArtifact(ctx, in.IDs, in.Cascade)
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

func (h *handler) handleBatchArchive(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.BulkMutationInput) (*sdkmcp.CallToolResult, any, error) {
	res, err := h.proto.BulkArchive(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	if in.DryRun {
		return text(fmt.Sprintf("dry run: would archive %d artifacts: %v", res.Count, res.AffectedIDs)), nil, nil
	}
	return text(fmt.Sprintf("archived %d artifacts", res.Count)), nil, nil
}

type vacuumInput struct {
	Days  int    `json:"days,omitempty"`
	Scope string `json:"scope,omitempty"`
	Force bool   `json:"force,omitempty"`
}

func (h *handler) handleVacuum(ctx context.Context, _ *sdkmcp.CallToolRequest, in vacuumInput) (*sdkmcp.CallToolResult, any, error) {
	deleted, err := h.proto.Vacuum(ctx, in.Days, in.Scope, in.Force)
	if err != nil {
		return nil, nil, err
	}
	if len(deleted) == 0 {
		return text("nothing to vacuum"), nil, nil
	}
	var lines []string
	for _, id := range deleted {
		lines = append(lines, fmt.Sprintf("deleted %s", id))
	}
	lines = append(lines, fmt.Sprintf("%d archived artifacts vacuumed", len(deleted)))
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleMotd(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, any, error) {
	m, err := h.proto.Motd(ctx)
	if err != nil {
		return nil, nil, err
	}
	var sections []string
	if len(m.Campaigns) > 0 {
		var lines []string
		for _, c := range m.Campaigns {
			prefix := ""
			if c.Scope != "" {
				prefix = "[" + c.Scope + "] "
			}
			lines = append(lines, fmt.Sprintf("  %s %s%s", c.ID, prefix, c.Title))
		}
		sections = append(sections, "Campaigns:\n"+strings.Join(lines, "\n"))
	}
	if len(m.Goals) > 0 {
		var lines []string
		for _, g := range m.Goals {
			prefix := ""
			if g.Scope != "" {
				prefix = "[" + g.Scope + "] "
			}
			lines = append(lines, fmt.Sprintf("  %s %s%s", g.ID, prefix, g.Title))
		}
		sections = append(sections, "Goal:\n"+strings.Join(lines, "\n"))
	}
	if len(m.Warnings) > 0 {
		var lines []string
		for _, w := range m.Warnings {
			lines = append(lines, "  ⚠ "+w)
		}
		sections = append(sections, "Warnings:\n"+strings.Join(lines, "\n"))
	}
	if len(sections) == 0 {
		return text("nothing to report"), nil, nil
	}
	return text(strings.Join(sections, "\n\n")), nil, nil
}

type drainDiscoverInput struct {
	Path   string `json:"path"`
	Format string `json:"format,omitempty"`
}

func (h *handler) handleDrainDiscover(ctx context.Context, _ *sdkmcp.CallToolRequest, in drainDiscoverInput) (*sdkmcp.CallToolResult, any, error) {
	entries, err := h.proto.DrainDiscover(ctx, in.Path)
	if err != nil {
		return nil, nil, err
	}
	if len(entries) == 0 {
		return text("no .md files found"), nil, nil
	}
	if in.Format == "json" {
		data, _ := json.MarshalIndent(entries, "", "  ")
		return text(string(data)), nil, nil
	}
	var lines []string
	for _, e := range entries {
		lines = append(lines, fmt.Sprintf("%-50s  [dir: %-15s  %d bytes]", e.Path, e.Dir, e.SizeB))
	}
	lines = append(lines, fmt.Sprintf("\n%d files discovered. Read each and use create_artifact / attach_section to migrate.", len(entries)))
	return text(strings.Join(lines, "\n")), nil, nil
}

type drainCleanupInput struct {
	Path string `json:"path"`
}

func (h *handler) handleDrainCleanup(ctx context.Context, _ *sdkmcp.CallToolRequest, in drainCleanupInput) (*sdkmcp.CallToolResult, any, error) {
	n, err := h.proto.DrainCleanup(ctx, in.Path)
	if err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("removed %d files", n)), nil, nil
}

func (h *handler) handleInventory(ctx context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, any, error) {
	inv, err := h.proto.Inventory(ctx)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.MarshalIndent(inv, "", "  ")
	return text(string(data)), nil, nil
}

type dashboardInput struct {
	StaleDays int `json:"stale_days,omitempty"`
}

func (h *handler) handleDashboard(ctx context.Context, _ *sdkmcp.CallToolRequest, in dashboardInput) (*sdkmcp.CallToolResult, any, error) {
	staleDays := in.StaleDays
	if staleDays <= 0 {
		staleDays = 30
	}
	report, err := h.proto.Dashboard(ctx, staleDays)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	return text(string(data)), nil, nil
}

type linkInput struct {
	ID       string   `json:"id"`
	Relation string   `json:"relation"`
	Targets  []string `json:"targets"`
	Unlink   bool     `json:"unlink,omitempty"`
}

func (h *handler) handleLink(ctx context.Context, _ *sdkmcp.CallToolRequest, in linkInput) (*sdkmcp.CallToolResult, any, error) {
	var results []protocol.Result
	var err error
	if in.Unlink {
		results, err = h.proto.UnlinkArtifacts(ctx, in.ID, in.Relation, in.Targets)
	} else {
		results, err = h.proto.LinkArtifacts(ctx, in.ID, in.Relation, in.Targets)
	}
	if err != nil {
		return nil, nil, err
	}
	verb := "linked"
	if in.Unlink {
		verb = "unlinked"
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, in.ID, in.Relation, r.ID))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleUnlink(ctx context.Context, _ *sdkmcp.CallToolRequest, in linkInput) (*sdkmcp.CallToolResult, any, error) {
	results, err := h.proto.UnlinkArtifacts(ctx, in.ID, in.Relation, in.Targets)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("unlinked %s -[%s]-> %s", in.ID, in.Relation, r.ID))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

type detectInput struct {
	Check   string `json:"check,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Status  string `json:"status,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Project string `json:"project,omitempty"`
}

func (h *handler) handleDetect(ctx context.Context, _ *sdkmcp.CallToolRequest, in detectInput) (*sdkmcp.CallToolResult, any, error) {
	check := in.Check
	if check == "" {
		check = "all"
	}
	var parts []string

	if check == "overlaps" || check == "all" {
		report, err := h.proto.DetectOverlaps(ctx, protocol.OverlapInput{
			Kind: in.Kind, Status: in.Status, Project: in.Project,
		})
		if err != nil {
			return nil, nil, err
		}
		if len(report.Overlaps) == 0 {
			parts = append(parts, fmt.Sprintf("No overlaps found across %d artifacts.", report.TotalScanned))
		} else {
			var b strings.Builder
			for _, o := range report.Overlaps {
				fmt.Fprintf(&b, "%s\n", o.Label)
				for _, a := range o.Artifacts {
					fmt.Fprintf(&b, "  %-16s %s\n", a.ID, a.Title)
				}
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%d overlap(s) across %d artifacts", report.TotalOverlaps, report.TotalScanned)
			parts = append(parts, b.String())
		}
	}

	if check == "orphans" || check == "all" {
		report, err := h.proto.DetectOrphans(ctx, protocol.OrphanInput{
			Scope: in.Scope, Status: in.Status,
		})
		if err != nil {
			return nil, nil, err
		}
		if len(report.Orphans) == 0 {
			parts = append(parts, fmt.Sprintf("No orphans found across %d artifacts.", report.TotalScanned))
		} else {
			var b strings.Builder
			for _, o := range report.Orphans {
				fmt.Fprintf(&b, "%-16s %-5s [%s] %s\n  → %s\n\n", o.ID, o.Kind, o.Status, o.Title, o.Reason)
			}
			fmt.Fprintf(&b, "%d orphan(s) across %d artifacts", report.TotalOrphans, report.TotalScanned)
			parts = append(parts, b.String())
		}
	}

	return text(strings.Join(parts, "\n\n")), nil, nil
}

func (h *handler) handleDetectOverlaps(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.OverlapInput) (*sdkmcp.CallToolResult, any, error) {
	report, err := h.proto.DetectOverlaps(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	if len(report.Overlaps) == 0 {
		return text(fmt.Sprintf("No overlaps found across %d %s artifacts.", report.TotalScanned, in.Status)), nil, nil
	}
	var b strings.Builder
	for _, o := range report.Overlaps {
		fmt.Fprintf(&b, "%s\n", o.Label)
		for _, a := range o.Artifacts {
			fmt.Fprintf(&b, "  %-16s %s\n", a.ID, a.Title)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "%d overlap(s) across %d artifacts", report.TotalOverlaps, report.TotalScanned)
	return text(b.String()), nil, nil
}

// --- orphan handler ---

func (h *handler) handleDetectOrphans(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.OrphanInput) (*sdkmcp.CallToolResult, any, error) {
	report, err := h.proto.DetectOrphans(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	if len(report.Orphans) == 0 {
		return text(fmt.Sprintf("No orphans found across %d artifacts.", report.TotalScanned)), nil, nil
	}
	var b strings.Builder
	for _, o := range report.Orphans {
		fmt.Fprintf(&b, "%-16s %-5s [%s] %s\n  → %s\n\n", o.ID, o.Kind, o.Status, o.Title, o.Reason)
	}
	fmt.Fprintf(&b, "%d orphan(s) across %d artifacts", report.TotalOrphans, report.TotalScanned)
	return text(b.String()), nil, nil
}

func (h *handler) handleLint(_ context.Context) (*sdkmcp.CallToolResult, any, error) {
	results := h.proto.Lint()
	if len(results) == 0 {
		return text("OK    schema is valid (0 errors, 0 warnings)"), nil, nil
	}
	var b strings.Builder
	errors, warnings := 0, 0
	for _, r := range results {
		switch r.Level {
		case "error":
			errors++
			fmt.Fprintf(&b, "ERROR %s\n", r.Message)
		case "warn":
			warnings++
			fmt.Fprintf(&b, "WARN  %s\n", r.Message)
		}
	}
	fmt.Fprintf(&b, "\nschema validated (%d error(s), %d warning(s))", errors, warnings)
	return text(b.String()), nil, nil
}

func (h *handler) handleCheck(ctx context.Context, scope string) (*sdkmcp.CallToolResult, any, error) {
	report, err := h.proto.Check(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.MarshalIndent(report, "", "  ")
	return text(string(data)), nil, nil
}

// --- vocab handlers ---

func (h *handler) handleVocabList(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, any, error) {
	kinds := h.proto.VocabList()
	return text(strings.Join(kinds, "\n")), nil, nil
}

type vocabKindInput struct {
	Kind string `json:"kind"`
}

func (h *handler) handleVocabAdd(_ context.Context, _ *sdkmcp.CallToolRequest, in vocabKindInput) (*sdkmcp.CallToolResult, any, error) {
	if err := h.proto.VocabAdd(in.Kind); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("registered kind %q", in.Kind)), nil, nil
}

func (h *handler) handleVocabRemove(ctx context.Context, _ *sdkmcp.CallToolRequest, in vocabKindInput) (*sdkmcp.CallToolResult, any, error) {
	if err := h.proto.VocabRemove(ctx, in.Kind); err != nil {
		return nil, nil, err
	}
	return text(fmt.Sprintf("removed kind %q", in.Kind)), nil, nil
}

// --- rendering helpers ---

func countDistinctScopes(node *protocol.TreeNode) int {
	scopes := map[string]struct{}{}
	var walk func(n *protocol.TreeNode)
	walk = func(n *protocol.TreeNode) {
		if n.Scope != "" {
			scopes[n.Scope] = struct{}{}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(node)
	return len(scopes)
}

func renderTree(node *protocol.TreeNode, prefix string, last, showScope bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	edgeLabel := ""
	if node.Edge != "" {
		arrow := " -> "
		if node.Direction == "incoming" {
			arrow = " <- "
		}
		edgeLabel = node.Edge + arrow
	}
	scopeLabel := ""
	if showScope && node.Scope != "" {
		scopeLabel = fmt.Sprintf(" [%s]", node.Scope)
	}
	fmt.Fprintf(b, "%s%s%s%s%s [%s] %s\n", prefix, connector, edgeLabel, node.ID, scopeLabel, node.Status, node.Title)
	cp := prefix
	if prefix != "" {
		if last {
			cp += "    "
		} else {
			cp += "│   "
		}
	}
	for i, ch := range node.Children {
		renderTree(ch, cp, i == len(node.Children)-1, showScope, b)
	}
}

func renderBriefing(node *protocol.TreeNode, prefix string, last, showScope bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}

	edgeLabel := ""
	if node.Edge != "" {
		arrow := " -> "
		if node.Direction == "incoming" {
			arrow = " <- "
		}
		edgeLabel = node.Edge + arrow
	}

	scopeLabel := ""
	if showScope && node.Scope != "" {
		scopeLabel = fmt.Sprintf(" [%s]", node.Scope)
	}

	kindStatus := node.Status
	if node.Kind != "" {
		kindStatus = node.Kind + "|" + node.Status
	}

	fmt.Fprintf(b, "%s%s%s%s%s [%s] %s\n", prefix, connector, edgeLabel, node.ID, scopeLabel, kindStatus, node.Title)

	cp := prefix
	if prefix != "" {
		if last {
			cp += "    "
		} else {
			cp += "│   "
		}
	}
	for i, ch := range node.Children {
		renderBriefing(ch, cp, i == len(node.Children)-1, showScope, b)
	}
}

func sortArtifacts(arts []*model.Artifact, field string) {
	sort.Slice(arts, func(i, j int) bool {
		switch field {
		case "title":
			return arts[i].Title < arts[j].Title
		case "status":
			return arts[i].Status < arts[j].Status
		case "scope":
			return arts[i].Scope < arts[j].Scope
		case "kind":
			return arts[i].Kind < arts[j].Kind
		case "sprint":
			return arts[i].Sprint < arts[j].Sprint
		default:
			return arts[i].ID < arts[j].ID
		}
	})
}

func text(s string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: s}},
	}
}

func noOut[In any](h func(context.Context, *sdkmcp.CallToolRequest, In) (*sdkmcp.CallToolResult, any, error)) sdkmcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest, in In) (*sdkmcp.CallToolResult, any, error) {
		tool := ""
		if req != nil {
			tool = req.Params.Name
		}
		start := time.Now()
		result, out, err := h(ctx, req, in)
		elapsed := time.Since(start)
		if err != nil {
			slog.Error("tool call failed", "tool", tool, "elapsed", elapsed, "error", err)
		} else {
			slog.Debug("tool call", "tool", tool, "elapsed", elapsed)
		}
		return result, out, err
	}
}
