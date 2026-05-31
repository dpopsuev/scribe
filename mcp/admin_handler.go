package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	labelNone      = "(none)"
	checkAll       = "all"
	checkOverlaps  = "overlaps"
	checkOrphans   = "orphans"
	checkKnowledge = "knowledge"
	checkEviction  = "eviction"
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
	case "seed", "schema", "transfer_scope", "vacuum":
		// CLI-only operations — not advertised on the MCP surface.
		return text(fmt.Sprintf("admin(action=%s) is a CLI operation — use: scribe %s", in.Action, in.Action)), nil, nil
	case "lint":
		// schema lint — CLI-only. For knowledge lint use knowledge_lint.
		return text("admin(action=lint) is a CLI operation — use: scribe lint. For knowledge checks use admin(action=knowledge_lint)"), nil, nil
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
	case "check":
		return h.handleCheck(ctx, in.Scope)
	case "set_scope_labels":
		if in.Scope == "" {
			return nil, nil, fmt.Errorf("scope is required for set_scope_labels") //nolint:err113 // agent-facing input validation
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
			labels := labelNone
			if len(info.Labels) > 0 {
				labels = strings.Join(info.Labels, ", ")
			}
			fmt.Fprintf(&b, "%-20s %s → %s\n", info.Scope, info.Key, labels)
		}
		return text(b.String()), nil, nil
	case "correlate":
		return h.handleCorrelate(ctx, in)
	case "ingest_session":
		return h.handleIngestSession(ctx, knowledgeInput{Path: in.Path, Scope: in.Scope})
	case "knowledge_lint":
		// knowledge_lint: wikilink resolution, orphan detection, cluster gaps.
		// Distinct from schema lint (admin=lint checks schema consistency).
		return h.handleKnowledgeLint(ctx, knowledgeInput{Scope: in.Scope})
	case "context_read":
		return h.handleContextRead(ctx, in)
	case "session_start":
		return h.handleSessionStart(ctx, in)
	case "session_commit":
		return h.handleSessionCommit(ctx, in)
	case "session_diff":
		return h.handleSessionDiff(ctx, in)
	case "session_merge":
		return h.handleSessionMerge(ctx, in)
	case "restore", "unarchive":
		return nil, nil, fmt.Errorf( //nolint:err113 // agent-facing redirect
			"admin(%s) is not supported — use artifact(action=de-archive, id=<id>) to restore an archived artifact",
			in.Action)
	default:
		return nil, nil, fmt.Errorf("unknown admin action %q (valid: motd, changelog, dashboard, snapshot, set_goal, vacuum, detect, lint, check, set_scope_labels, list_scope_labels, transfer_scope, seed, schema, correlate, ingest_session, knowledge_lint, context_read, session_start, session_commit, session_diff, session_merge)", in.Action) //nolint:err113 // agent-facing hint
	}
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

