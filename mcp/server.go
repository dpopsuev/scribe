package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	batterymcp "github.com/dpopsuev/battery/mcp"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	toolNameArtifact = "artifact"
	toolNameGraph    = "graph"
	toolNameAdmin    = "admin"
	actionLink       = "link"
)

var graphActions = map[string]bool{
	actionLink: true, "analyze": true, "synonym": true,
}

var adminActions = map[string]bool{
	"lint": true, "synthesize": true, "history": true, "hygiene": true, "dashboard": true, "changelog": true, "status": true, "triage": true,
	"fold_campaign": true, "reparent_children": true,
}

// baseInstructions is the core MCP server instructions shown to clients.
const baseInstructions = "Labeled Artifact Graph. " +
	"SCHEMA: artifact(action=query, kind=label_definition, scope=_schema) to learn available kinds and labels. " +
	"ORGANIZE: project: labels map to git repos (auto-detected). " +
	"For grouping related artifacts within a project, use parent_of edges and kind:knowledge.context as containers — NOT sub-projects. " +
	"Use area:/context:/domain: labels for cross-cutting concerns. " +
	"DISCOVER: schema(kind=X) shows valid relations, sections, and lifecycle for any kind. " +
	"RECOVERY: if connection fails, check that the Scribe server is running (scribe serve --transport http --addr :8080)"

// buildInstructions returns the instructions string for a session.
// Workspace context is resolved lazily via the onInitialized handler
// after the client connects, so the static instructions never show
// the WORKSPACE UNSET warning (it was already stale by the time the
// agent read it).
func buildInstructions(_ bool) string {
	return baseInstructions
}

