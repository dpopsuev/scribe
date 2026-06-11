package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	battmcp "github.com/dpopsuev/battery/mcp"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/directive"
	"github.com/dpopsuev/scribe/service"
	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// scribeInstructions is the MCP server instructions shown to clients.
// Kept deliberately minimal — the schema is self-describing.
// Query artifact(action=list, kind=kind_definition, scope=_schema) to learn when to create each kind.
// Query artifact(action=list, kind=edge_type_definition, scope=_schema) to learn what each relation means.
// Query artifact(action=list, kind=label_definition, scope=_schema) to learn when to apply each label.
const scribeInstructions = "Artifact graph + knowledge vault. " +
	"SESSION START: admin(action=brief) — discloses scope, active goal, open bugs. " +
	"CAPABILITIES: admin(action=capabilities) — structured map of every action, option, and field; call this when unsure what's possible (e.g. rename_id, dry_run, cascade). " +
	"SCHEMA: the _schema scope is self-describing — query kind_definition, edge_type_definition, label_definition artifacts to learn when and how to use each construct."

// NewServer creates an MCP server exposing Scribe tools over the given store.
// Returns both the server and a directive registry for CLI introspection.
// Built on battery/mcp framework — auto-Observable, panic recovery, result helpers.
// svc must be constructed via service.Open — both CLI and MCP use the same factory
// so Protocol configuration, homeScopes, and schema are identical across surfaces.
func NewServer(svc *service.Service, vocab []string, version string) (*sdkmcp.Server, *directive.Registry) {
	batt := battmcp.NewServer("scribe", version).
		WithInstructions(scribeInstructions)

	reg := directive.New()
	sid := newSessionID()
	var store parchment.Store
	if svc.Proto != nil {
		store = svc.Proto.Store()
	}
	if svc.ReadLog == nil {
		svc.ReadLog = loadReadLog(context.Background(), store, svc.Proto, sid)
		svc.SessionID = sid
	}
	h := &handler{
		proto:       svc.Proto,
		svc:         svc,
		snapshotter: svc.Snapshotter,
		version:     version,
		homeScopes:  svc.HomeScopes,
	}

	// Build SDK directly for full MCP 2025 spec support (Title, Annotations).
	sdk := batt.SDK()
	destructiveHint := true

	artifactDesc := "CRUD + search + graph for work (task/spec/bug/goal) and knowledge (note/concept/source) artifacts. " +
		"FIND: list(query=) for keyword FTS; recall(query=, top=10) for ranked FTS with kind/recency scoring; list(semantic=true, query=) for vector similarity (requires embeddings). " +
		"READ: get(id=) full artifact; get_section(id=, name=) for one section only (cheaper). " +
		"LIST: always add scope/kind/status or top=N — bare list returns ALL artifacts and burns context. " +
		"WRITE: create, set, attach_section. " +
		"GRAPH: briefing(id=) — full edge-aware context chain; tree(id=) — children; link/unlink — edges; topo_sort — dependency order; impact — blast radius. " +
		"ORIENT: orient for vault map (call after brief); catalog for full inventory. " +
		"STASH: get(stash_id=) to inspect a failed-create stash; create(stash_id=) to promote it."
	var artifactSchema any
	_ = json.Unmarshal(schemaFor[artifactInput](), &artifactSchema)
	sdk.AddTool(&sdkmcp.Tool{
		Name:        "artifact",
		Title:       "Artifact Manager",
		Description: artifactDesc,
		InputSchema: artifactSchema,
		Annotations: &sdkmcp.ToolAnnotations{DestructiveHint: &destructiveHint},
	}, bindHandler(h.handleArtifact))
	reg.Register(directive.ToolMeta{
		Name: "artifact", Description: artifactDesc,
		Keywords:   []string{"create", "get", "list", "set", "artifact", "section", "tree", "briefing", "topo_sort", "link", "unlink", "move", "impact"},
		Categories: []string{"crud", "graph"},
	})

	// admin tool
	adminDesc := "Session bootstrap and housekeeping. " +
		"CALL FIRST: brief — returns scope, open bugs, active goal, and memory surface. " +
		"CONTINUATION: brief(since=<RFC3339>) for delta only — returns only what changed since that timestamp. " +
		"brief(compact=true) for a one-line status summary. " +
		"dashboard for scope health and staleness. vacuum(dry_run=true) before pruning. detect for orphans/overlaps."
	var adminSchema any
	_ = json.Unmarshal(schemaFor[adminInput](), &adminSchema)
	sdk.AddTool(&sdkmcp.Tool{
		Name:        "admin",
		Title:       "Workspace Admin",
		Description: adminDesc,
		InputSchema: adminSchema,
		Annotations: &sdkmcp.ToolAnnotations{DestructiveHint: &destructiveHint},
	}, bindHandler(h.handleAdmin))
	reg.Register(directive.ToolMeta{
		Name: "admin", Description: adminDesc,
		Keywords:   []string{"brief", "dashboard", "goal", "vacuum", "detect", "orphan"},
		Categories: []string{"lifecycle", "maintenance"},
	})

	return sdk, reg
}