type archiveInput struct {
	IDs         []string `json:"ids"`
	Scope       string   `json:"scope,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Status      string   `json:"status,omitempty"`
	IDPrefix    string   `json:"id_prefix,omitempty"`
	ExcludeKind string   `json:"exclude_kind,omitempty"`
	DryRun      bool     `json:"dry_run,omitempty"`
}

func (h *handler) handleMotdCompact(ctx context.Context) (*sdkmcp.CallToolResult, any, error) {
	active, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusActive})
	draft, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusDraft})
	bugs, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindBug, Status: parchment.StatusOpen})

	msg := fmt.Sprintf("Scribe %s | %d active, %d draft, %d open bugs",
		h.version, len(active), len(draft), len(bugs))
	return text(msg), nil, nil
}

type motdInput struct {
	Since string `json:"since,omitempty"`
}

func (h *handler) handleMotd(ctx context.Context, _ *sdkmcp.CallToolRequest, in motdInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocyclo,funlen,nestif // motd report is inherently multi-check
	m, err := h.svc.Motd(ctx)
	if err != nil {
		return nil, nil, err
	}
	var sections []string
	scopeStr := "all"
	if len(h.homeScopes) > 0 {
		scopeStr = strings.Join(h.homeScopes, ", ")
	}
	sections = append(sections, fmt.Sprintf("Scribe %s | Scope: %s", h.version, scopeStr))

	// Open bugs — fires first
	bugs, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindBug, Status: parchment.StatusOpen})
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
	active, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusActive})
	draft, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusDraft})
	if len(active) > 0 || len(draft) > 0 {
		sections = append(sections, fmt.Sprintf("Active Work: %d active, %d draft", len(active), len(draft)))
	}

	// Stale drafts — count only, no itemized list (use dashboard for details).
	staleThreshold := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	stale, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusDraft, UpdatedBefore: staleThreshold})
	if len(stale) > 0 {
		m.Warnings = append(m.Warnings, fmt.Sprintf("%d draft(s) stale >7 days — run dashboard for details", len(stale)))
	}

	// Changed since (session delta).
	if in.Since != "" { //nolint:nestif // session delta block is inherently nested
		changed, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{UpdatedAfter: in.Since, ExcludeStatus: parchment.StatusArchived})
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

	if len(m.Context) > 0 {
		var lines []string
		for _, c := range m.Context {
			lines = append(lines, "  "+c)
		}
		sections = append(sections, "Domain Context:\n"+strings.Join(lines, "\n"))
	}

	if len(m.Warnings) > 0 {
		var lines []string
		for _, w := range m.Warnings {
			lines = append(lines, "  ⚠ "+w)
		}
		sections = append(sections, "Warnings:\n"+strings.Join(lines, "\n"))
	}

	// Memory: top-3 evergreen knowledge artifacts for this scope.
	// Surfaces without requiring the agent to call a separate recall action.
	scope := ""
	if len(h.homeScopes) > 0 {
		scope = h.homeScopes[0]
	}
	if in.Since == "" { // skip on delta calls — memory is session-start context
		if memLines := h.motdMemoryLines(ctx, scope, 3); len(memLines) > 0 {
			sections = append(sections, "Memory:\n"+strings.Join(memLines, "\n"))
		}
	}

	// Tier 1→2 navigation hint — only on full session-start motd, not delta calls.
	if in.Since == "" {
		sections = append(sections, "→ artifact(action=orient) for vault structure and schema map")
	}

	if len(sections) == 0 {
		return text("nothing to report"), nil, nil
	}
	return text(strings.Join(sections, "\n\n")), nil, nil
}

func (h *handler) handleChangelog(ctx context.Context, since, scope string) (*sdkmcp.CallToolResult, any, error) {
	if since == "" {
		return nil, nil, fmt.Errorf("since parameter is required for changelog (RFC 3339 timestamp)") //nolint:err113 // agent-facing hint
	}
	li := parchment.ListInput{
		UpdatedAfter:  since,
		ExcludeStatus: parchment.StatusArchived,
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
	byScope := make(map[string][]*parchment.Artifact)
	for _, a := range arts {
		s := a.Scope
		if s == "" {
			s = labelNone
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

func (h *handler) handleSnapshot(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: adminInput passed by value intentionally
	if h.snapshotter == nil {
		return nil, nil, fmt.Errorf("snapshot system not configured") //nolint:err113 // agent-facing input validation
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
			return nil, nil, fmt.Errorf("snapshot_name required for diff") //nolint:err113 // agent-facing input validation
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
			return nil, nil, fmt.Errorf("snapshot_name required for restore (use list to find keys)") //nolint:err113 // agent-facing hint
		}
		if err := h.snapshotter.Restore(ctx, in.SnapshotName); err != nil {
			return nil, nil, err
		}
		return text(fmt.Sprintf("database restored from snapshot: %s (pre-restore backup created)", in.SnapshotName)), nil, nil

	default:
		return nil, nil, fmt.Errorf("unknown snapshot action %q (valid: create, list, diff, restore)", in.SnapshotAction) //nolint:err113 // agent-facing hint
	}
}

type dashboardInput struct {
	StaleDays int `json:"stale_days,omitempty"`
}

func (h *handler) handleDashboard(ctx context.Context, _ *sdkmcp.CallToolRequest, in dashboardInput) (*sdkmcp.CallToolResult, any, error) {
	staleDays := in.StaleDays
	if staleDays <= 0 {
		staleDays = 30
	}
	report, err := h.svc.Dashboard(ctx, staleDays)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.Marshal(report)
	return text(string(data)), nil, nil
}

type linkInput struct {
	ID       string   `json:"id"`
	Relation string   `json:"relation"`
	Targets  []string `json:"targets"`
	Unlink   bool     `json:"unlink,omitempty"`
}

func (h *handler) handleDetect(ctx context.Context, _ *sdkmcp.CallToolRequest, in detectInput) (*sdkmcp.CallToolResult, any, error) {
	check := in.Check
	if check == "" {
		check = checkAll
	}
	var parts []string

	if check == checkOverlaps || check == checkAll {
		report, err := h.proto.DetectOverlaps(ctx, parchment.OverlapInput{
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

	if check == checkOrphans || check == checkAll {
		report, err := h.proto.DetectOrphans(ctx, parchment.OrphanInput{
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

	if check == checkKnowledge || check == checkAll {
		kPart := h.detectKnowledge(ctx, in)
		if kPart != "" {
			parts = append(parts, kPart)
		}
	}

	if check == checkEviction {
		ePart, err := h.detectEviction(ctx, in.Scope)
		if err != nil {
			return nil, nil, err
		}
		parts = append(parts, ePart)
	}

	return text(strings.Join(parts, "\n\n")), nil, nil
}

func (h *handler) detectEviction(ctx context.Context, scope string) (string, error) {
	candidates, err := h.proto.DetectEvictionCandidates(ctx, parchment.EvictionPolicy{
		MinAgeDays:        30,
		RecencyWindowDays: 90,
		Scope:             scope,
	})
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "No eviction candidates found.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d eviction candidate(s):\n\n", len(candidates))
	for _, c := range candidates {
		fmt.Fprintf(&b, "%-16s %-10s [%s] %s\n  reason: %s\n  tensor: access=%.2f structural=%.2f quality=%.2f recency=%.2f\n\n",
			c.Artifact.ID, string(c.Label), c.Artifact.Status, c.Artifact.Title,
			c.Reason,
			c.Tensor.AccessHeat, c.Tensor.StructuralHeat, c.Tensor.QualityScore, c.Tensor.Recency,
		)
	}
	return b.String(), nil
}



func (h *handler) handleCheck(ctx context.Context, scope string) (*sdkmcp.CallToolResult, any, error) {
	report, err := h.proto.Check(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	data, _ := json.Marshal(report)
	return text(string(data)), nil, nil
}

// --- vocab handlers ---

// --- rendering helpers ---

// sortArtifacts delegates to service.SortArtifacts.
func sortArtifacts(arts []*parchment.Artifact, field string) {
	service.SortArtifacts(arts, field)
}

// handleGetSummary returns a compact summary for one or more artifacts.
// Only id, title, kind, scope, status, priority, parent, sprint — no sections.

// handleSessionStart creates a named snapshot that marks the session baseline.
// The snapshot key is used in subsequent session_diff and session_merge calls.
// Target field carries the session name.
func (h *handler) handleSessionStart(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if h.snapshotter == nil {
		return nil, nil, fmt.Errorf("snapshot system not configured — cannot start session") //nolint:err113 // agent-facing
	}
	name := in.Target
	if name == "" {
		name = fmt.Sprintf("session-%d", time.Now().UnixMilli())
	}
	meta, err := h.snapshotter.Create(ctx, name)
	if err != nil {
		return nil, nil, fmt.Errorf("session_start: %w", err)
	}
	return text(fmt.Sprintf("session started: key=%s ts=%s artifacts=%d",
		meta.Key, meta.Timestamp.Format(time.RFC3339), meta.Artifacts)), nil, nil
}

// handleSessionCommit is a no-op — SQLite WAL writes are already durable.
// Returns the current snapshot key for reference.
func (h *handler) handleSessionCommit(_ context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	return text(fmt.Sprintf(
		"session committed (SQLite WAL is always durable; no explicit commit required). "+
			"Use session_diff(target=%s) to inspect changes.", in.Target)), nil, nil
}

// handleSessionDiff reports artifacts modified since the named session snapshot.
// Uses EventLog events since the snapshot timestamp when available, falling back
// to Filter{UpdatedAfter: baseline} scan.
func (h *handler) handleSessionDiff(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if h.snapshotter == nil {
		return nil, nil, fmt.Errorf("snapshot system not configured — cannot diff session") //nolint:err113 // agent-facing
	}
	if in.Target == "" && in.SnapshotName == "" {
		return nil, nil, fmt.Errorf("session_diff requires target= (session name/key)") //nolint:err113 // agent-facing
	}
	key := in.Target
	if key == "" {
		key = in.SnapshotName
	}

	diff, err := h.snapshotter.Diff(ctx, key)
	if err != nil {
		return nil, nil, fmt.Errorf("session_diff: %w", err)
	}

	var lines []string
	if len(diff.Added) > 0 {
		lines = append(lines, fmt.Sprintf("added (%d): %s", len(diff.Added), strings.Join(diff.Added, ", ")))
	}
	if len(diff.Modified) > 0 {
		lines = append(lines, fmt.Sprintf("modified (%d): %s", len(diff.Modified), strings.Join(diff.Modified, ", ")))
	}
	if len(diff.Removed) > 0 {
		lines = append(lines, fmt.Sprintf("removed (%d): %s", len(diff.Removed), strings.Join(diff.Removed, ", ")))
	}
	if len(lines) == 0 {
		return text("no changes since session baseline"), nil, nil
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

// handleSessionMerge identifies artifacts added or modified since the session
// snapshot and re-scopes them from the session scope into the target scope.
// Target field carries the session key; Scope carries the destination scope.
func (h *handler) handleSessionMerge(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if h.snapshotter == nil {
		return nil, nil, fmt.Errorf("snapshot system not configured — cannot merge session") //nolint:err113 // agent-facing
	}
	if in.Target == "" {
		return nil, nil, fmt.Errorf("session_merge requires target= (session snapshot key)") //nolint:err113 // agent-facing
	}
	if in.Scope == "" {
		return nil, nil, fmt.Errorf("session_merge requires scope= (destination scope)") //nolint:err113 // agent-facing
	}

	diff, err := h.snapshotter.Diff(ctx, in.Target)
	if err != nil {
		return nil, nil, fmt.Errorf("session_merge diff: %w", err)
	}

	toMerge := make([]string, 0, len(diff.Added)+len(diff.Modified))
	toMerge = append(toMerge, diff.Added...)
	toMerge = append(toMerge, diff.Modified...)

	if len(toMerge) == 0 {
		return text("nothing to merge — no changes since session baseline"), nil, nil
	}

	var merged, failed []string
	for _, id := range toMerge {
		_, err := h.proto.SetField(ctx, []string{id}, parchment.FieldScope, in.Scope, parchment.SetFieldOptions{Force: true})
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		merged = append(merged, id)
	}

	var lines []string
	if len(merged) > 0 {
		lines = append(lines, fmt.Sprintf("merged %d artifact(s) to scope %q: %s", len(merged), in.Scope, strings.Join(merged, ", ")))
	}
	if len(failed) > 0 {
		lines = append(lines, fmt.Sprintf("failed: %s", strings.Join(failed, "; ")))
	}
	return text(strings.Join(lines, "\n")), nil, nil
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
