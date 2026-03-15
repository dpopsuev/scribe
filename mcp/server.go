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
func NewServer(s store.Store, homeScopes, vocab []string, idc protocol.IDConfig, version string, snapshotter ...*store.Snapshotter) (*sdkmcp.Server, *directive.Registry) {
	srv := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "scribe", Version: version},
		&sdkmcp.ServerOptions{
			Instructions: "Scribe is a work graph for AI agents with native DAG support. " +
				"Use it to create, query, and manage structured artifacts (tasks, specs, goals, bugs, campaigns) " +
				"with parent-child trees, dependency edges, named text sections, and lifecycle status tracking. " +
				"Start with admin motd for context, then artifact list to explore. " +
				"Use graph topo_sort for execution order. Use follows edges for ROI ordering. " +
				"Templates auto-link via satisfies with cascading resolution (scoped > global). " +
				"Kinds: task (work unit), spec (design doc), bug (defect), goal (north star), campaign (mission), " +
				"template (section guidance), need (capability gap), doc/ref (knowledge), config (runtime settings), mirror (external ticket). " +
				"Relations: parent_of (tree), depends_on (hard block), follows (ROI order), implements (task→spec/bug), " +
				"justifies (need→spec), documents (ref→any), satisfies (artifact→template). " +
				"Workflow: motd → list/topo_sort → get (with section_filter) → create/update (with patch map) → set status. " +
				"Bulk ops: get accepts ids array, graph accepts bulk_link/bulk_unlink edge arrays, move re-parents, replace swaps edge targets.",
		},
	)
	reg := directive.New()
	var snap *store.Snapshotter
	if len(snapshotter) > 0 && snapshotter[0] != nil {
		snap = snapshotter[0]
	}
	h := &handler{
		proto:       protocol.New(s, nil, homeScopes, vocab, idc),
		snapshotter: snap,
		version:     version,
	}

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name: "artifact",
		Description: "Create, read, update, and manage work artifacts. " +
			"Actions: create (new artifact), get (by ID), list (filter/search), set (update field), " +
			"archive (mark read-only), attach_section (add text), get_section (read text), detach_section (remove text). " +
			"When creating artifacts linked to templates via satisfies relation, all template sections must be provided.",
		Keywords:   []string{"create", "get", "list", "set", "archive", "artifact", "section"},
		Categories: []string{"crud"},
	}, noOut(h.handleArtifact))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name: "graph",
		Description: "Navigate and modify artifact relationships. " +
			"Actions: tree (parent-child hierarchy with optional relation/direction/depth), " +
			"briefing (recursive traversal of ALL edges from any artifact, showing full context chain), " +
			"topo_sort (topological order of descendants by depends_on for execution planning), " +
			"link (add directed relationship), unlink (remove relationship), " +
			"bulk_link/bulk_unlink (array of edge tuples in one call), " +
			"move (re-parent atomically), replace (swap edge target). " +
			"Supported relations: parent_of, depends_on, follows, justifies, implements, documents.",
		Keywords:   []string{"tree", "briefing", "topo_sort", "link", "unlink", "bulk_link", "move", "replace", "relation", "edge", "graph"},
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
	proto       *protocol.Protocol
	snapshotter *store.Snapshotter
	version     string
}

// --- consolidated input types ---

type artifactInput struct {
	Action string `json:"action" jsonschema:"required,create | batch_create | clone | get | list | set | update | archive | attach_section | get_section | detach_section | diff"`

	ID    string `json:"id,omitempty" jsonschema:"artifact ID (required for get, set, update, archive, *_section)"`
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
	Sections  []map[string]string `json:"sections,omitempty" jsonschema:"array of section objects with name and text fields (create)"`

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
	Count          bool     `json:"count,omitempty" jsonschema:"return count instead of full artifacts (list)"`
	Top            int      `json:"top,omitempty" jsonschema:"return N most relevant artifacts ranked by status+priority+recency (list)"`
	Fields         []string `json:"fields,omitempty" jsonschema:"return only these columns: id, kind, scope, status, title, parent, priority, sprint, depends_on, labels (list)"`
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

	SectionFilter []string `json:"section_filter,omitempty" jsonschema:"return only these sections by name (get)"`
	Against       string   `json:"against,omitempty" jsonschema:"artifact ID to compare against (diff)"`

	IDs     []string `json:"ids,omitempty" jsonschema:"artifact IDs for bulk operations (get, set, archive)"`
	Cascade bool     `json:"cascade,omitempty" jsonschema:"recursively archive child subtrees (archive)"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"preview without applying changes (archive)"`

	Name string `json:"name,omitempty" jsonschema:"section name (attach_section, get_section, detach_section)"`
	Text string `json:"text,omitempty" jsonschema:"section body text (attach_section)"`

	Patch     map[string]string `json:"patch,omitempty" jsonschema:"map of field->value for multi-field update in one call (update)"`

	Artifacts []map[string]any `json:"artifacts,omitempty" jsonschema:"array of create inputs for batch_create"`
}

