package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
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

	case "set_scope":
		// scope=<name> sets home scope; labels=[] sets arbitrary label filter for the scope.
		if in.Scope != "" && len(in.Labels) > 0 {
			if err := h.proto.SetScopeLabels(ctx, in.Scope, in.Labels); err != nil {
				return nil, nil, err
			}
			return text(fmt.Sprintf("scope %q labels set to %v", in.Scope, in.Labels)), nil, nil
		}
		return h.handleSetScope(in.Labels) // Labels reused as []string scopes

	case "correlate":
		return h.handleCorrelate(ctx, in)
	case "ingest_session":
		return h.handleIngestSession(ctx, knowledgeInput{Path: in.Path, Scope: in.Scope})

	case "context_read":
		return h.handleContextRead(ctx, in)
	case "session":
		switch in.SubAction {
		case "start":
			return h.handleSessionStart(ctx, in)
		case "commit":
			return h.handleSessionCommit(ctx, in)
		case "diff":
			return h.handleSessionDiff(ctx, in)
		case "merge":
			return h.handleSessionMerge(ctx, in)
		default:
			return nil, nil, fmt.Errorf("session requires sub_action=start|commit|diff|merge") //nolint:err113 // agent-facing hint
		}
	case "vocab":
		return h.handleVocab(ctx, in)
	default:
		return nil, nil, fmt.Errorf("unknown admin action %q (valid: brief, changelog, dashboard, snapshot, set_goal, detect, correlate, ingest_session, context_read, session, set_scope, vocab)", in.Action) //nolint:err113 // agent-facing hint
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
	out, err := h.svc.SnapshotAction(ctx, in.SubAction, in.SnapshotName)
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

// handleVocab returns label definitions from _schema with progressive disclosure.
// depth=0 (default): comma-separated slug list.
// depth=1: one line per slug — slug | family | when_to_apply summary.
// depth=2: full JSON of each label_definition's Extra and sections.
func (h *handler) handleVocab(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics consistent with all admin handlers
	if h.proto == nil {
		return text("no schema available"), nil, nil
	}
	labelDefs, err := h.proto.ListArtifacts(ctx, parchment.ListInput{
		Labels: []string{
			parchment.LabelPrefixKind + parchment.KindLabelDefinition,
			parchment.LabelPrefixScope + parchment.SchemaScope,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	relDefs, err := h.proto.ListArtifacts(ctx, parchment.ListInput{
		Labels: []string{
			parchment.LabelPrefixKind + parchment.KindRelationship,
			parchment.LabelPrefixScope + parchment.SchemaScope,
		},
	})
	if err != nil {
		return nil, nil, err
	}

	sort.Slice(labelDefs, func(i, j int) bool { return labelDefs[i].Title < labelDefs[j].Title })
	sort.Slice(relDefs, func(i, j int) bool { return relDefs[i].Title < relDefs[j].Title })

	switch in.Depth {
	case 0:
		slugs := make([]string, 0, len(labelDefs)+len(relDefs))
		for _, art := range labelDefs {
			slugs = append(slugs, art.Title)
		}
		for _, art := range relDefs {
			slugs = append(slugs, art.Title)
		}
		return text(strings.Join(slugs, ", ")), nil, nil

	case 1:
		var lines []string
		for _, art := range labelDefs {
			family, _ := art.Extra["family"].(string)
			desc := sectionText(art, "when_to_apply")
			if desc == "" {
				desc = sectionText(art, "implies")
			}
			lines = append(lines, fmt.Sprintf("%-30s [%-10s] %s", art.Title, family, desc))
		}
		if len(relDefs) > 0 {
			lines = append(lines, "", "Relations:")
			for _, art := range relDefs {
				desc := sectionText(art, "when_to_apply")
				if desc == "" {
					desc = sectionText(art, "description")
				}
				lines = append(lines, fmt.Sprintf("  %-28s %s", art.Title, desc))
			}
		}
		return text(strings.Join(lines, "\n")), nil, nil

	default: // depth >= 2
		type entry struct {
			Slug     string         `json:"slug"`
			Extra    map[string]any `json:"extra,omitempty"`
			Sections []struct {
				Name string `json:"name"`
				Text string `json:"text"`
			} `json:"sections,omitempty"`
		}
		var out []entry
		for _, art := range labelDefs {
			e := entry{Slug: art.Title, Extra: art.Extra}
			for _, sec := range art.Sections {
				e.Sections = append(e.Sections, struct {
					Name string `json:"name"`
					Text string `json:"text"`
				}{Name: sec.Name, Text: sec.Text})
			}
			out = append(out, e)
		}
		for _, art := range relDefs {
			e := entry{Slug: art.Title, Extra: art.Extra}
			for _, sec := range art.Sections {
				e.Sections = append(e.Sections, struct {
					Name string `json:"name"`
					Text string `json:"text"`
				}{Name: sec.Name, Text: sec.Text})
			}
			out = append(out, e)
		}
		data, _ := json.Marshal(out)
		return text(string(data)), nil, nil
	}
}

func sectionText(art *parchment.Artifact, name string) string {
	for _, sec := range art.Sections {
		if sec.Name == name {
			return sec.Text
		}
	}
	return ""
}


