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
	case "motd":
		if in.Compact {
			return h.handleMotdCompact(ctx)
		}
		return h.handleMotd(ctx, req, motdInput{Since: in.Since})
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
	default:
		return nil, nil, fmt.Errorf("unknown admin action %q (valid: motd, changelog, dashboard, snapshot, set_goal, detect, correlate, ingest_session, context_read, session, set_scope, set_scope_labels)", in.Action) //nolint:err113 // agent-facing hint
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

func (h *handler) handleMotdCompact(ctx context.Context) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderMotdCompact(ctx, h.version)
	if err != nil {
		return nil, nil, err
	}
	return text(out), nil, nil
}

type motdInput struct {
	Since string `json:"since,omitempty"`
}

func (h *handler) handleMotd(ctx context.Context, _ *sdkmcp.CallToolRequest, in motdInput) (*sdkmcp.CallToolResult, any, error) {
	out, err := h.svc.RenderMotd(ctx, in.Since, h.version, h.homeScopes)
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