// NewServer creates an MCP server exposing Scribe tools over the given store.
// stdioLabels carries workspace labels detected from the server's own CWD
// (stdio transport only). For HTTP transport, labels are set per-session via
// the initialize handler.
func NewServer(svc *service.Service, vocab []string, version string, stdioLabels ...string) (*sdkmcp.Server, *Registry) {
	reg := newRegistry()
	sid := service.NewSessionID()
	var store parchment.Store
	if svc.Proto != nil {
		store = svc.Proto.Store()
	}
	if svc.ReadLog == nil {
		svc.ReadLog = loadReadLog(context.Background(), store, svc.Proto, sid)
		svc.SessionID = sid
	}

	wLabels := stdioLabels
	wConfigured := len(wLabels) > 0

	h := &handler{
		proto: svc.Proto,
		svc:   svc,

		version:             version,
		homeScopes:          svc.HomeScopes,
		workspaceLabels:     wLabels,
		workspaceConfigured: wConfigured,
		recordSession:       svc.RecordSession,
	}

	// Build SDK server directly with InitializedHandler in options.
	sdk := sdkmcp.NewServer(
		&sdkmcp.Implementation{Name: "scribe", Version: version},
		&sdkmcp.ServerOptions{
			Instructions:       buildInstructions(wConfigured),
			InitializedHandler: h.onInitialized,
		},
	)
	destructiveHint := true

	// --- artifact tool: CRUD + query (the 80% path) ---
	artifactDesc := "Labeled Artifact Graph — CRUD + query. " +
		"SCHEMA: flat input union — pass only fields for the chosen action (hosts list every kwarg). " +
		"FIND: query(query=) for FTS; query(ranked=true, query=) for scored recall; query(mode=semantic, query=) for vector similarity; query(mode=working_set) for session+ready+recent+hygiene. " +
		"READ: get(id=) full artifact; get(id=, format=context) for graph context. " +
		"WRITE: create, set (single field), update (sections/extra patch). " +
		"PLAN: query(id=, sort=topo) for dependency order; query(id=, sort=topo, unblocked=true) for ready queue. " +
		"ORGANIZE: project: labels map to git repos. Use parent_of edges and kind:knowledge.context as containers. " +
		"DISCOVER: schema(kind=X) shows valid relations, sections, and lifecycle for any kind. " +
		"SORT: id (default), title, status, scope, kind, sprint, priority, topo. " +
		"TIME FILTERS: created_after/before (first creation), updated_after/before (last change), inserted_after/before (DB row write). All RFC3339, e.g. 2026-07-01T00:00:00Z. " +
		"PAGINATION: response includes next_cursor — pass as cursor= to continue."
	var artifactSchema any
	_ = json.Unmarshal(schemaFor[artifactInput](), &artifactSchema)
	patchSchemaFromRegistry(artifactSchema, h)
	sdk.AddTool(&sdkmcp.Tool{
		Name:        toolNameArtifact,
		Title:       "Artifact",
		Description: artifactDesc,
		InputSchema: artifactSchema,
		Annotations: &sdkmcp.ToolAnnotations{DestructiveHint: &destructiveHint},
	}, bindHandler(h.handleArtifact))
	reg.register(ToolMeta{
		Name: toolNameArtifact, Description: artifactDesc,
		Keywords:   []string{"create", "get", "query", "set", "update", "delete", "artifact"},
		Categories: []string{"crud"},
	})

	// --- graph tool: edge management + analysis ---
	graphDesc := "Artifact graph — relationships + analysis. " +
		"EDGES: link(id=, relation=, targets=[]) to add; link(mode=unlink) to remove; link(edges=[{from,relation,to}]) for bulk. " +
		"ANALYZE: analyze(mode=fan) for fan-in/fan-out; analyze(mode=pagerank) for centrality; " +
		"analyze(mode=co_citation, id=) for related; analyze(mode=paths, from=, to=) for shortest path. " +
		"SYNONYMS: synonym(mode=add, id=, alias=) to register; synonym(mode=resolve, term=) to look up."
	var graphSchema any
	_ = json.Unmarshal(schemaFor[graphInput](), &graphSchema)
	patchSchemaFromRegistry(graphSchema, h)
	sdk.AddTool(&sdkmcp.Tool{
		Name:        toolNameGraph,
		Title:       "Graph",
		Description: graphDesc,
		InputSchema: graphSchema,
	}, bindHandler(h.handleGraph))
	reg.register(ToolMeta{
		Name: toolNameGraph, Description: graphDesc,
		Keywords:   []string{"link", "unlink", "edge", "analyze", "synonym", "graph"},
		Categories: []string{"graph"},
	})

	// --- admin tool: ops + introspection ---
	adminDesc := "Artifact admin — ops + introspection. " +
		"HEALTH: hygiene(scope=) for zombie campaigns, stale tasks, orphans. " +
		"LINT: lint(id=) for consistency checks. " +
		"SYNTHESIZE: synthesize(id=) to auto-generate content. " +
		"HISTORY: history(id=) for change log. " +
		"CHANGELOG: changelog(id=) for field-level revision diffs. " +
		"DASHBOARD: dashboard(scope=) for project overview. " +
		"STATUS: status() for server version, DB size, scopes, embeddings. " +
		"TRIAGE: triage() for campaign health, stale work, orphans, lifecycle mismatches."
	var adminSchema any
	_ = json.Unmarshal(schemaFor[adminInput](), &adminSchema)
	sdk.AddTool(&sdkmcp.Tool{
		Name:        toolNameAdmin,
		Title:       "Admin",
		Description: adminDesc,
		InputSchema: adminSchema,
	}, bindHandler(h.handleAdmin))
	reg.register(ToolMeta{
		Name: toolNameAdmin, Description: adminDesc,
		Keywords:   []string{"lint", "hygiene", "dashboard", "history", "synthesize", "changelog", "admin"},
		Categories: []string{"admin"},
	})

	return sdk, reg
}

// ToolRegistry returns a populated directive registry without requiring
// a database connection. Useful for CLI introspection (scribe tools).
func ToolRegistry() *Registry {
	_, reg := NewServer(service.New(nil, nil, nil), nil, "dev")
	return reg
}

// NewServerFromStore constructs a Server from a raw store + config — used by
// tests that open a store directly rather than going through service.Open.
func NewServerFromStore(s parchment.Store, homeScopes []string, idc parchment.ProtocolConfig, version string, workspaceLabels ...string) (*sdkmcp.Server, *Registry) {
	proto := parchment.New(s, nil, homeScopes, nil, idc)
	svc := service.New(proto, nil, homeScopes)
	svc.Version = version
	return NewServer(svc, nil, version, workspaceLabels...)
}

