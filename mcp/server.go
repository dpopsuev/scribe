package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dpopsuev/scribe/mcpclient"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/render"
	"github.com/dpopsuev/scribe/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// NewServer creates an MCP server exposing Scribe tools over the given store.
func NewServer(s store.Store, homeScopes []string) *sdkmcp.Server {
	srv := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "scribe", Version: "0.2.0"},
		&sdkmcp.ServerOptions{
			Instructions: "Scribe is a lean governance artifact store with native DAG support. " +
				"Use it to create, query, and manage structured artifacts (contracts, specs, sprints, rules, goals) " +
				"with parent-child trees, dependency edges, named text sections, and lifecycle status tracking. " +
				"Start with motd for context, then list_artifacts or search_artifacts to explore.",
		},
	)
	h := &handler{
		proto: protocol.New(s, nil, homeScopes),
		locus: mcpclient.New(mcpclient.DefaultLocusURL()),
	}

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "create_artifact",
		Description: "Create a new governance artifact (contract, specification, rule, etc.)",
	}, noOut(h.handleCreate))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "get_artifact",
		Description: "Retrieve a single artifact by ID",
	}, noOut(h.handleGet))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "list_artifacts",
		Description: "List artifacts with optional filters (kind, scope, status, parent, sprint). Supports group_by (status, scope, kind, sprint), sort (id, title, status, scope, kind, sprint), and limit.",
	}, noOut(h.handleList))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "set_field",
		Description: "Set a single field on an artifact. Supported fields: title, goal, scope, status, parent, priority, sprint, kind, depends_on (comma-separated), labels (comma-separated). Unknown fields are stored in the extra map.",
	}, noOut(h.handleSetField))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "search_artifacts",
		Description: "Search artifacts by substring match across title, goal, and section text. Returns matching artifacts. Supports optional scope, kind, and status filters.",
	}, noOut(h.handleSearch))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "attach_section",
		Description: "Add or replace a named text section on an artifact. Use for mermaid diagrams, architecture specs, notes, or any structured text attachment.",
	}, noOut(h.handleAttachSection))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "get_section",
		Description: "Retrieve a named section's text from an artifact.",
	}, noOut(h.handleGetSection))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "contract_tree",
		Description: "Return the parent-child tree rooted at an artifact",
	}, noOut(h.handleTree))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "set_goal",
		Description: "Set a new goal (archives any current goal for the scope) and auto-create a root delivery artifact linked via 'justifies'. Returns both the goal and its root artifact.",
	}, noOut(h.handleSetGoal))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "archive_artifact",
		Description: "Archive one or more artifacts (marks read-only). Use cascade=true to recursively archive child subtrees.",
	}, noOut(h.handleArchive))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "vacuum",
		Description: "Delete archived artifacts older than the specified number of days. Returns IDs of deleted artifacts.",
	}, noOut(h.handleVacuum))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "motd",
		Description: "Message of the day: returns due reminders, recent notes, and the current goal. Useful at session start for context.",
	}, noOut(h.handleMotd))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "drain_discover",
		Description: "List .md files under a directory for agent-driven migration into Scribe. Returns file paths, directories, and sizes. The agent reads each file and creates artifacts via create_artifact / attach_section.",
	}, noOut(h.handleDrainDiscover))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "drain_cleanup",
		Description: "Delete .md files under a directory after migration is confirmed.",
	}, noOut(h.handleDrainCleanup))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "inventory",
		Description: "Return a dashboard summary: total artifacts, counts by kind and status, active sprints, and current goals.",
	}, noOut(h.handleInventory))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "link_artifacts",
		Description: "Add a directed relationship between artifacts. Supported relations: parent_of, depends_on, justifies, implements, documents, satisfies.",
	}, noOut(h.handleLink))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "unlink_artifacts",
		Description: "Remove a directed relationship between artifacts.",
	}, noOut(h.handleUnlink))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "context_mesh",
		Description: "Query Locus for codebase context related to a governance artifact. Returns architecture components, cycles, and API surface data matching the artifact's scope.",
	}, noOut(h.handleContextMesh))

	sdkmcp.AddTool(srv, &sdkmcp.Tool{
		Name:        "detect_overlaps",
		Description: "Find active artifacts that share component labels (project:path format), indicating potential scope conflicts between contracts.",
	}, noOut(h.handleDetectOverlaps))

	return srv
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
	arts, err := h.proto.ListArtifacts(ctx, in)
	if err != nil {
		return nil, nil, err
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
	var b strings.Builder
	renderTree(tree, "", true, &b)
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
	IDs     []string `json:"ids"`
	Cascade bool     `json:"cascade,omitempty"`
}

func (h *handler) handleArchive(ctx context.Context, _ *sdkmcp.CallToolRequest, in archiveInput) (*sdkmcp.CallToolResult, any, error) {
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

type vacuumInput struct {
	Days int `json:"days,omitempty"`
}

func (h *handler) handleVacuum(ctx context.Context, _ *sdkmcp.CallToolRequest, in vacuumInput) (*sdkmcp.CallToolResult, any, error) {
	deleted, err := h.proto.Vacuum(ctx, in.Days)
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

type linkInput struct {
	ID       string   `json:"id"`
	Relation string   `json:"relation"`
	Targets  []string `json:"targets"`
}

func (h *handler) handleLink(ctx context.Context, _ *sdkmcp.CallToolRequest, in linkInput) (*sdkmcp.CallToolResult, any, error) {
	results, err := h.proto.LinkArtifacts(ctx, in.ID, in.Relation, in.Targets)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s -[%s]-> %s", in.ID, in.Relation, r.ID))
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

// --- rendering helpers ---

func renderTree(node *protocol.TreeNode, prefix string, last bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	fmt.Fprintf(b, "%s%s%s [%s] %s\n", prefix, connector, node.ID, node.Status, node.Title)
	cp := prefix
	if prefix != "" {
		if last {
			cp += "    "
		} else {
			cp += "│   "
		}
	}
	for i, ch := range node.Children {
		renderTree(ch, cp, i == len(node.Children)-1, b)
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
