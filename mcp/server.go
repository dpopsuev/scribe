package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dpopsuev/scribe/directive"
	"github.com/dpopsuev/scribe/mcpclient"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/render"
	"github.com/dpopsuev/scribe/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server exposing Scribe tools over the given store.
// Returns both the server and a directive registry for CLI introspection.
func NewServer(s store.Store, homeScopes, vocab []string) (*sdkmcp.Server, *directive.Registry) {
	srv := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "scribe", Version: "0.1.1"},
		&sdkmcp.ServerOptions{
		Instructions: "Scribe is a lean governance artifact store with native DAG support. " +
			"Use it to create, query, and manage structured artifacts (tasks, specs, sprints, goals, bugs) " +
			"with parent-child trees, dependency edges, named text sections, and lifecycle status tracking. " +
			"Start with motd for context, then list_artifacts to explore.",
		},
	)
	reg := directive.New()
	h := &handler{
		proto: protocol.New(s, nil, homeScopes, vocab),
		locus: mcpclient.New(mcpclient.DefaultLocusURL()),
	}

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "create_artifact",
		Description: "Create a new governance artifact (task, spec, goal, sprint, bug)",
		Keywords:    []string{"create", "new", "artifact", "contract", "sprint", "goal"},
		Categories:  []string{"crud"},
	}, noOut(h.handleCreate))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "get_artifact",
		Description: "Retrieve a single artifact by ID",
		Keywords:    []string{"get", "show", "artifact", "detail"},
		Categories:  []string{"crud"},
	}, noOut(h.handleGet))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "list_artifacts",
		Description: "List artifacts with optional filters (kind, scope, status, parent, sprint, id_prefix, exclude_kind, exclude_status). Supports group_by (status, scope, kind, sprint), sort (id, title, status, scope, kind, sprint), limit, and query (substring search across title, goal, section text).",
		Keywords:    []string{"list", "artifacts", "filter", "query", "sprint", "scope"},
		Categories:  []string{"crud", "query"},
	}, noOut(h.handleList))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "set_field",
		Description: "Set a single field on an artifact. Supported fields: title, goal, scope, status, parent, priority, sprint, kind, depends_on (comma-separated), labels (comma-separated). Unknown fields are stored in the extra map.",
		Keywords:    []string{"set", "update", "field", "status", "title"},
		Categories:  []string{"crud"},
	}, noOut(h.handleSetField))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "attach_section",
		Description: "Add or replace a named text section on an artifact. Use for mermaid diagrams, architecture specs, notes, or any structured text attachment.",
		Keywords:    []string{"section", "attach", "text", "note", "diagram"},
		Categories:  []string{"crud", "sections"},
	}, noOut(h.handleAttachSection))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "get_section",
		Description: "Retrieve a named section's text from an artifact.",
		Keywords:    []string{"section", "get", "text"},
		Categories:  []string{"query", "sections"},
	}, noOut(h.handleGetSection))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "contract_tree",
		Description: "Return the parent-child tree rooted at an artifact",
		Keywords:    []string{"tree", "hierarchy", "children", "parent"},
		Categories:  []string{"query", "navigation"},
	}, noOut(h.handleTree))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "set_goal",
		Description: "Set a new goal (archives any current goal for the scope) and auto-create a root delivery artifact linked via 'justifies'. Returns both the goal and its root artifact.",
		Keywords:    []string{"goal", "set", "north star"},
		Categories:  []string{"lifecycle"},
	}, noOut(h.handleSetGoal))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "archive_artifact",
		Description: "Archive one or more artifacts (marks read-only). Use cascade=true to recursively archive child subtrees. When no IDs given, archives all matching filter (scope, kind, status, id_prefix, exclude_kind). Use dry_run=true to preview.",
		Keywords:    []string{"archive", "retire", "cascade"},
		Categories:  []string{"lifecycle"},
	}, noOut(h.handleArchive))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "vacuum",
		Description: "Delete archived artifacts older than the specified number of days. Returns IDs of deleted artifacts.",
		Keywords:    []string{"vacuum", "cleanup", "delete", "purge"},
		Categories:  []string{"lifecycle", "maintenance"},
	}, noOut(h.handleVacuum))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "motd",
		Description: "Message of the day: returns due reminders, recent notes, and the current goal. Useful at session start for context.",
		Keywords:    []string{"motd", "context", "reminder", "goal"},
		Categories:  []string{"query", "navigation"},
	}, noOut(h.handleMotd))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "dashboard",
		Description: "Housekeeping dashboard: storage, staleness, scope health. Returns scopes with total/active/archived/sections/edges/stale counts, DB size, and top 10 stale artifacts.",
		Keywords:    []string{"dashboard", "df", "housekeeping", "stale", "storage"},
		Categories:  []string{"query", "maintenance"},
	}, noOut(h.handleDashboard))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "link_artifacts",
		Description: "Add a directed relationship between artifacts. Supported relations: parent_of, depends_on, justifies, implements, documents, satisfies. Set unlink=true to remove the relationship instead.",
		Keywords:    []string{"link", "relation", "edge", "depends", "parent"},
		Categories:  []string{"crud", "graph"},
	}, noOut(h.handleLink))

	directive.AddTool(reg, srv, directive.ToolMeta{
		Name:        "detect_orphans",
		Description: "Find tasks without implements links to specs/bugs, and specs/bugs without tasks implementing them. Warns about missing relationships. Use check=overlaps to find scope conflicts, check=all for both.",
		Keywords:    []string{"orphan", "lint", "unlinked", "implements", "spec", "bug", "overlap", "conflict"},
		Categories:  []string{"query", "governance"},
	}, noOut(h.handleDetect))

	return srv, reg
}