type handler struct {
	proto *parchment.Protocol
	svc   *service.Service

	version             string
	homeScopes          []string // default scopes; narrowable at runtime
	workspaceLabels     []string // context labels stamped on every artifact this session
	workspaceConfigured bool     // true once workspace context has been set
	workspaceFromClient bool     // true when client sent Meta.workspace (vs server CWD fallback)
	workspaceWarned     bool     // true after first workspace-unset warning (suppress repeats)
	recordSession       bool     // when true, create agent.session/agent.turn artifacts
	sessionArtifactID   string   // lazily created agent.session artifact

	clientHarness string // MCP client name from initialize (e.g., "claude-code", "cursor")
	clientVersion string // MCP client version
}

// --- consolidated input types ---

type artifactInput struct {
	Action string `json:"action" jsonschema:"required,create | get | query | set | update | delete | attach | detach | recent | brief | schema | help | kernel_create | kernel_confirm | kernel_reject | export | claim | release | handoff"`

	ID     string `json:"id,omitempty"`
	Target string `json:"target,omitempty" jsonschema:"single target ID for link mode=replace; or new parent ID for set(field=parent)"`
	Kind   string `json:"kind,omitempty" jsonschema:"task, spec, bug, goal, campaign, doc, ref, need, decision"`
	Scope  string `json:"scope,omitempty"`

	Title     string              `json:"title,omitempty"`
	Goal      string              `json:"goal,omitempty"`
	Parent    string              `json:"parent,omitempty"`
	Status    string              `json:"status,omitempty" jsonschema:"work.draft, work.active, work.blocked, work.complete, note.fleeting, note.mature, note.evergreen, decision.proposed, decision.accepted, decision.rejected, decision.deferred"`
	Priority  string              `json:"priority,omitempty" jsonschema:"none, low, medium, high, critical"`
	DependsOn []string            `json:"depends_on,omitempty"`
	Labels    []string            `json:"labels,omitempty"`
	Links     map[string][]string `json:"links,omitempty"`
	Extra     map[string]any      `json:"extra,omitempty"`
	Sections  []map[string]string `json:"sections,omitempty" jsonschema:"[{name, text}, ...]"`

	Format  string   `json:"format,omitempty" jsonschema:"summary or full (default); export: markdown"`
	GroupBy string   `json:"group_by,omitempty" jsonschema:"status, scope, kind, sprint"`
	Sort    string   `json:"sort,omitempty" jsonschema:"id, title, status, scope, kind, sprint, priority, topo"`
	Limit   int      `json:"limit,omitempty"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"opaque pagination cursor returned as next_cursor in previous response — pass verbatim to continue"`
	Count   bool     `json:"count,omitempty"`
	Ranked  bool     `json:"ranked,omitempty" jsonschema:"scored FTS with kind and recency weighting"`
	Mode    string   `json:"mode,omitempty" jsonschema:"fts (default) | semantic | hybrid | working_set"`
	Session string   `json:"session,omitempty" jsonschema:"scope results to a single agent session ID"`
	Top     int      `json:"top,omitempty" jsonschema:"N most relevant by status+priority+recency"`
	Fields  []string `json:"fields,omitempty" jsonschema:"id, kind, scope, status, title, parent, priority"`
	Query   string   `json:"query,omitempty" jsonschema:"substring search across title, goal, sections"`

	TitleContains  string   `json:"title_contains,omitempty"`
	CreatedAfter   string   `json:"created_after,omitempty" jsonschema:"RFC3339 lower bound on created_at (when the artifact was first created), e.g. 2026-07-01T00:00:00Z"`
	CreatedBefore  string   `json:"created_before,omitempty" jsonschema:"RFC3339 upper bound on created_at"`
	UpdatedAfter   string   `json:"updated_after,omitempty" jsonschema:"RFC3339 lower bound on updated_at (last field/section/status change)"`
	UpdatedBefore  string   `json:"updated_before,omitempty" jsonschema:"RFC3339 upper bound on updated_at"`
	InsertedAfter  string   `json:"inserted_after,omitempty" jsonschema:"RFC3339 lower bound on inserted_at (when the row was first written to the DB — immutable)"`
	InsertedBefore string   `json:"inserted_before,omitempty" jsonschema:"RFC3339 upper bound on inserted_at"`
	IDPrefix       string   `json:"id_prefix,omitempty"`
	Sprint         string   `json:"sprint,omitempty"`
	ExcludeKind    string   `json:"exclude_kind,omitempty"`
	ExcludeStatus  string   `json:"exclude_status,omitempty"`
	LabelsOr       []string `json:"labels_or,omitempty"`
	ExcludeLabels  []string `json:"exclude_labels,omitempty"`
	ExcerptChars   int      `json:"excerpt_chars,omitempty" jsonschema:"include first N characters of most relevant section per result (0=off)"`
	IncludeCode    bool     `json:"include_code,omitempty" jsonschema:"working_set/hygiene: include index-severity findings"`
	OutDir         string   `json:"out_dir,omitempty" jsonschema:"export: output directory for scope export"`
	Agent          string   `json:"agent,omitempty" jsonschema:"claim/release/handoff: agent identity"`
	TTLSeconds     int      `json:"ttl_seconds,omitempty" jsonschema:"claim: lease duration in seconds (default 3600)"`
	FromSession    string   `json:"from_session,omitempty" jsonschema:"handoff: source session"`
	ToSession      string   `json:"to_session,omitempty" jsonschema:"handoff: destination session"`
	Evidence       []string `json:"evidence,omitempty" jsonschema:"handoff: evidence artifact IDs"`
	ArtifactID     string   `json:"artifact_id,omitempty" jsonschema:"handoff: primary artifact being handed off"`

	Field        string `json:"field,omitempty" jsonschema:"title, goal, scope, status, parent, priority, kind, depends_on, labels"`
	Value        string `json:"value,omitempty" jsonschema:"new value (comma-separated for list fields)"`
	Force        bool   `json:"force,omitempty" jsonschema:"bypass transition validation — allows status moves that would normally be blocked by lifecycle rules"`
	BypassGuards bool   `json:"bypass_guards,omitempty" jsonschema:"skip rule evaluator guards entirely (use for migrations or emergency fixes)"`
	RenameID     bool   `json:"rename_id,omitempty" jsonschema:"when field is scope: atomically renames the artifact ID to match the new scope key; result.new_id contains the new ID; all edge references cascade automatically"`

	Alias      string `json:"alias,omitempty" jsonschema:"alias for synonym add/remove"`
	Term       string `json:"term,omitempty" jsonschema:"term for synonym resolve"`
	From       string `json:"from,omitempty" jsonschema:"source artifact for paths mode"`
	To         string `json:"to,omitempty" jsonschema:"target artifact for paths mode"`
	MinShared  int    `json:"min_shared,omitempty" jsonschema:"minimum shared neighbors for co_citation/coupling"`
	MaxDepth   int    `json:"max_depth,omitempty" jsonschema:"max hops for path search"`
	Iterations int    `json:"iterations,omitempty" jsonschema:"iterations for pagerank (default 20)"`

	Name           string   `json:"name,omitempty"`
	Text           string   `json:"text,omitempty"`
	Body           string   `json:"body,omitempty"`
	SectionFilter  []string `json:"section_filter,omitempty"`
	SectionsDelete []string `json:"sections_delete,omitempty" jsonschema:"section names to remove"`
	Against        string   `json:"against,omitempty"`

	IDs     []string `json:"ids,omitempty"`
	Cascade bool     `json:"cascade,omitempty" jsonschema:"apply status transition recursively to all children"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"simulate the operation and return what would change without writing anything"`

	Patch        map[string]string `json:"patch,omitempty" jsonschema:"{field: value} pairs for batch_update"`
	Artifacts    []map[string]any  `json:"artifacts,omitempty"`
	SkipHooks    bool              `json:"skip_hooks,omitempty"`
	CloneFrom    string            `json:"clone_from,omitempty" jsonschema:"create: source artifact ID to clone from"`
	IncludeEdges bool              `json:"include_edges,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
	Prefix       string            `json:"prefix,omitempty"`

	Relation  string      `json:"relation,omitempty" jsonschema:"parent_of, depends_on, follows, justifies, implements, documents, blocks, duplicates, relates_to, clones, mentions, tested_by, supersedes, cites, elaborates, contradicts, traces_to, calls, explains, causes, resolves"` //nolint:misspell // relation names are domain terms // synthesises intentionally omitted (British spelling causes linter noise)
	Weight    float64     `json:"weight,omitempty" jsonschema:"edge coupling strength (0.0 = boolean, 1.0 = max; default 0)"`
	Direction string      `json:"direction,omitempty" jsonschema:"outbound (default) or inbound"`
	Depth     int         `json:"depth,omitempty" jsonschema:"tree/briefing: max depth; query sort=topo: max results when unblocked=true"`
	Unblocked bool        `json:"unblocked,omitempty" jsonschema:"query sort=topo: return only unblocked ready tasks"`
	Targets   []string    `json:"targets,omitempty"`
	OldTarget string      `json:"old_target,omitempty"`
	Edges     []edgeInput `json:"edges,omitempty" jsonschema:"link/unlink bulk mode: [{from, relation, to}]"`

	// Attachment fields — used by attach and detach actions.
	// Name (shared with section operations) is the attachment filename.
	ContentType string `json:"content_type,omitempty" jsonschema:"MIME type for attach action — e.g. image/png, image/svg+xml"`
	Data        string `json:"data,omitempty"         jsonschema:"base64-encoded binary content for attach action"`

	// Kernel fields — used by kernel_create, kernel_confirm, kernel_reject actions.
	PointerID string `json:"pointer_id,omitempty" jsonschema:"source pointer artifact ID (kernel_create)"`
	Content   string `json:"content,omitempty"    jsonschema:"kernel text content (kernel_create)"`
	Line      int    `json:"line,omitempty"       jsonschema:"1-based line in section (kernel_create selector)"`
	Anchor    string `json:"anchor,omitempty"     jsonschema:"heading anchor (kernel_create selector)"`
	Section   string `json:"section,omitempty"    jsonschema:"section name (kernel_create selector)"`
}

type edgeInput struct {
	From     string `json:"from" jsonschema:"source artifact ID"`
	Relation string `json:"relation" jsonschema:"edge type"`
	To       string `json:"to" jsonschema:"target artifact ID"`
}

// graphInput is the schema for the graph tool — edge management + analysis.
type graphInput struct {
	Action string `json:"action" jsonschema:"required,link | analyze | synonym"`

	ID        string      `json:"id,omitempty"`
	Target    string      `json:"target,omitempty" jsonschema:"single target ID for link mode=replace"`
	Relation  string      `json:"relation,omitempty" jsonschema:"parent_of, depends_on, follows, justifies, implements, documents, blocks, duplicates, relates_to, clones, mentions, tested_by, supersedes, cites, elaborates, contradicts, traces_to, calls, explains, causes, resolves"` //nolint:misspell // relation names are domain terms
	Weight    float64     `json:"weight,omitempty" jsonschema:"edge coupling strength (0.0 = boolean, 1.0 = max; default 0)"`
	Direction string      `json:"direction,omitempty" jsonschema:"outbound (default) or inbound"`
	Depth     int         `json:"depth,omitempty" jsonschema:"tree/briefing: max depth"`
	Unblocked bool        `json:"unblocked,omitempty" jsonschema:"return only unblocked ready tasks"`
	Targets   []string    `json:"targets,omitempty"`
	OldTarget string      `json:"old_target,omitempty"`
	Edges     []edgeInput `json:"edges,omitempty" jsonschema:"link/unlink bulk mode: [{from, relation, to}]"`
	Mode      string      `json:"mode,omitempty" jsonschema:"link: unlink; analyze: fan, pagerank, co_citation, paths, coupling"`
	Labels    []string    `json:"labels,omitempty"`

	// Analyze fields.
	From       string `json:"from,omitempty" jsonschema:"source artifact for paths mode"`
	To         string `json:"to,omitempty" jsonschema:"target artifact for paths mode"`
	MinShared  int    `json:"min_shared,omitempty" jsonschema:"minimum shared neighbors for co_citation/coupling"`
	MaxDepth   int    `json:"max_depth,omitempty" jsonschema:"max hops for path search"`
	Iterations int    `json:"iterations,omitempty" jsonschema:"iterations for pagerank (default 20)"`

	// Synonym fields.
	Alias string `json:"alias,omitempty" jsonschema:"alias for synonym add/remove"`
	Term  string `json:"term,omitempty" jsonschema:"term for synonym resolve"`
}

// adminInput is the schema for the admin tool — ops + introspection.
type adminInput struct {
	Action string `json:"action" jsonschema:"required,lint | synthesize | history | hygiene | dashboard | changelog"`

	ID      string `json:"id,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Against string `json:"against,omitempty"`
	Query   string `json:"query,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Format  string `json:"format,omitempty" jsonschema:"summary or full (default)"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	DryRun  bool   `json:"dry_run,omitempty"`
	Cascade bool   `json:"cascade,omitempty"`
}