type graphInput struct {
	Action    string   `json:"action" jsonschema:"required,tree | briefing | topo_sort | link | unlink | bulk_link | bulk_unlink | move | replace | impact"`
	ID        string   `json:"id,omitempty" jsonschema:"root artifact ID for tree/briefing, or source artifact for link/unlink/move/replace"`
	Relation  string   `json:"relation,omitempty" jsonschema:"edge type: parent_of, depends_on, follows, justifies, implements, documents"`
	Direction string   `json:"direction,omitempty" jsonschema:"traversal direction for tree: outbound (default) or inbound"`
	Depth     int      `json:"depth,omitempty" jsonschema:"max traversal depth for tree (0 = unlimited)"`
	Targets   []string `json:"targets,omitempty" jsonschema:"target artifact IDs to link/unlink"`
	Target    string   `json:"target,omitempty" jsonschema:"new parent ID (move) or new target ID (replace)"`
	OldTarget string   `json:"old_target,omitempty" jsonschema:"existing target to replace (replace)"`
	Edges     []edgeInput `json:"edges,omitempty" jsonschema:"array of edge tuples for bulk_link/bulk_unlink"`
}

type edgeInput struct {
	From     string `json:"from" jsonschema:"source artifact ID"`
	Relation string `json:"relation" jsonschema:"edge type"`
	To       string `json:"to" jsonschema:"target artifact ID"`
}

type adminInput struct {
	Action string `json:"action" jsonschema:"required,motd | changelog | dashboard | snapshot | set_goal | vacuum | detect | lint | check | set_scope_labels | list_scope_labels | transfer_scope | seed"`

	// Snapshot sub-action and params
	SnapshotAction string `json:"snapshot_action,omitempty" jsonschema:"snapshot sub-action: create, list, diff, restore"`
	SnapshotName   string `json:"snapshot_name,omitempty" jsonschema:"snapshot label (create) or key (diff)"`

	StaleDays int    `json:"stale_days,omitempty" jsonschema:"days without update to consider an artifact stale (dashboard)"`
	Since     string `json:"since,omitempty" jsonschema:"RFC 3339 timestamp to show changes since (motd)"`

	Title string `json:"title,omitempty" jsonschema:"goal title (set_goal)"`
	Scope string `json:"scope,omitempty" jsonschema:"scope to target (set_goal, vacuum, detect)"`
	Kind  string `json:"kind,omitempty" jsonschema:"root delivery artifact kind, default goal (set_goal); or kind filter (detect)"`

	Days  int  `json:"days,omitempty" jsonschema:"delete archived artifacts older than this many days (vacuum)"`
	Force bool `json:"force,omitempty" jsonschema:"skip confirmation for destructive vacuum (vacuum)"`

	Target  string `json:"target,omitempty" jsonschema:"destination scope (transfer_scope)"`
	DryRun  bool   `json:"dry_run,omitempty" jsonschema:"preview without applying (transfer_scope)"`

	Check   string `json:"check,omitempty" jsonschema:"orphans (default), overlaps, or all (detect)"`
	Status  string `json:"status,omitempty" jsonschema:"filter by status (detect)"`
	Project string `json:"project,omitempty" jsonschema:"filter by project scope (detect)"`

	Labels []string `json:"labels,omitempty" jsonschema:"freeform labels for categorization (set_scope_labels)"`
}

// --- dispatchers ---

