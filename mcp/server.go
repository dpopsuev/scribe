package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	battmcp "github.com/dpopsuev/battery/mcp"
	"github.com/dpopsuev/battery/tool"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/directive"
	"github.com/dpopsuev/scribe/service"
	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// scribeInstructions is the MCP server instructions shown to clients.
const scribeInstructions = "Work artifact graph + knowledge vault. " +
	"SESSION START: call admin(action=motd) — it discloses your scope, active goal, and open bugs. " +
	"You are the compiler: ingest reads sources and extracts notes, synthesize compiles related notes, file answers back as notes."

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
	h := &handler{
		proto:       svc.Proto,
		svc:         svc,
		snapshotter: svc.Snapshotter,
		version:     version,
		homeScopes:  svc.HomeScopes,
		readLog:     loadReadLog(context.Background(), store, svc.Proto, sid),
		sessionID:   sid,
	}

	// Build SDK directly for full MCP 2025 spec support (Title, Annotations).
	sdk := batt.SDK()
	destructiveHint := true

	// artifact tool
	artifactDesc := "CRUD + search for work (task/spec/bug/goal) and knowledge (note/concept/source) artifacts. " +
		"FIND: search(query=) for keyword FTS; recall(query=, top=10) for semantic — both cheaper than list. " +
		"READ: get(id=) full artifact; get_section(id=, name=) for one section only (cheaper). " +
		"LIST: always add scope/kind/status or top=N — bare list returns ALL artifacts and burns context. " +
		"WRITE: create, set, attach_section, archive. " +
		"ORIENT: orient for vault map (call after motd); catalog for full inventory."
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
		Keywords:   []string{"create", "get", "list", "set", "archive", "artifact", "section"},
		Categories: []string{"crud"},
	})

	// graph tool
	graphDesc := "Artifact relationships and DAG traversal. " +
		"briefing(id=) — full edge-aware context chain including parents, specs, and dependencies; use before starting work on an artifact. " +
		"tree(id=) — direct children only, shallow. " +
		"topo_sort — dependency-ordered work queue. " +
		"link/unlink — add or remove edges. move — reparent an artifact."
	var graphSchema any
	_ = json.Unmarshal(schemaFor[graphInput](), &graphSchema)
	sdk.AddTool(&sdkmcp.Tool{
		Name:        "graph",
		Title:       "Artifact Graph",
		Description: graphDesc,
		InputSchema: graphSchema,
		Annotations: &sdkmcp.ToolAnnotations{DestructiveHint: &destructiveHint},
	}, bindHandler(h.handleGraph))
	reg.Register(directive.ToolMeta{
		Name: "graph", Description: graphDesc,
		Keywords:   []string{"tree", "briefing", "topo_sort", "link", "unlink", "bulk_link", "move", "replace", "relation", "edge"},
		Categories: []string{"query", "graph"},
	})

	// admin tool
	adminDesc := "Session bootstrap and housekeeping. " +
		"CALL FIRST: motd — returns scope, open bugs, active goal, and memory surface. " +
		"CONTINUATION: motd(since=<RFC3339>) for delta only — returns only what changed since that timestamp. " +
		"motd(compact=true) for a one-line status summary. " +
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
		Keywords:   []string{"motd", "dashboard", "goal", "vacuum", "detect", "orphan"},
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
	homeScopes  []string        // default scopes for operations that need a scope
	readLog     map[string]bool // tracks which artifact IDs have been read this session
	sessionID   string          // stable per-process ID used to persist readLog as a config artifact
}

// --- consolidated input types ---