// --- dispatchers ---

// bindHandler bridges a typed Scribe handler to sdkmcp.ToolHandler.
func bindHandler[In any](h func(context.Context, *sdkmcp.CallToolRequest, In) (*sdkmcp.CallToolResult, any, error)) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (res *sdkmcp.CallToolResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				errRes := text(fmt.Sprintf("panic: %v", r))
				errRes.IsError = true
				res = errRes
			}
		}()
		var in In
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			if errRes := batterymcp.UnmarshalWithHints(req.Params.Arguments, &in, arrayTypeHints); errRes != nil {
				return errRes, nil
			}
		}
		req.Params = &sdkmcp.CallToolParamsRaw{Arguments: req.Params.Arguments}
		out, _, err := h(ctx, req, in)
		if err != nil {
			errRes := text(err.Error())
			errRes.IsError = true
			return errRes, nil
		}
		return out, nil
	}
}

func text(s string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: s},
		},
	}
}

func schemaFor[T any]() json.RawMessage {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic("scribe: schema for input: " + err.Error())
	}
	data, err := json.Marshal(s)
	if err != nil {
		panic("scribe: marshal schema: " + err.Error())
	}
	return data
}

const hintIDArray = `JSON array of strings — e.g. ["TASK-1", "TASK-2"]`
const hintLabelArray = `JSON array of strings — e.g. ["label1", "label2"]`

