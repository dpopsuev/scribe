package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const toolNameArtifact = "artifact"

// baseInstructions is the core MCP server instructions shown to clients.
const baseInstructions = "Labeled Artifact Graph. " +
	"SCHEMA: artifact(action=query, kind=label_definition, scope=_schema) to learn available kinds and labels."

// workspaceUnconfiguredWarning is prepended to instructions when the client
// has not declared workspace context in the initialize params.
const workspaceUnconfiguredWarning = "WORKSPACE UNSET: pass workspace={cwd,git_remote} in your initialize _meta params " +
	"to scope artifacts to your repository. Until set, artifacts have no workspace context.\n\n"

// buildInstructions returns the instructions string for a session,
// prepending a warning when the workspace context has not been configured.
func buildInstructions(configured bool) string {
	if !configured {
		return workspaceUnconfiguredWarning + baseInstructions
	}
	return baseInstructions
}

// NewServer creates an MCP server exposing Scribe tools over the given store.
// stdioLabels carries workspace labels detected from the server's own CWD
// (stdio transport only). For HTTP transport, labels are set per-session via
// the initialize handler.
func NewServer(svc *service.Service, vocab []string, version string, stdioLabels ...string) (*sdkmcp.Server, *Registry) {
	reg := newRegistry()
	sid := newSessionID()
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

	artifactDesc := "Labeled Artifact Graph — nodes and edges. " +
		"FIND: query(query=) for FTS; query(ranked=true, query=) for scored recall; query(mode=semantic, query=) for vector similarity. " +
		"READ: get(id=) full artifact. " +
		"WRITE: create, set (single field), update (sections/extra patch). " +
		"EDGES: link(id=, relation=, targets=[]) to add; link(mode=unlink) to remove; link(edges=[{from,relation,to}]) for bulk. " +
		"PLAN: query(id=, sort=topo) for dependency order; query(id=, sort=topo, unblocked=true) for ready queue."
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
		Keywords:   []string{"create", "get", "query", "set", "update", "link", "unlink", "edge", "artifact"},
		Categories: []string{"crud", "graph"},
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
	return NewServer(svc, nil, version, workspaceLabels...)
}

type handler struct {
	proto *parchment.Protocol
	svc   *service.Service

	version             string
	homeScopes          []string // default scopes; narrowable at runtime
	workspaceLabels     []string // context labels stamped on every artifact this session
	workspaceConfigured bool     // true once workspace context has been set
}

// --- consolidated input types ---

type artifactInput struct {
	Action string `json:"action" jsonschema:"required,create | get | query | set | update | link"`

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

	Format  string   `json:"format,omitempty" jsonschema:"summary or full (default)"`
	GroupBy string   `json:"group_by,omitempty" jsonschema:"status, scope, kind, sprint"`
	Sort    string   `json:"sort,omitempty" jsonschema:"id, title, status, scope, kind"`
	Limit   int      `json:"limit,omitempty"`
	Cursor  string   `json:"cursor,omitempty" jsonschema:"pagination cursor from previous list response"`
	Count   bool     `json:"count,omitempty"`
	Ranked  bool     `json:"ranked,omitempty" jsonschema:"scored FTS with kind and recency weighting"`
	Mode    string   `json:"mode,omitempty" jsonschema:"fts (default) | semantic | hybrid"`
	Session string   `json:"session,omitempty" jsonschema:"scope results to a single agent session ID"`
	Top     int      `json:"top,omitempty" jsonschema:"N most relevant by status+priority+recency"`
	Fields  []string `json:"fields,omitempty" jsonschema:"id, kind, scope, status, title, parent, priority"`
	Query   string   `json:"query,omitempty" jsonschema:"substring search across title, goal, sections"`

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
	Cascade bool     `json:"cascade,omitempty" jsonschema:"apply status transition recursively to all children"`
	DryRun  bool     `json:"dry_run,omitempty" jsonschema:"simulate the operation and return what would change without writing anything"`

	Patch        map[string]string `json:"patch,omitempty" jsonschema:"{field: value} pairs for batch_update"`
	Artifacts    []map[string]any  `json:"artifacts,omitempty"`
	SkipHooks    bool              `json:"skip_hooks,omitempty"`
	CloneFrom    string            `json:"clone_from,omitempty" jsonschema:"create: source artifact ID to clone from"`
	IncludeEdges bool              `json:"include_edges,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
	Prefix       string            `json:"prefix,omitempty"`

	Relation  string      `json:"relation,omitempty" jsonschema:"parent_of, depends_on, follows, justifies, implements, documents, blocks, duplicates, relates_to, clones, mentions, tested_by, supersedes, cites, elaborates, contradicts, traces_to, calls, explains, causes, resolves"` //nolint:misspell // synthesises intentionally omitted (British spelling causes linter noise)
	Weight    float64     `json:"weight,omitempty" jsonschema:"edge coupling strength (0.0 = boolean, 1.0 = max; default 0)"`
	Direction string      `json:"direction,omitempty" jsonschema:"outbound (default) or inbound"`
	Depth     int         `json:"depth,omitempty" jsonschema:"tree/briefing: max depth; query sort=topo: max results when unblocked=true"`
	Unblocked bool        `json:"unblocked,omitempty" jsonschema:"query sort=topo: return only unblocked ready tasks"`
	Targets   []string    `json:"targets,omitempty"`
	OldTarget string      `json:"old_target,omitempty"`
	Edges     []edgeInput `json:"edges,omitempty" jsonschema:"link/unlink bulk mode: [{from, relation, to}]"`
}

type edgeInput struct {
	From     string `json:"from" jsonschema:"source artifact ID"`
	Relation string `json:"relation" jsonschema:"edge type"`
	To       string `json:"to" jsonschema:"target artifact ID"`
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

const hintIDArray = `JSON array of strings — e.g. ["TASK-1", "TASK-2"]`
const hintLabelArray = `JSON array of strings — e.g. ["label1", "label2"]`

// arrayTypeHints maps struct field names to their expected JSON wire format.
// Used by unmarshalInput to produce actionable error messages when the caller
// passes the wrong type (e.g. a comma-separated string where a JSON array is required).
var arrayTypeHints = map[string]string{
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