func (h *handler) handleArtifact(ctx context.Context, req *sdkmcp.CallToolRequest, in artifactInput) (*sdkmcp.CallToolResult, any, error) {
	switch in.Action {
	case "create":
		// Convert MCP sections format to model.Section
		var sections []model.Section
		for _, sec := range in.Sections {
			if name, ok := sec["name"]; ok {
				text := sec["text"] // empty string if not present
				sections = append(sections, model.Section{Name: name, Text: text})
			}
		}

		return h.handleCreate(ctx, req, protocol.CreateInput{
			Kind: in.Kind, Title: in.Title, Scope: in.Scope,
			Goal: in.Goal, Parent: in.Parent, Status: in.Status,
			Priority: in.Priority,
			DependsOn: in.DependsOn, Labels: in.Labels, Prefix: in.Prefix,
			Links: in.Links, Extra: in.Extra, CreatedAt: in.CreatedAt,
			Sections: sections,
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
			return nil, nil, fmt.Errorf("id or ids required for get action")
		}
		if len(ids) == 1 {
			return h.handleGet(ctx, req, getInput{ID: ids[0], IncludeEdges: in.IncludeEdges, SectionFilter: in.SectionFilter})
		}
		return h.handleBulkGet(ctx, ids, in.SectionFilter)
	case "list":
		li := protocol.ListInput{
			Kind: in.Kind, Scope: in.Scope, Status: in.Status,
			Parent: in.Parent, Sprint: in.Sprint, IDPrefix: in.IDPrefix,
			ExcludeKind: in.ExcludeKind, ExcludeStatus: in.ExcludeStatus,
			Labels: in.Labels, LabelsOr: in.LabelsOr, ExcludeLabels: in.ExcludeLabels,
			GroupBy: in.GroupBy, Sort: in.Sort, Limit: in.Limit, Query: in.Query,
			CreatedAfter: in.CreatedAfter, CreatedBefore: in.CreatedBefore,
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
			return h.handleListCompact(ctx, li, in.Fields)
		}
		return h.handleList(ctx, req, li)
	case "set":
		ids := in.IDs
		if len(ids) == 0 && in.ID != "" {
			ids = []string{in.ID}
		}
		if len(ids) == 0 {
			return nil, nil, fmt.Errorf("id or ids required for set action")
		}
		return h.handleBulkSetField(ctx, ids, in.Field, in.Value, in.Force)
	case "update":
		return h.handleUpdate(ctx, in)
	case "archive":
		return h.handleArchive(ctx, req, archiveInput{
			IDs: in.IDs, Cascade: in.Cascade, Scope: in.Scope,
			Kind: in.Kind, Status: in.Status, IDPrefix: in.IDPrefix,
			ExcludeKind: in.ExcludeKind, DryRun: in.DryRun,
		})
	case "attach_section":
		if len(in.Sections) > 0 {
			return h.handleBatchAttachSections(ctx, in.ID, in.Sections)
		}
		return h.handleAttachSection(ctx, req, sectionInput{ID: in.ID, Name: in.Name, Text: in.Text})
	case "get_section":
		return h.handleGetSection(ctx, req, getSectionInput{ID: in.ID, Name: in.Name})
	case "detach_section":
		return h.handleDetachSection(ctx, req, getSectionInput{ID: in.ID, Name: in.Name})
	case "diff":
		if in.ID == "" || in.Against == "" {
			return nil, nil, fmt.Errorf("id and against required for diff")
		}
		return h.handleDiff(ctx, in.ID, in.Against)
	default:
		return nil, nil, fmt.Errorf("unknown artifact action %q (valid: create, batch_create, clone, get, list, set, update, archive, attach_section, get_section, detach_section, diff)", in.Action)
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
	case "topo_sort":
		if in.ID == "" {
			return nil, nil, fmt.Errorf("id required for topo_sort (root artifact)")
		}
		entries, err := h.proto.TopoSort(ctx, in.ID)
		if err != nil && len(entries) == 0 {
			return nil, nil, err
		}
		var b strings.Builder
		for i, e := range entries {
			fmt.Fprintf(&b, "%d. %s [%s] %s", i+1, e.ID, e.Status, e.Title)
			if e.Priority != "" && e.Priority != "none" {
				fmt.Fprintf(&b, " (%s)", e.Priority)
			}
			b.WriteString("\n")
		}
		if err != nil {
			fmt.Fprintf(&b, "\n%s\n", err)
		}
		return text(b.String()), nil, nil
	case "unlink":
		return h.handleLink(ctx, req, linkInput{
			ID: in.ID, Relation: in.Relation, Targets: in.Targets, Unlink: true,
		})
	case "bulk_link":
		return h.handleBulkEdge(ctx, in.Edges, false)
	case "bulk_unlink":
		return h.handleBulkEdge(ctx, in.Edges, true)
	case "move":
		if in.ID == "" || in.Target == "" {
			return nil, nil, fmt.Errorf("id and target required for move")
		}
		return h.handleMove(ctx, in.ID, in.Target)
	case "replace":
		if in.ID == "" || in.Relation == "" || in.OldTarget == "" || in.Target == "" {
			return nil, nil, fmt.Errorf("id, relation, old_target, and target required for replace")
		}
		return h.handleReplace(ctx, in.ID, in.Relation, in.OldTarget, in.Target)
	case "impact":
		if in.ID == "" {
			return nil, nil, fmt.Errorf("id required for impact analysis")
		}
		return h.handleImpact(ctx, in.ID)
	default:
		return nil, nil, fmt.Errorf("unknown graph action %q (valid: tree, briefing, topo_sort, link, unlink, bulk_link, bulk_unlink, move, replace, impact)", in.Action)
	}
}

func (h *handler) handleAdmin(ctx context.Context, req *sdkmcp.CallToolRequest, in adminInput) (*sdkmcp.CallToolResult, any, error) {
	switch in.Action {
	case "motd":
		return h.handleMotd(ctx, req, motdInput{Since: in.Since})
	case "changelog":
		return h.handleChangelog(ctx, in.Since, in.Scope)
	case "snapshot":
		return h.handleSnapshot(ctx, in)
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
	case "seed":
		dir := in.Scope // reuse scope field as dir path
		if dir == "" {
			return nil, nil, fmt.Errorf("scope field required as seed directory path")
		}
		result, err := h.proto.Seed(ctx, dir)
		if err != nil {
			return nil, nil, err
		}
		var lines []string
		for _, id := range result.Created {
			lines = append(lines, "created "+id)
		}
		for _, id := range result.Skipped {
			lines = append(lines, "skipped "+id)
		}
		return text(fmt.Sprintf("seed: %d created, %d skipped\n%s",
			len(result.Created), len(result.Skipped), strings.Join(lines, "\n"))), nil, nil
	case "transfer_scope":
		if in.Scope == "" || in.Target == "" {
			return nil, nil, fmt.Errorf("scope and target required for transfer_scope")
		}
		result, err := h.proto.BulkSetField(ctx, protocol.BulkMutationInput{
			Scope: in.Scope, Kind: in.Kind, Status: in.Status, DryRun: in.DryRun,
		}, "scope", in.Target)
		if err != nil {
			return nil, nil, err
		}
		if result.DryRun {
			return text(fmt.Sprintf("dry run: would transfer %d artifacts from %s to %s", result.Count, in.Scope, in.Target)), nil, nil
		}
		return text(fmt.Sprintf("transferred %d artifacts from %s to %s", result.Count, in.Scope, in.Target)), nil, nil
	default:
		return nil, nil, fmt.Errorf("unknown admin action %q", in.Action)
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
	ID            string   `json:"id"`
	IncludeEdges  bool     `json:"include_edges,omitempty"`
	SectionFilter []string `json:"section_filter,omitempty"`
}

func filterSections(art *model.Artifact, filter []string) {
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
	filterSections(art, in.SectionFilter)
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

func (h *handler) handleBulkGet(ctx context.Context, ids []string, sectionFilter []string) (*sdkmcp.CallToolResult, any, error) {
	var arts []*model.Artifact
	for _, id := range ids {
		art, err := h.proto.GetArtifact(ctx, id)
		if err != nil {
			return nil, nil, fmt.Errorf("get %s: %w", id, err)
		}
		filterSections(art, sectionFilter)
		arts = append(arts, art)
	}
	data, _ := json.MarshalIndent(arts, "", "  ")
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

func (h *handler) handleListCount(ctx context.Context, in protocol.ListInput) (*sdkmcp.CallToolResult, any, error) {
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
		data, _ := json.MarshalIndent(groups, "", "  ")
		return text(string(data)), nil, nil
	}

	return text(fmt.Sprintf("%d", len(arts))), nil, nil
}

func relevanceScore(a *model.Artifact) int {
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
		if daysSince < 1 {
			score += 30
		} else if daysSince < 7 {
			score += 20
		} else if daysSince < 30 {
			score += 10
		}
	}
	return score
}

func (h *handler) handleListTop(ctx context.Context, in protocol.ListInput, top int) (*sdkmcp.CallToolResult, any, error) {
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
	sort.Slice(arts, func(i, j int) bool {
		return relevanceScore(arts[i]) > relevanceScore(arts[j])
	})
	if top < len(arts) {
		arts = arts[:top]
	}
	data, _ := json.MarshalIndent(arts, "", "  ")
	return text(string(data)), nil, nil
}

var validFields = map[string]func(*model.Artifact) string{
	"id":         func(a *model.Artifact) string { return a.ID },
	"kind":       func(a *model.Artifact) string { return a.Kind },
	"scope":      func(a *model.Artifact) string { return a.Scope },
	"status":     func(a *model.Artifact) string { return a.Status },
	"title":      func(a *model.Artifact) string { return a.Title },
	"parent":     func(a *model.Artifact) string { return a.Parent },
	"priority":   func(a *model.Artifact) string { return a.Priority },
	"sprint":     func(a *model.Artifact) string { return a.Sprint },
	"depends_on": func(a *model.Artifact) string { return strings.Join(a.DependsOn, ",") },
	"labels":     func(a *model.Artifact) string { return strings.Join(a.Labels, ",") },
}

func (h *handler) handleListCompact(ctx context.Context, in protocol.ListInput, fields []string) (*sdkmcp.CallToolResult, any, error) {
	// Validate fields
	var getters []func(*model.Artifact) string
	for _, f := range fields {
		g, ok := validFields[f]
		if !ok {
			return nil, nil, fmt.Errorf("unknown field %q (valid: id, kind, scope, status, title, parent, priority, sprint, depends_on, labels)", f)
		}
		getters = append(getters, g)
	}

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
	if in.Sort != "" {
		sortArtifacts(arts, in.Sort)
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
	fmt.Fprintf(&b, "\n(%d artifacts)\n", len(arts))
	return text(b.String()), nil, nil
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

func (h *handler) handleBulkSetField(ctx context.Context, ids []string, field, value string, force bool) (*sdkmcp.CallToolResult, any, error) {
	results, err := h.proto.SetField(ctx, ids, field, value, protocol.SetFieldOptions{Force: force})
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
		return nil, nil, fmt.Errorf("id is required for batch attach_section")
	}
	var added, replaced int
	for _, sec := range sections {
		name, ok := sec["name"]
		if !ok || name == "" {
			return nil, nil, fmt.Errorf("each section must have a 'name' field")
		}
		t := sec["text"]
		wasReplaced, err := h.proto.AttachSection(ctx, id, name, t)
		if err != nil {
			return nil, nil, fmt.Errorf("section %q: %w", name, err)
		}
		if wasReplaced {
			replaced++
		} else {
			added++
		}
	}
	return text(fmt.Sprintf("%s: %d sections added, %d replaced", id, added, replaced)), nil, nil
}

func (h *handler) handleUpdate(ctx context.Context, in artifactInput) (*sdkmcp.CallToolResult, any, error) {
	if in.ID == "" {
		return nil, nil, fmt.Errorf("id is required for update action")
	}

	var lines []string

	// Apply field updates — patch map takes precedence, then individual fields
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

	for field, value := range fieldMap {
		results, err := h.proto.SetField(ctx, []string{in.ID}, field, value, protocol.SetFieldOptions{Force: in.Force})
		if err != nil {
			return nil, nil, fmt.Errorf("set %s: %w", field, err)
		}
		r := results[0]
		if !r.OK {
			return nil, nil, fmt.Errorf("set %s: %s", field, r.Error)
		}
		lines = append(lines, fmt.Sprintf("%s.%s = %s", in.ID, field, value))
	}

	// Apply section attaches
	for _, sec := range in.Sections {
		name, ok := sec["name"]
		if !ok || name == "" {
			continue
		}
		t := sec["text"]
		replaced, err := h.proto.AttachSection(ctx, in.ID, name, t)
		if err != nil {
			return nil, nil, fmt.Errorf("section %q: %w", name, err)
		}
		action := "added"
		if replaced {
			action = "replaced"
		}
		lines = append(lines, fmt.Sprintf("%s: section %q %s", in.ID, name, action))
	}

	if len(lines) == 0 {
		return nil, nil, fmt.Errorf("update requires at least one field or section to change")
	}

	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleBatchCreate(ctx context.Context, in artifactInput) (*sdkmcp.CallToolResult, any, error) {
	rawArtifacts := in.Artifacts
	if len(rawArtifacts) == 0 {
		return nil, nil, fmt.Errorf("artifacts array is required for batch_create")
	}

	var created []*model.Artifact
	idRefs := make(map[string]string)

	for i, raw := range rawArtifacts {
		// Re-marshal + unmarshal to leverage existing JSON parsing
		data, _ := json.Marshal(raw)
		var ci artifactInput
		if err := json.Unmarshal(data, &ci); err != nil {
			return nil, nil, fmt.Errorf("artifact[%d]: %w", i, err)
		}

		if strings.HasPrefix(ci.Parent, "$") {
			if resolved, ok := idRefs[ci.Parent]; ok {
				ci.Parent = resolved
			} else {
				return nil, nil, fmt.Errorf("artifact[%d]: unresolved parent reference %q", i, ci.Parent)
			}
		}

		var sections []model.Section
		for _, sec := range ci.Sections {
			if name, ok := sec["name"]; ok {
				sections = append(sections, model.Section{Name: name, Text: sec["text"]})
			}
		}

		art, err := h.proto.CreateArtifact(ctx, protocol.CreateInput{
			Kind: ci.Kind, Title: ci.Title, Scope: ci.Scope,
			Goal: ci.Goal, Parent: ci.Parent, Status: ci.Status,
			Priority: ci.Priority,
			DependsOn: ci.DependsOn, Labels: ci.Labels, Prefix: ci.Prefix,
			Links: ci.Links, Extra: ci.Extra, CreatedAt: ci.CreatedAt,
			Sections: sections,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("artifact[%d] %q: %w", i, ci.Title, err)
		}
		created = append(created, art)
		idRefs[fmt.Sprintf("$%d", i)] = art.ID
	}

	var lines []string
	for _, art := range created {
		lines = append(lines, fmt.Sprintf("%s [%s] %s", art.ID, art.Kind, art.Title))
	}
	return text(fmt.Sprintf("created %d artifacts:\n%s", len(created), strings.Join(lines, "\n"))), nil, nil
}

func (h *handler) handleClone(ctx context.Context, in artifactInput) (*sdkmcp.CallToolResult, any, error) {
	if in.ID == "" {
		return nil, nil, fmt.Errorf("id is required for clone (source artifact)")
	}
	source, err := h.proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("source %s: %w", in.ID, err)
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
	var sections []model.Section
	for _, s := range source.Sections {
		sections = append(sections, model.Section{Name: s.Name, Text: s.Text})
	}

	// Copy extra from source
	var extra map[string]any
	if len(source.Extra) > 0 {
		extra = make(map[string]any)
		for k, v := range source.Extra {
			extra[k] = v
		}
	}

	art, err := h.proto.CreateArtifact(ctx, protocol.CreateInput{
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
		return nil, nil, fmt.Errorf("clone: %w", err)
	}

	data, _ := json.MarshalIndent(art, "", "  ")
	return text(fmt.Sprintf("cloned %s → %s\n%s", in.ID, art.ID, string(data))), nil, nil
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

type motdInput struct {
	Since string `json:"since,omitempty"`
}

func (h *handler) handleMotd(ctx context.Context, _ *sdkmcp.CallToolRequest, in motdInput) (*sdkmcp.CallToolResult, any, error) {
	m, err := h.proto.Motd(ctx)
	if err != nil {
		return nil, nil, err
	}
	var sections []string
	sections = append(sections, fmt.Sprintf("Scribe %s", h.version))

	// Open bugs — fires first
	bugs, _ := h.proto.ListArtifacts(ctx, protocol.ListInput{Kind: model.KindBug, Status: model.StatusOpen})
	if len(bugs) > 0 {
		var lines []string
		for _, b := range bugs {
			prio := ""
			if b.Priority != "" {
				prio = " [" + b.Priority + "]"
			}
			lines = append(lines, fmt.Sprintf("  %s%s %s", b.ID, prio, b.Title))
		}
		sections = append(sections, "Open Bugs:\n"+strings.Join(lines, "\n"))
	}

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

	// Active work summary
	active, _ := h.proto.ListArtifacts(ctx, protocol.ListInput{Status: model.StatusActive})
	draft, _ := h.proto.ListArtifacts(ctx, protocol.ListInput{Status: model.StatusDraft})
	if len(active) > 0 || len(draft) > 0 {
		sections = append(sections, fmt.Sprintf("Active Work: %d active, %d draft", len(active), len(draft)))
	}

	// Stale drafts (>7 days without update)
	staleThreshold := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	stale, _ := h.proto.ListArtifacts(ctx, protocol.ListInput{Status: model.StatusDraft, UpdatedBefore: staleThreshold})
	if len(stale) > 0 {
		var lines []string
		limit := len(stale)
		if limit > 10 {
			limit = 10
		}
		for _, s := range stale[:limit] {
			lines = append(lines, fmt.Sprintf("  %s [%s] %s", s.ID, s.Scope, s.Title))
		}
		header := fmt.Sprintf("Stale Drafts (%d, >7 days):", len(stale))
		if len(stale) > 10 {
			header = fmt.Sprintf("Stale Drafts (%d, >7 days, showing 10):", len(stale))
		}
		sections = append(sections, header+"\n"+strings.Join(lines, "\n"))
	}

	// Changed since (session delta)
	if in.Since != "" {
		changed, _ := h.proto.ListArtifacts(ctx, protocol.ListInput{UpdatedAfter: in.Since, ExcludeStatus: model.StatusArchived})
		if len(changed) > 0 {
			var lines []string
			limit := len(changed)
			if limit > 15 {
				limit = 15
			}
			for _, c := range changed[:limit] {
				lines = append(lines, fmt.Sprintf("  %s %-8s [%s] %s", c.ID, c.Status, c.Kind, c.Title))
			}
			header := fmt.Sprintf("Changed Since %s (%d):", in.Since[:10], len(changed))
			if len(changed) > 15 {
				header = fmt.Sprintf("Changed Since %s (%d, showing 15):", in.Since[:10], len(changed))
			}
			sections = append(sections, header+"\n"+strings.Join(lines, "\n"))
		}
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

func (h *handler) handleChangelog(ctx context.Context, since, scope string) (*sdkmcp.CallToolResult, any, error) {
	if since == "" {
		return nil, nil, fmt.Errorf("since parameter is required for changelog (RFC 3339 timestamp)")
	}
	li := protocol.ListInput{
		UpdatedAfter:  since,
		ExcludeStatus: model.StatusArchived,
		Scope:         scope,
	}
	arts, err := h.proto.ListArtifacts(ctx, li)
	if err != nil {
		return nil, nil, err
	}
	if len(arts) == 0 {
		return text(fmt.Sprintf("no changes since %s", since[:10])), nil, nil
	}

	// Group by scope
	byScope := make(map[string][]*model.Artifact)
	for _, a := range arts {
		s := a.Scope
		if s == "" {
			s = "(none)"
		}
		byScope[s] = append(byScope[s], a)
	}

	var sections []string
	for s, scopeArts := range byScope {
		var lines []string
		for _, a := range scopeArts {
			lines = append(lines, fmt.Sprintf("  %-16s %-8s %-8s %s", a.ID, a.Kind, a.Status, a.Title))
		}
		sections = append(sections, fmt.Sprintf("[%s] (%d):\n%s", s, len(scopeArts), strings.Join(lines, "\n")))
	}
	sort.Strings(sections)

	header := fmt.Sprintf("Changes since %s (%d artifacts):\n", since[:10], len(arts))
	return text(header + strings.Join(sections, "\n\n")), nil, nil
}

func (h *handler) handleSnapshot(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) {
	if h.snapshotter == nil {
		return nil, nil, fmt.Errorf("snapshot system not configured")
	}

	switch in.SnapshotAction {
	case "create":
		meta, err := h.snapshotter.Create(ctx, in.SnapshotName)
		if err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("snapshot created: %s (%d artifacts, %d bytes)",
			meta.Key, meta.Artifacts, meta.SizeBytes)), nil, nil

	case "list":
		snapshots, err := h.snapshotter.List(ctx)
		if err != nil {
			return nil, nil, err
		}
		if len(snapshots) == 0 {
			return text("no snapshots found"), nil, nil
		}
		var lines []string
		for _, s := range snapshots {
			name := s.Name
			if name == "" {
				name = "(auto)"
			}
			lines = append(lines, fmt.Sprintf("  %-20s %s  %d bytes",
				name, s.Timestamp.Format("2006-01-02 15:04:05"), s.SizeBytes))
		}
		return text(fmt.Sprintf("Snapshots (%d):\n%s", len(snapshots), strings.Join(lines, "\n"))), nil, nil

	case "diff":
		if in.SnapshotName == "" {
			return nil, nil, fmt.Errorf("snapshot_name required for diff")
		}
		diff, err := h.snapshotter.Diff(ctx, in.SnapshotName)
		if err != nil {
			return nil, nil, err
		}
		var parts []string
		if len(diff.Added) > 0 {
			parts = append(parts, fmt.Sprintf("Added (%d): %s", len(diff.Added), strings.Join(diff.Added, ", ")))
		}
		if len(diff.Removed) > 0 {
			parts = append(parts, fmt.Sprintf("Removed (%d): %s", len(diff.Removed), strings.Join(diff.Removed, ", ")))
		}
		if len(diff.Modified) > 0 {
			parts = append(parts, fmt.Sprintf("Modified (%d): %s", len(diff.Modified), strings.Join(diff.Modified, ", ")))
		}
		if len(parts) == 0 {
			return text("no differences"), nil, nil
		}
		return text(strings.Join(parts, "\n")), nil, nil

	case "restore":
		if in.SnapshotName == "" {
			return nil, nil, fmt.Errorf("snapshot_name required for restore (use list to find keys)")
		}
		if err := h.snapshotter.Restore(ctx, in.SnapshotName); err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("database restored from snapshot: %s (pre-restore backup created)", in.SnapshotName)), nil, nil

	default:
		return nil, nil, fmt.Errorf("unknown snapshot action %q (valid: create, list, diff, restore)", in.SnapshotAction)
	}
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

func (h *handler) handleImpact(ctx context.Context, id string) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.GetArtifact(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Impact analysis for %s [%s] %s:", id, art.Status, art.Title))

	// Children (parent_of)
	children, _ := h.proto.ListArtifacts(ctx, protocol.ListInput{Parent: id})
	if len(children) > 0 {
		lines = append(lines, fmt.Sprintf("\nChildren (%d):", len(children)))
		for _, ch := range children {
			lines = append(lines, fmt.Sprintf("  %s [%s] %s", ch.ID, ch.Status, ch.Title))
		}
	}

	// Inbound depends_on (things that depend on this)
	depEdges, _ := h.proto.GetArtifactEdges(ctx, id)
	var dependents, implementors []string
	for _, e := range depEdges {
		if e.Direction == "incoming" {
			switch e.Relation {
			case "depends_on":
				dependents = append(dependents, fmt.Sprintf("  %s [%s] %s", e.Target.ID, e.Target.Status, e.Target.Title))
			case "implements":
				implementors = append(implementors, fmt.Sprintf("  %s [%s] %s", e.Target.ID, e.Target.Status, e.Target.Title))
			}
		}
	}
	if len(dependents) > 0 {
		lines = append(lines, fmt.Sprintf("\nDepends on this (%d):", len(dependents)))
		lines = append(lines, dependents...)
	}
	if len(implementors) > 0 {
		lines = append(lines, fmt.Sprintf("\nImplements this (%d):", len(implementors)))
		lines = append(lines, implementors...)
	}

	// Warnings
	var warnings []string
	if len(children) > 0 {
		nonTerminal := 0
		for _, ch := range children {
			if ch.Status != "complete" && ch.Status != "archived" && ch.Status != "cancelled" {
				nonTerminal++
			}
		}
		if nonTerminal > 0 {
			warnings = append(warnings, fmt.Sprintf("%d children would be orphaned (non-terminal)", nonTerminal))
		}
	}
	if len(dependents) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d artifacts depend on this — their dependency chain would break", len(dependents)))
	}
	if len(warnings) > 0 {
		lines = append(lines, "\nWarnings:")
		for _, w := range warnings {
			lines = append(lines, "  ⚠ "+w)
		}
	}

	if len(children) == 0 && len(dependents) == 0 && len(implementors) == 0 {
		lines = append(lines, "\nNo downstream impact — safe to archive.")
	}

	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleBulkEdge(ctx context.Context, edges []edgeInput, unlink bool) (*sdkmcp.CallToolResult, any, error) {
	if len(edges) == 0 {
		return nil, nil, fmt.Errorf("edges array is required for bulk_link/bulk_unlink")
	}
	var lines []string
	for _, e := range edges {
		var results []protocol.Result
		var err error
		if unlink {
			results, err = h.proto.UnlinkArtifacts(ctx, e.From, e.Relation, []string{e.To})
		} else {
			results, err = h.proto.LinkArtifacts(ctx, e.From, e.Relation, []string{e.To})
		}
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, err))
			continue
		}
		for _, r := range results {
			if r.OK {
				verb := "linked"
				if unlink {
					verb = "unlinked"
				}
				lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, e.From, e.Relation, e.To))
			} else {
				lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, r.Error))
			}
		}
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleMove(ctx context.Context, id, newParent string) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.GetArtifact(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	oldParent := art.Parent
	results, err := h.proto.SetField(ctx, []string{id}, "parent", newParent)
	if err != nil {
		return nil, nil, err
	}
	if !results[0].OK {
		return nil, nil, fmt.Errorf("move %s: %s", id, results[0].Error)
	}
	msg := fmt.Sprintf("moved %s: parent %s -> %s", id, oldParent, newParent)
	return text(msg), nil, nil
}

func (h *handler) handleReplace(ctx context.Context, id, relation, oldTarget, newTarget string) (*sdkmcp.CallToolResult, any, error) {
	// Unlink old
	results, err := h.proto.UnlinkArtifacts(ctx, id, relation, []string{oldTarget})
	if err != nil {
		return nil, nil, err
	}
	if len(results) > 0 && !results[0].OK {
		return nil, nil, fmt.Errorf("unlink old: %s", results[0].Error)
	}
	// Link new
	results, err = h.proto.LinkArtifacts(ctx, id, relation, []string{newTarget})
	if err != nil {
		return nil, nil, err
	}
	if len(results) > 0 && !results[0].OK {
		return nil, nil, fmt.Errorf("link new: %s", results[0].Error)
	}
	return text(fmt.Sprintf("replaced %s -[%s]-> %s with %s", id, relation, oldTarget, newTarget)), nil, nil
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