// arrayTypeHints maps struct field names to their expected JSON wire format.
// Used by unmarshalInput to produce actionable error messages when the caller
// passes the wrong type (e.g. a comma-separated string where a JSON array is required).
var arrayTypeHints = batterymcp.TypeHints{
	"depends_on":     hintIDArray,
	"ids":            hintIDArray,
	"labels":         hintLabelArray,
	"labels_or":      hintLabelArray,
	"exclude_labels": hintLabelArray,
	"fields":         `JSON array of strings — e.g. ["id", "title", "status"]`,
	"section_filter": `JSON array of strings — e.g. ["summary", "source"]`,
	"targets":        `JSON array of strings — e.g. ["REF-1", "REF-2"]`,
	"links":          `JSON object mapping relation→[]string — e.g. {"documents": ["REF-1"]}`,
	"sections":       `JSON array of {"name":"<name>","text":"<body>"} objects — e.g. [{"name":"summary","text":"..."}]`,
	"edges":          `JSON array of {"from":"X","relation":"r","to":"Y"} objects`,
	"extra":          `JSON object — e.g. {"key": "value"}`,
	"patch":          `JSON object mapping field→value — e.g. {"status": "active"}`,
}

// patchSchemaFromRegistry replaces static jsonschema field descriptions that
// enumerate domain vocabulary (kind, status) with live values from the
// Protocol's LabelTrait registry. This makes the MCP schema self-updating:
// adding a new kind YAML automatically surfaces in the tool schema.
func patchSchemaFromRegistry(schema any, h *handler) {
	obj, ok := schema.(map[string]any)
	if !ok || h.proto == nil {
		return
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		return
	}

	// kind: all registered kind names from labelTraits.
	if kindProp, ok := props["kind"].(map[string]any); ok {
		kinds := h.proto.AllKinds()
		if len(kinds) > 0 {
			kindProp["description"] = strings.Join(kinds, ", ")
		}
	}

	// status: all registered status labels (domain lifecycle statuses).
	if statusProp, ok := props["status"].(map[string]any); ok {
		statuses := h.proto.AllStatuses()
		if len(statuses) > 0 {
			statusProp["description"] = strings.Join(statuses, ", ")
		}
	}

	// relation: all registered relation names from schema.
	if relationProp, ok := props["relation"].(map[string]any); ok {
		relations := h.proto.RegisteredRelations()
		if len(relations) > 0 {
			relationProp["description"] = strings.Join(relations, ", ")
		}
	}
}
