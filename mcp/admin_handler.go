package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleAdmin(ctx context.Context, req *sdkmcp.CallToolRequest, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocyclo,cyclop,gocritic // dispatch switch; hugeParam: value semantics intentional
	switch in.Action {
	case "brief":
		if in.Compact {
			return h.handleBriefCompact(ctx)
		}
		return h.handleBrief(ctx, req, briefInput{Since: in.Since})
	case "changelog":
		return h.handleChangelog(ctx, in.Since, in.Scope)
	case "snapshot":
		// snapshot: CLI operation, not advertised on MCP surface but kept functional.
		return h.handleSnapshot(ctx, in)
	case "dashboard":
		return h.handleDashboard(ctx, req, dashboardInput{StaleDays: in.StaleDays})
	case "set_goal":
		return h.handleSetGoal(ctx, req, service.SetGoalInput{
			Title: in.Title, Scope: in.Scope, Kind: in.Kind,
		})
	case "detect":
		return h.handleDetect(ctx, req, detectInput{
			Check: in.Check, Scope: in.Scope, Status: in.Status,
			Kind: in.Kind, Project: in.Project,
		})

	case "decision":
		return h.handleDecision(ctx, in)
	case "set_scope":
		return h.handleSetScope(in.Labels) // Labels reused as []string scopes

	case "set_scope_labels":
		if in.Scope == "" {
			return nil, nil, fmt.Errorf("scope is required for set_scope_labels") //nolint:err113 // agent-facing input validation
		}
		if err := h.proto.SetScopeLabels(ctx, in.Scope, in.Labels); err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("scope %q labels set to %v", in.Scope, in.Labels)), nil, nil

	case "correlate":
		return h.handleCorrelate(ctx, in)
	case "ingest_session":
		return h.handleIngestSession(ctx, knowledgeInput{Path: in.Path, Scope: in.Scope})

	case "context_read":
		return h.handleContextRead(ctx, in)
	case "session":
		switch in.SnapshotAction {
		case "start":
			return h.handleSessionStart(ctx, in)
		case "commit":
			return h.handleSessionCommit(ctx, in)
		case "diff": //nolint:goconst // "diff" also appears in snapshot sub-dispatch; not extractable without coupling the two
			return h.handleSessionDiff(ctx, in)
		case "merge":
			return h.handleSessionMerge(ctx, in)
		default:
			return nil, nil, fmt.Errorf("session requires snapshot_action=start|commit|diff|merge") //nolint:err113 // agent-facing hint
		}
	case "capabilities":
		return h.handleCapabilities(ctx)
	default:
		return nil, nil, fmt.Errorf("unknown admin action %q (valid: brief, capabilities, changelog, dashboard, snapshot, set_goal, detect, correlate, ingest_session, decision, context_read, session, set_scope, set_scope_labels)", in.Action) //nolint:err113 // agent-facing hint
	}
}

// handleDecision dispatches decision cache sub-actions: check | record | list.
// Uses adminInput.Check=key, adminInput.Value=answer, adminInput.Scope.
func (h *handler) handleDecision(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) {
	switch in.SnapshotAction {
	case "record":
		if in.Check == "" || in.Evidence == "" {
			return nil, nil, fmt.Errorf("decision record requires check=<key> and evidence=<answer>") //nolint:err113 // user-facing hint
		}
		if err := h.svc.RecordDecision(ctx, in.Check, in.Evidence, in.Scope); err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("decision recorded: %q → %q", in.Check, in.Evidence)), nil, nil
	case "list":
		arts, err := h.svc.ListDecisions(ctx, in.Scope)
		if err != nil {
			return nil, nil, err
		}
		if len(arts) == 0 {
			return text("no decisions recorded"), nil, nil
		}
		var lines []string
		for _, a := range arts {
			lines = append(lines, fmt.Sprintf("  %-30s %s", a.Title, a.Goal))
		}
		return text(strings.Join(lines, "\n")), nil, nil
	default: // "check" or empty
		if in.Check == "" {
			return nil, nil, fmt.Errorf("decision check requires check=<key>") //nolint:err113 // user-facing hint
		}
		answer, err := h.svc.CheckDecision(ctx, in.Check, in.Scope)
		if err != nil {
			return nil, nil, err
		}
		if answer == "" {
			return text(fmt.Sprintf("%q: not decided", in.Check)), nil, nil
		}
		return text(fmt.Sprintf("%q: %s", in.Check, answer)), nil, nil
	}
}

// handleSetScope narrows the session's home scopes to a subset of the current scopes.
// Takes scopes via the Labels field (reused as []string). Allows an agent that
// connected to a wide workspace to self-narrow once it knows its project.
func (h *handler) handleSetScope(scopes []string) (*sdkmcp.CallToolResult, any, error) {
	if len(scopes) == 0 {
		return text(fmt.Sprintf("current scopes: %s", strings.Join(h.homeScopes, ", "))), nil, nil
	}
	h.homeScopes = scopes
	h.svc.HomeScopes = scopes
	return text(fmt.Sprintf("scope narrowed to: %s", strings.Join(scopes, ", "))), nil, nil
}