type artifactInput struct {
	Action string `json:"action" jsonschema:"required,create | batch_create | clone | get | list | set | update | archive | de-archive | retire | attach_section | get_section | detach_section | list_sections | search_sections | bulk_section_update | batch_update | move | diff | promote_stash | inspect_stash | recall"`

	ID     string `json:"id,omitempty"`
	Target string `json:"target,omitempty" jsonschema:"new parent ID (move)"`
	Kind   string `json:"kind,omitempty" jsonschema:"task, spec, bug, goal, campaign, doc, ref, need, decision"`
	Scope  string `json:"scope,omitempty"`

	Title     string              `json:"title,omitempty"`
	Goal      string              `json:"goal,omitempty"`
	Parent    string              `json:"parent,omitempty"`
	Status    string              `json:"status,omitempty" jsonschema:"draft, active, complete, archived, retired"`
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
	Offset  int      `json:"offset,omitempty"`
	Count   bool     `json:"count,omitempty"`
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

	Field string `json:"field,omitempty" jsonschema:"title, goal, scope, status, parent, priority, kind, depends_on, labels"`
	Value string `json:"value,omitempty" jsonschema:"new value (comma-separated for list fields)"`
	Force bool   `json:"force,omitempty" jsonschema:"bypass transition validation"`

	Name          string   `json:"name,omitempty"`
	Text          string   `json:"text,omitempty"`
	Body          string   `json:"body,omitempty"`
	SectionFilter []string `json:"section_filter,omitempty"`
	Against       string   `json:"against,omitempty"`

	IDs     []string `json:"ids,omitempty"`
	Cascade bool     `json:"cascade,omitempty"`
	DryRun  bool     `json:"dry_run,omitempty"`

	Patch        map[string]string `json:"patch,omitempty" jsonschema:"{field: value} pairs for batch_update"`
	Artifacts    []map[string]any  `json:"artifacts,omitempty"`
	SkipHooks    bool              `json:"skip_hooks,omitempty"`
	StashID      string            `json:"stash_id,omitempty"`
	IncludeEdges bool              `json:"include_edges,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
	Prefix       string            `json:"prefix,omitempty"`
}

type graphInput struct {
	Action    string      `json:"action" jsonschema:"required,tree | briefing | topo_sort | next | link | unlink | bulk_link | bulk_unlink | move | replace | impact"`
	ID        string      `json:"id,omitempty" jsonschema:"root ID for tree/briefing, or source for link/unlink/move/replace"`
	Relation  string      `json:"relation,omitempty" jsonschema:"parent_of, depends_on, follows, justifies, implements, documents"`
	Direction string      `json:"direction,omitempty" jsonschema:"outbound (default) or inbound"`
	Depth     int         `json:"depth,omitempty" jsonschema:"max traversal depth (0 = unlimited)"`
	Targets   []string    `json:"targets,omitempty"`
	Target    string      `json:"target,omitempty" jsonschema:"new parent ID (move) or new target ID (replace)"`
	OldTarget string      `json:"old_target,omitempty"`
	Edges     []edgeInput `json:"edges,omitempty"`
	Format    string      `json:"format,omitempty" jsonschema:"text (default) or json"`
}

type edgeInput struct {
	From     string `json:"from" jsonschema:"source artifact ID"`
	Relation string `json:"relation" jsonschema:"edge type"`
	To       string `json:"to" jsonschema:"target artifact ID"`
}

// knowledgeInput defines the input schema for the knowledge tool.
type knowledgeInput struct {
	Action string `json:"action" jsonschema:"required,lint | capture | promote | daily | backlinks | export_vault | import_vault | ingest | synthesize | ingest_session | recall | session_start | session_commit | session_diff | session_merge"`

	// capture: create a fleeting note
	Title  string   `json:"title,omitempty" jsonschema:"note title (required for capture)"`
	Body   string   `json:"body,omitempty" jsonschema:"note body text (capture)"`
	Scope  string   `json:"scope,omitempty" jsonschema:"scope/vault to write into"`
	Labels []string `json:"labels,omitempty" jsonschema:"freeform tags"`

	// promote / daily / backlinks: target artifact
	ID       string `json:"id,omitempty" jsonschema:"artifact ID (required for promote, backlinks)"`
	Relation string `json:"relation,omitempty" jsonschema:"edge type filter for backlinks (default: all)"`

	// export_vault / import_vault: filesystem path
	Dir string `json:"dir,omitempty" jsonschema:"directory path (required for export_vault, import_vault)"`

	// ingest: external source URL
	URL string `json:"url,omitempty" jsonschema:"source URL for ingest"`

	// ingest_session: filesystem path to a .jsonl session file or directory
	Path string `json:"path,omitempty" jsonschema:"path to .jsonl session file or directory (ingest_session)"`

	// synthesize: full-text search query
	Query string `json:"query,omitempty" jsonschema:"search query (required for synthesize)"`
}

type adminInput struct {
	Action  string `json:"action" jsonschema:"required,motd | changelog | dashboard | set_goal | detect | check | correlate | ingest_session | knowledge_lint | set_scope_labels | list_scope_labels | context_read | session_start | session_commit | session_diff | session_merge"`
	Compact bool   `json:"compact,omitempty" jsonschema:"minimal output for repeat calls (motd)"`

	SnapshotAction string `json:"snapshot_action,omitempty" jsonschema:"create, list, diff, or restore"`
	SnapshotName   string `json:"snapshot_name,omitempty" jsonschema:"snapshot label (create) or key (diff)"`

	StaleDays int    `json:"stale_days,omitempty" jsonschema:"days without update to consider stale (dashboard)"`
	Since     string `json:"since,omitempty" jsonschema:"RFC 3339 lower bound (motd, changelog)"`

	Title string `json:"title,omitempty"`
	Scope string `json:"scope,omitempty"`
	Kind  string `json:"kind,omitempty" jsonschema:"artifact kind filter or root kind for set_goal"`

	Days  int  `json:"days,omitempty"`
	Force bool `json:"force,omitempty"`

	Target string `json:"target,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`

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

// appendRelatedNotes searches FTS for existing knowledge notes related to
// the given title+body and appends them to b as link candidates.
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
// bypassing Battery's mcpserver layer to allow full sdkmcp.Tool field control
// (Title, Annotations, OutputSchema). Includes basic error recovery.
func bindHandler[In any](h func(context.Context, *sdkmcp.CallToolRequest, In) (*sdkmcp.CallToolResult, any, error)) sdkmcp.ToolHandler {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest) (res *sdkmcp.CallToolResult, retErr error) {
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

// text creates a CallToolResult with a single TextContent block.
// Uses a direct SDK result because Battery v0.11 exposes transport-neutral results.
// resolveIDs merges explicit ids slice with a single id fallback.
func resolveIDs(ids []string, id string) []string {
	if len(ids) > 0 {
		return ids
	}
	if id != "" {
		return []string{id}
	}
	return nil
}

// renderResults formats []parchment.Result as human-readable lines.
// okLabel is used for successful results; errLabel is unused (errors always show the error text).
func renderResults(results []parchment.Result, okLabel, _ string) string {
	lines := make([]string, 0, len(results))
	for _, r := range results {
		if r.OK {
			lines = append(lines, r.ID+" -> "+okLabel)
		} else {
			lines = append(lines, r.ID+" -> error: "+r.Error)
		}
	}
	return strings.Join(lines, "\n")
}

func text(s string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: s},
		},
	}
}