// ToolRegistry returns a populated directive registry without requiring
// a database connection. Useful for CLI introspection (scribe tools).
func ToolRegistry() *directive.Registry {
	_, reg := NewServer(service.New(nil, nil, nil), nil, "dev")
	return reg
}

// NewServerFromStore constructs a Server from a raw store + config — used by
// tests that open a store directly rather than going through service.Open.
func NewServerFromStore(s parchment.Store, homeScopes []string, idc parchment.ProtocolConfig, version string) (*sdkmcp.Server, *directive.Registry) {
	proto := parchment.New(s, nil, homeScopes, nil, idc)
	svc := service.New(proto, nil, homeScopes)
	return NewServer(svc, nil, version)
}

type handler struct {
	proto       *parchment.Protocol
	svc         *service.Service
	snapshotter *parchment.Snapshotter
	version     string
	homeScopes  []string // default scopes; narrowable at runtime via admin(set_scope)
}

// --- consolidated input types ---

type artifactInput struct {
	Action string `json:"action" jsonschema:"required,create | get | list | set | update | retire | attach_section | detach_section | bulk_section_update | diff | recall | orient | tree | briefing | link | unlink | topo_sort | replace | catalog | impact"`

	ID     string `json:"id,omitempty"`
	Target string `json:"target,omitempty" jsonschema:"new parent ID (move)"`
	Kind   string `json:"kind,omitempty" jsonschema:"task, spec, bug, goal, campaign, doc, ref, need, decision"`
	Scope  string `json:"scope,omitempty"`

	Title     string              `json:"title,omitempty"`
	Goal      string              `json:"goal,omitempty"`
	Parent    string              `json:"parent,omitempty"`
	Status    string              `json:"status,omitempty" jsonschema:"work.draft, work.active, work.blocked, work.complete, note.fleeting, note.mature, note.evergreen, decision.proposed, decision.accepted, decision.rejected, decision.deferred, retired, archived"`
	Priority  string              `json:"priority,omitempty" jsonschema:"none, low, medium, high, critical"`
	DependsOn []string            `json:"depends_on,omitempty"`
	Labels    []string            `json:"labels,omitempty"`
	Links     map[string][]string `json:"links,omitempty"`
	Extra     map[string]any      `json:"extra,omitempty"`
	Sections  []map[string]string `json:"sections,omitempty" jsonschema:"[{name, text}, ...]"`

	Format   string   `json:"format,omitempty" jsonschema:"summary or full (default)"`
	GroupBy  string   `json:"group_by,omitempty" jsonschema:"status, scope, kind, sprint"`
	Sort     string   `json:"sort,omitempty" jsonschema:"id, title, status, scope, kind"`
	Limit    int      `json:"limit,omitempty"`
	Cursor   string   `json:"cursor,omitempty" jsonschema:"pagination cursor from previous list response"`
	Count    bool     `json:"count,omitempty"`
	Ranked   bool     `json:"ranked,omitempty" jsonschema:"scored FTS with kind and recency weighting"`
	Semantic bool     `json:"semantic,omitempty" jsonschema:"deprecated: use mode=semantic"`
	Mode     string   `json:"mode,omitempty" jsonschema:"fts (default) | semantic | hybrid"`
	Session  string   `json:"session,omitempty" jsonschema:"scope results to a single agent session ID"`
	Top      int      `json:"top,omitempty" jsonschema:"N most relevant by status+priority+recency"`
	Fields   []string `json:"fields,omitempty" jsonschema:"id, kind, scope, status, title, parent, priority"`
	Query    string   `json:"query,omitempty" jsonschema:"substring search across title, goal, sections"`

	TitleContains  string   `json:"title_contains,omitempty"`
	CreatedAfter   string   `json:"created_after,omitempty"`
	CreatedBefore  string   `json:"created_before,omitempty"`
	UpdatedAfter   string   `json:"updated_after,omitempty"`
	UpdatedBefore  string   `json:"updated_before,omitempty"`
	InsertedAfter  string   `json:"inserted_after,omitempty"`
	InsertedBefore string   `json:"inserted_before,omitempty"`
	IDPrefix       string   `json:"id_prefix,omitempty"`
	Sprint         string   `json:"sprint,omitempty"`
	ExcludeKind    string   `json:"exclude_kind,omitempty"`
	ExcludeStatus  string   `json:"exclude_status,omitempty"`
	LabelsOr       []string `json:"labels_or,omitempty"`
	ExcludeLabels  []string `json:"exclude_labels,omitempty"`

	Field        string `json:"field,omitempty" jsonschema:"title, goal, scope, status, parent, priority, kind, depends_on, labels"`
	Value        string `json:"value,omitempty" jsonschema:"new value (comma-separated for list fields)"`
	Force        bool   `json:"force,omitempty" jsonschema:"bypass transition validation — allows status moves that would normally be blocked by lifecycle rules"`
	BypassGuards bool   `json:"bypass_guards,omitempty" jsonschema:"skip rule evaluator guards entirely (use for migrations or emergency fixes)"`
	RenameID     bool   `json:"rename_id,omitempty" jsonschema:"when field is scope: atomically renames the artifact ID to match the new scope key; result.new_id contains the new ID; all edge references cascade automatically"`

	Name           string   `json:"name,omitempty"`
	Text           string   `json:"text,omitempty"`
	Body           string   `json:"body,omitempty"`
	SectionFilter  []string `json:"section_filter,omitempty"`
	SectionsDelete []string `json:"sections_delete,omitempty" jsonschema:"section names to remove"`
	Against        string   `json:"against,omitempty"`

	IDs     []string `json:"ids,omitempty"`
	Cascade bool     `json:"cascade,omitempty" jsonschema:"apply status transition recursively to all children (retire/archive tree operations)"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"simulate the operation and return what would change without writing anything"`

	Patch        map[string]string `json:"patch,omitempty" jsonschema:"{field: value} pairs for batch_update"`
	Artifacts    []map[string]any  `json:"artifacts,omitempty"`
	SkipHooks    bool              `json:"skip_hooks,omitempty"`
	StashID      string            `json:"stash_id,omitempty" jsonschema:"get: inspect stash; create: promote stash"`
	CloneFrom    string            `json:"clone_from,omitempty" jsonschema:"create: source artifact ID to clone from"`
	IncludeEdges bool              `json:"include_edges,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
	Prefix       string            `json:"prefix,omitempty"`

	Relation  string      `json:"relation,omitempty" jsonschema:"parent_of, depends_on, follows, justifies, implements, documents"`
	Weight    float64     `json:"weight,omitempty" jsonschema:"edge coupling strength (0.0 = boolean, 1.0 = max; default 0)"`
	Direction string      `json:"direction,omitempty" jsonschema:"outbound (default) or inbound"`
	Depth     int         `json:"depth,omitempty" jsonschema:"tree/briefing: max depth; topo_sort: max results when unblocked=true"`
	Unblocked bool        `json:"unblocked,omitempty" jsonschema:"topo_sort: return only unblocked ready tasks"`
	Targets   []string    `json:"targets,omitempty"`
	OldTarget string      `json:"old_target,omitempty"`
	Edges     []edgeInput `json:"edges,omitempty" jsonschema:"link/unlink bulk mode: [{from, relation, to}]"`
}

type edgeInput struct {
	From     string `json:"from" jsonschema:"source artifact ID"`
	Relation string `json:"relation" jsonschema:"edge type"`
	To       string `json:"to" jsonschema:"target artifact ID"`
}

// knowledgeInput carries the two fields used by handleIngestSession.
// This is a minimal survivor of the old knowledge tool; all other fields
// were removed when the knowledge tool was merged into admin.
type knowledgeInput struct {
	Path  string `json:"path,omitempty"`
	Scope string `json:"scope,omitempty"`
}

type adminInput struct {
	Action  string `json:"action" jsonschema:"required,brief | capabilities | changelog | dashboard | snapshot | set_goal | detect | correlate | ingest_session | decision | set_scope | set_scope_labels | context_read | session"`
	Compact bool   `json:"compact,omitempty" jsonschema:"minimal output for repeat calls (brief)"`

	SnapshotAction string `json:"snapshot_action,omitempty" jsonschema:"create, list, diff, or restore"`
	SnapshotName   string `json:"snapshot_name,omitempty" jsonschema:"snapshot label (create) or key (diff)"`

	StaleDays int    `json:"stale_days,omitempty" jsonschema:"days without update to consider stale (dashboard)"`
	Since     string `json:"since,omitempty" jsonschema:"RFC 3339 lower bound (brief, changelog)"`

	Title string `json:"title,omitempty"`
	Scope string `json:"scope,omitempty"`
	Kind  string `json:"kind,omitempty" jsonschema:"artifact kind filter or root kind for set_goal"`

	Target string `json:"target,omitempty"`

	Check   string `json:"check,omitempty" jsonschema:"orphans (default), overlaps, knowledge, eviction, or all"`
	Status  string `json:"status,omitempty"`
	Project string `json:"project,omitempty"`

	Labels []string `json:"labels,omitempty"`

	// correlate: freeform text containing artifact IDs and delivery signals
	Evidence string `json:"evidence,omitempty" jsonschema:"freeform text with artifact IDs (correlate)"`

	// ingest_session: filesystem path to a .jsonl session file or directory
	Path string `json:"path,omitempty" jsonschema:"path to .jsonl session file or directory (ingest_session)"`
}

// --- dispatchers ---

func unmarshalInput[In any](raw []byte, in *In) *sdkmcp.CallToolResult {
	if err := json.Unmarshal(raw, in); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			if hint, ok := arrayTypeHints[typeErr.Field]; ok {
				res := text(fmt.Sprintf("field %q must be %s (got JSON %s)", typeErr.Field, hint, typeErr.Value))
				res.IsError = true
				return res
			}
		}
		res := text("invalid arguments: " + err.Error())
		res.IsError = true
		return res
	}
	return nil
}

// bindHandler bridges a typed Scribe handler directly to sdkmcp.ToolHandler,
// bypassing Battery's mcpserver layer to allow full sdkmcp.Tool field control.
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
			if errRes := unmarshalInput(req.Params.Arguments, &in); errRes != nil {
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

// arrayTypeHints maps struct field names to their expected JSON wire format.
// Used by unmarshalInput to produce actionable error messages when the caller
// passes the wrong type (e.g. a comma-separated string where a JSON array is required).
var arrayTypeHints = map[string]string{
	"depends_on":     `JSON array of strings — e.g. ["TASK-1", "TASK-2"]`,
	"ids":            `JSON array of strings — e.g. ["TASK-1", "TASK-2"]`,
	"labels":         `JSON array of strings — e.g. ["label1", "label2"]`,
	"labels_or":      `JSON array of strings — e.g. ["label1", "label2"]`,
	"exclude_labels": `JSON array of strings — e.g. ["label1", "label2"]`,
	"fields":         `JSON array of strings — e.g. ["id", "title", "status"]`,
	"section_filter": `JSON array of strings — e.g. ["summary", "source"]`,
	"targets":        `JSON array of strings — e.g. ["REF-1", "REF-2"]`,
	"links":          `JSON object mapping relation→[]string — e.g. {"documents": ["REF-1"]}`,
	"sections":       `JSON array of {"name":"<name>","text":"<body>"} objects — e.g. [{"name":"summary","text":"..."}]`,
	"edges":          `JSON array of {"from":"X","relation":"r","to":"Y"} objects`,
	"extra":          `JSON object — e.g. {"key": "value"}`,
	"patch":          `JSON object mapping field→value — e.g. {"status": "active"}`,
}