func (h *handler) handleSetGoal(ctx context.Context, _ *sdkmcp.CallToolRequest, in service.SetGoalInput) (*sdkmcp.CallToolResult, any, error) {
	res, err := h.svc.SetGoal(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	var lines []string
	for _, a := range res.Archived {
		lines = append(lines, fmt.Sprintf("archived %s: %s", a.ID, a.Title))
	}
	lines = append(lines, //nolint:gocritic // appendCombine: two distinct lines
		fmt.Sprintf("%s [current] %s", res.Goal.ID, res.Goal.Title),
		fmt.Sprintf("%s [draft] %s (justifies %s)", res.Root.ID, res.Root.Title, res.Goal.ID),
	)
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleBriefCompact(ctx context.Context) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderBriefCompact(ctx, h.version)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

type briefInput struct {
	Since string `json:"since,omitempty"`
}

func (h *handler) handleBrief(ctx context.Context, _ *sdkmcp.CallToolRequest, in briefInput) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderBrief(ctx, in.Since, h.version, h.homeScopes)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

func (h *handler) handleChangelog(ctx context.Context, since, scope string) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderChangelog(ctx, since, scope)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

func (h *handler) handleSnapshot(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	out, err := h.svc.SnapshotAction(ctx, in.SnapshotAction, in.SnapshotName)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

type dashboardInput struct {
	StaleDays int `json:"stale_days,omitempty"`
}

func (h *handler) handleDashboard(ctx context.Context, _ *sdkmcp.CallToolRequest, in dashboardInput) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderDashboard(ctx, in.StaleDays)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

func (h *handler) handleDetect(ctx context.Context, _ *sdkmcp.CallToolRequest, in detectInput) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderDetect(ctx, in.Check, in.Scope, in.Kind, in.Project, in.Status, in.StaleDays)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

// handleGetSummary returns a compact summary for one or more artifacts.
// Only id, title, kind, scope, status, priority, parent, sprint — no sections.

// handleSessionStart creates a named snapshot that marks the session baseline.
// The snapshot key is used in subsequent session_diff and session_merge calls.
// Target field carries the session name.
func (h *handler) handleSessionStart(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	out, err := h.svc.SessionStart(ctx, in.Target)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

func (h *handler) handleSessionCommit(_ context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	return text(h.svc.SessionCommit(in.Target)), nil, nil
}

func (h *handler) handleSessionDiff(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if in.Target == "" && in.SnapshotName == "" {
		return nil, nil, fmt.Errorf("session_diff requires target= (session name/key)") //nolint:err113 // agent-facing
	}
	key := in.Target
	if key == "" {
		key = in.SnapshotName
	}
	out, err := h.svc.SessionDiff(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

func (h *handler) handleSessionMerge(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if in.Target == "" {
		return nil, nil, fmt.Errorf("session_merge requires target= (session snapshot key)") //nolint:err113 // agent-facing
	}
	if in.Scope == "" {
		return nil, nil, fmt.Errorf("session_merge requires scope= (destination scope)") //nolint:err113 // agent-facing
	}
	out, err := h.svc.SessionMerge(ctx, in.Target, in.Scope)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

func (h *handler) handleContextRead(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: consistent with all other admin handlers
	if in.Target == "" {
		return text("context_read requires target= (task ID)"), nil, nil
	}
	packet, err := h.svc.ContextRead(ctx, in.Target)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.Marshal(packet)
	return text(string(data)), nil, nil
}

type detectInput struct {
	Check     string `json:"check,omitempty" jsonschema:"orphans, overlaps, knowledge, or all (default: all)"`
	Scope     string `json:"scope,omitempty"`
	Status    string `json:"status,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Project   string `json:"project,omitempty"`
	StaleDays int    `json:"stale_days,omitempty" jsonschema:"days before a fleeting note is considered stuck (default: 7)"`
}

// handleCapabilities returns a structured map of every callable operation,
// option, and field — the MCP equivalent of GraphQL introspection.
// Agents call admin(action=capabilities) once at session start to discover
// what's possible without relying on description prose or prior knowledge.
func (h *handler) handleCapabilities(_ context.Context) (*sdkmcp.CallToolResult, any, error) {
	caps := map[string]any{
		// artifact tool actions
		"artifact_actions": []string{
			"create", "get", "list", "set", "update",
			"retire", "attach_section", "detach_section", "bulk_section_update",
			"diff", "recall", "orient", "tree", "briefing", "link", "unlink",
			"topo_sort", "replace", "catalog", "impact",
		},
		// admin tool actions
		"admin_actions": []string{
			"brief", "capabilities", "changelog", "dashboard", "snapshot",
			"set_goal", "detect", "correlate", "ingest_session", "decision",
			"context_read", "session", "set_scope", "set_scope_labels",
		},
		// set() options — each is a bool flag on the set action
		"set_options": map[string]string{ //nolint:gosec // G101: map keys are option names, not credentials
			"force":         "bypass lifecycle transition validation — allows status moves blocked by rules",
			"bypass_guards": "skip rule evaluator entirely — for migrations or emergency writes",
			"cascade":       "apply operation recursively to all children — used with retire/archive",
			"dry_run":       "simulate without writing — returns what would change",
			"rename_id":     "field=scope only — atomically renames the artifact ID to match the new scope key; result.new_id carries the new identifier; all edge references cascade automatically",
		},
		// fields accepted by set(field=X, value=Y)
		"set_fields": []string{
			"title", "goal", "scope", "status", "parent", "priority",
			"kind", "depends_on", "labels", "sprint", "alias",
		},
		// result shape for set() — new_id is only present when rename_id=true
		"set_result_shape": map[string]string{
			"id":     "artifact ID (original, before rename if rename_id was used)",
			"new_id": "new artifact ID after scope rename — only present when rename_id=true",
			"field":  "field that was set",
			"value":  "value that was set",
		},
		// schema kinds available in _schema scope for structural discovery
		"schema_kinds": []string{
			"kind_definition", "edge_type_definition", "label_definition", "rule",
		},
		"schema_discovery": "artifact(action=list, kind=kind_definition, scope=_schema) — learn when to create each kind. " +
			"artifact(action=list, kind=edge_type_definition, scope=_schema) — learn what each relation means. " +
			"artifact(action=list, kind=label_definition, scope=_schema) — learn when to apply each label.",
	}
	data, _ := json.Marshal(caps)
	return text(string(data)), nil, nil
}