// ToolRegistry returns a populated directive registry without requiring
// a database connection. Useful for CLI introspection (scribe tools).
func ToolRegistry() *directive.Registry {
	_, reg := NewServer(nil, nil, nil)
	return reg
}

type handler struct {
	proto *protocol.Protocol
	locus *mcpclient.LocusClient
}

// --- handlers (thin wrappers) ---

func (h *handler) handleCreate(ctx context.Context, _ *sdkmcp.CallToolRequest, in protocol.CreateInput) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.CreateArtifact(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.MarshalIndent(art, "", "  ")
	return text(string(data)), nil, nil
}

type idInput struct {
	ID string `json:"id"`
}

func (h *handler) handleGet(ctx context.Context, _ *sdkmcp.CallToolRequest, in idInput) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return text(render.Markdown(art)), nil, nil
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
	if in.GroupBy != "" {
		return text(render.GroupedTable(arts, in.GroupBy)), nil, nil
	}
	return text(render.Table(arts)), nil, nil
}

type setFieldInput struct {
	ID    string `json:"id"`
	Field string `json:"field"`
	Value string `json:"value"`
}

func (h *handler) handleSetField(ctx context.Context, _ *sdkmcp.CallToolRequest, in setFieldInput) (*sdkmcp.CallToolResult, any, error) {
	results, err := h.proto.SetField(ctx, []string{in.ID}, in.Field, in.Value)
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

func (h *handler) handleTree(ctx context.Context, _ *sdkmcp.CallToolRequest, in idInput) (*sdkmcp.CallToolResult, any, error) {
	tree, err := h.proto.ContractTree(ctx, in.ID)
	if err != nil {
		return nil, nil, err
	}
	showScope := countDistinctScopes(tree) > 1
	var b strings.Builder
	renderTree(tree, "", true, showScope, &b)
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
	now := time.Now().UTC()
	if len(m.DueReminders) > 0 {
		var lines []string
		for _, n := range m.DueReminders {
			r, _ := n.Extra["remind_at"].(string)
			lines = append(lines, fmt.Sprintf("  %s %s (due: %s)", n.ID, n.Title, r))
		}
		sections = append(sections, fmt.Sprintf("Due reminders (%d):\n%s", len(m.DueReminders), strings.Join(lines, "\n")))
	}
	if len(m.RecentNotes) > 0 {
		var lines []string
		for _, n := range m.RecentNotes {
			age := now.Sub(n.CreatedAt).Truncate(time.Minute)
			lines = append(lines, fmt.Sprintf("  %s %s (%s ago)", n.ID, n.Title, age))
		}
		sections = append(sections, fmt.Sprintf("Recent notes (%d):\n%s", len(m.RecentNotes), strings.Join(lines, "\n")))
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

// --- CON-318: context mesh ---

type contextMeshInput struct {
	ID   string `json:"id"`
	Path string `json:"path,omitempty"`
}

func (h *handler) handleContextMesh(ctx context.Context, _ *sdkmcp.CallToolRequest, in contextMeshInput) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("get artifact %s: %w", in.ID, err)
	}

	path := in.Path
	if path == "" {
		path = art.Scope
	}
	if path == "" {
		return nil, nil, fmt.Errorf("no path or scope on artifact %s to scan", in.ID)
	}

	type meshResult struct {
		ArtifactID string          `json:"artifact_id"`
		Title      string          `json:"title"`
		Scope      string          `json:"scope"`
		Scan       json.RawMessage `json:"scan,omitempty"`
		Cycles     json.RawMessage `json:"cycles,omitempty"`
		Surface    json.RawMessage `json:"api_surface,omitempty"`
		Error      string          `json:"error,omitempty"`
	}

	result := meshResult{
		ArtifactID: art.ID,
		Title:      art.Title,
		Scope:      path,
	}

	scanData, err := h.locus.ScanProject(ctx, path)
	if err != nil {
		result.Error = fmt.Sprintf("locus scan_project: %v", err)
	} else {
		result.Scan = scanData
	}

	if result.Error == "" {
		if cycles, err := h.locus.GetCycles(ctx, path); err == nil {
			result.Cycles = cycles
		}
		if surface, err := h.locus.GetAPISurface(ctx, path); err == nil {
			result.Surface = surface
		}
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return text(string(data)), nil, nil
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
	scopeLabel := ""
	if showScope && node.Scope != "" {
		scopeLabel = fmt.Sprintf(" [%s]", node.Scope)
	}
	fmt.Fprintf(b, "%s%s%s%s [%s] %s\n", prefix, connector, node.ID, scopeLabel, node.Status, node.Title)
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
	return h
}