func sdkResultToBattery(res *sdkmcp.CallToolResult) tool.Result {
	out := tool.Result{
		StructuredContent: res.StructuredContent,
		IsError:           res.IsError,
	}
	for _, c := range res.Content {
		switch v := c.(type) {
		case *sdkmcp.TextContent:
			out.Content = append(out.Content, tool.TextContent{Text: v.Text})
		case *sdkmcp.ImageContent:
			out.Content = append(out.Content, tool.ImageContent{MIMEType: v.MIMEType, Data: v.Data})
		case *sdkmcp.AudioContent:
			out.Content = append(out.Content, tool.AudioContent{MIMEType: v.MIMEType, Data: v.Data})
		case *sdkmcp.ResourceLink:
			out.Content = append(out.Content, tool.ResourceLink{
				URI:         v.URI,
				Name:        v.Name,
				Description: v.Description,
				MIMEType:    v.MIMEType,
			})
		case *sdkmcp.EmbeddedResource:
			if v.Resource == nil {
				continue
			}
			out.Content = append(out.Content, tool.ResourceContent{
				URI:      v.Resource.URI,
				MIMEType: v.Resource.MIMEType,
				Text:     v.Resource.Text,
				Blob:     v.Resource.Blob,
			})
		}
	}
	return out
}

// adaptTypedHandler bridges a typed Scribe handler into Battery's MCP handler shape.
// schemaFor derives a JSON Schema from Go struct type T using jsonschema tags.
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

// The typed handler takes a concrete input struct and returns
// (*CallToolResult, Out, error). This adapter unmarshals the raw JSON input,
// calls the handler, and marshals the Out value when CallToolResult is nil
// (same pattern as Origami's rawHandler).
// arrayTypeHints maps struct field names to their expected JSON wire format.
// Used to produce actionable error messages when the caller passes the wrong type
// (e.g. a comma-separated string where a JSON array is required).
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

func adaptTypedHandler[In any](h func(context.Context, *sdkmcp.CallToolRequest, In) (*sdkmcp.CallToolResult, any, error)) func(context.Context, json.RawMessage) (tool.Result, error) {
	return func(ctx context.Context, input json.RawMessage) (tool.Result, error) {
		var in In
		if len(input) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				var typeErr *json.UnmarshalTypeError
				if errors.As(err, &typeErr) {
					if hint, ok := arrayTypeHints[typeErr.Field]; ok {
						return tool.Result{}, fmt.Errorf("field %q must be %s (got JSON %s)", typeErr.Field, hint, typeErr.Value) //nolint:err113 // agent-facing hint
					}
					return tool.Result{}, fmt.Errorf("field %q: expected %s, got JSON %s", typeErr.Field, typeErr.Type, typeErr.Value) //nolint:err113 // agent-facing hint
				}
				return tool.Result{}, fmt.Errorf("invalid arguments: %w", err)
			}
		}

		// Build a minimal CallToolRequest for backward compat with handlers
		// that read req.Params or req.Session.
		req := &sdkmcp.CallToolRequest{}
		req.Params = &sdkmcp.CallToolParamsRaw{Arguments: input}

		res, out, err := h(ctx, req, in)
		if err != nil {
			return tool.Result{}, err
		}
		if res != nil {
			return sdkResultToBattery(res), nil
		}
		// No CallToolResult — preserve JSON text fallback while upgrading to Battery result transport.
		if out == nil {
			return tool.Result{}, nil
		}
		result, err := tool.StructuredResult(out)
		if err != nil {
			return tool.Result{}, fmt.Errorf("marshal output: %w", err)
		}
		return result, nil
	}
}
