package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// --- Result types ---

// BriefResult is the session brief payload for Scribe.
type BriefResult struct {
	SchemaHash string                `json:"schema_hash,omitempty"`
	Campaigns  []*parchment.Artifact `json:"campaigns,omitempty"`
	Goals      []*parchment.Artifact `json:"goals,omitempty"`
	Context    []string              `json:"context,omitempty"`
	Warnings   []string              `json:"warnings,omitempty"`
}

// DashboardScope holds per-scope statistics.
type DashboardScope struct {
	Scope    string `json:"scope"`
	Total    int    `json:"total"`
	Active   int    `json:"active"`
	Archived int    `json:"archived"`
	Sections int    `json:"sections"`
	Edges    int    `json:"edges"`
	Stale    int    `json:"stale"`
}

// DashboardResult is the housekeeping dashboard payload.
type DashboardResult struct {
	Scopes      []DashboardScope      `json:"scopes"`
	DBSizeBytes int64                 `json:"db_size_bytes"`
	StaleArts   []*parchment.Artifact `json:"stale_artifacts,omitempty"`
}

// InventoryResult is a count-by-kind/status summary.
type InventoryResult struct {
	Total    int                              `json:"total"`
	ByKind   map[string]int                   `json:"by_kind"`
	ByStatus map[string]int                   `json:"by_status"`
	Tracked  map[string][]*parchment.Artifact `json:"tracked,omitempty"`
}

// SetGoalInput parameterises goal creation.
type SetGoalInput struct {
	Title string `json:"title"`
	Scope string `json:"scope,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

// SetGoalResult is returned by SetGoal.
type SetGoalResult struct {
	Goal     *parchment.Artifact   `json:"goal"`
	Root     *parchment.Artifact   `json:"root"`
	Archived []*parchment.Artifact `json:"archived,omitempty"`
}

// DrainEntry represents a discovered legacy markdown file.
// --- Service methods ---

const defaultDashboardStaleDays = 30
const defaultDashboardStaleCap = 10
const scopeNone = "(none)"

// Brief returns the session brief: active campaigns, goals, and schema warnings.
func (s *Service) Brief(ctx context.Context) (*BriefResult, error) { //nolint:cyclop,funlen,gocyclo // brief report is inherently multi-check
	result := &BriefResult{
		SchemaHash: s.Proto.Schema().Hash(),
	}
	schema := s.Proto.Schema()

	for kind, def := range schema.BriefKinds() { //nolint:gocritic // rangeValCopy: acceptable
		arts, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{
			Kind: kind, Status: def.ActiveStatus,
		})
		if def.IsGoalKind {
			result.Goals = append(result.Goals, arts...)
		} else {
			result.Campaigns = append(result.Campaigns, arts...)
		}
	}

	all, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{})

	unknownCounts := make(map[string]int)
	completable, unimplemented := 0, 0
	for _, art := range all {
		if schema.UnknownKind(art.Kind) {
			unknownCounts[art.Kind]++
		}
		isEffortKind := art.Kind == parchment.KindCampaign || art.Kind == parchment.KindGoal
		if !schema.IsTerminal(art.Status) && isEffortKind { //nolint:nestif // brief check is inherently nested
			children, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Parent: art.ID})
			if len(children) > 0 {
				allDone := true
				for _, ch := range children {
					if !schema.IsTerminal(ch.Status) {
						allDone = false
						break
					}
				}
				if allDone {
					completable++
				}
			}
		}
		if !schema.IsTerminal(art.Status) && (art.Kind == parchment.KindSpec || art.Kind == parchment.KindBug) {
			backlinks, _ := s.Proto.Backlinks(ctx, art.ID, parchment.RelImplements)
			if len(backlinks) == 0 {
				unimplemented++
			}
		}
	}

	if len(unknownCounts) > 0 {
		kinds := make([]string, 0, len(unknownCounts))
		total := 0
		for k, c := range unknownCounts {
			kinds = append(kinds, k)
			total += c
		}
		sort.Strings(kinds)
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d artifact(s) have unrecognized kinds: %s — consider updating schema or migrating",
				total, strings.Join(kinds, ", ")))
	}
	if completable > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d campaign/goal(s) completable (all children terminal)", completable))
	}
	if unimplemented > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d spec/bug(s) have no implementing task", unimplemented))
	}
	return result, nil
}

// Dashboard returns storage and staleness statistics.
func (s *Service) Dashboard(ctx context.Context, staleDays int) (*DashboardResult, error) {
	if staleDays <= 0 {
		staleDays = defaultDashboardStaleDays
	}
	schema := s.Proto.Schema()
	cutoff := time.Now().UTC().Add(-time.Duration(staleDays) * 24 * time.Hour)

	all, err := s.Proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return nil, err
	}

	scopeMap := map[string]*DashboardScope{}
	var staleArts []*parchment.Artifact
	for _, art := range all {
		sc := art.Scope
		if sc == "" {
			sc = scopeNone
		}
		ds, ok := scopeMap[sc]
		if !ok {
			ds = &DashboardScope{Scope: sc}
			scopeMap[sc] = ds
		}
		ds.Total++
		if schema.IsReadonly(art.Status) {
			ds.Archived++
		} else if !schema.IsTerminal(art.Status) {
			ds.Active++
			if art.UpdatedAt.Before(cutoff) {
				ds.Stale++
				staleArts = append(staleArts, art)
			}
		}
		ds.Sections += len(art.Sections)
		for _, targets := range art.Links {
			ds.Edges += len(targets)
		}
	}

	sort.Slice(staleArts, func(i, j int) bool {
		return staleArts[i].UpdatedAt.Before(staleArts[j].UpdatedAt)
	})
	if len(staleArts) > defaultDashboardStaleCap {
		staleArts = staleArts[:defaultDashboardStaleCap]
	}

	result := &DashboardResult{StaleArts: staleArts}
	for _, ds := range scopeMap {
		result.Scopes = append(result.Scopes, *ds)
	}
	sort.Slice(result.Scopes, func(i, j int) bool {
		return result.Scopes[i].Total > result.Scopes[j].Total
	})
	if sizer, ok := s.Proto.Store().(parchment.DBSizer); ok {
		result.DBSizeBytes, _ = sizer.DBSizeBytes(ctx)
	}
	return result, nil
}

// Inventory returns a count-by-kind/status summary of all artifacts.
func (s *Service) Inventory(ctx context.Context) (*InventoryResult, error) {
	all, err := s.Proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return nil, err
	}
	briefKinds := s.Proto.Schema().BriefKinds()
	r := &InventoryResult{
		Total:    len(all),
		ByKind:   make(map[string]int),
		ByStatus: make(map[string]int),
		Tracked:  make(map[string][]*parchment.Artifact),
	}
	for _, art := range all {
		r.ByKind[art.Kind]++
		r.ByStatus[art.Status]++
		if def, ok := briefKinds[art.Kind]; ok && art.Status == def.ActiveStatus {
			r.Tracked[art.Kind] = append(r.Tracked[art.Kind], art)
		}
	}
	return r, nil
}

// SetGoal archives existing active goals and creates a new goal with a linked root.
func (s *Service) SetGoal(ctx context.Context, in SetGoalInput) (*SetGoalResult, error) {
	if in.Title == "" {
		return nil, fmt.Errorf("title is required") //nolint:err113 // agent-facing error
	}
	schema := s.Proto.Schema()
	goalKind, goalDef := schema.GoalKind()
	if goalKind == "" {
		return nil, fmt.Errorf("no kind with is_goal_kind=true in schema") //nolint:err113 // agent-facing error
	}

	existing, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{
		Kind: goalKind, Status: goalDef.ActiveStatus, Scope: in.Scope,
	})

	archived := make([]*parchment.Artifact, 0, len(existing))
	for _, old := range existing {
		results, err := s.Proto.SetField(ctx, []string{old.ID}, parchment.FieldStatus,
			schema.ReadonlyStatuses[0], parchment.SetFieldOptions{Force: true})
		if err != nil || (len(results) > 0 && !results[0].OK) {
			continue
		}
		if updated, _ := s.Proto.GetArtifact(ctx, old.ID); updated != nil {
			archived = append(archived, updated)
		}
	}

	goal, err := s.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: goalKind, Title: in.Title, Scope: in.Scope, Status: goalDef.ActiveStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("create goal: %w", err)
	}

	rootKind := in.Kind
	if rootKind == "" {
		rootKind = goalKind
	}
	root, err := s.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: rootKind, Title: in.Title, Scope: in.Scope,
		Links: map[string][]string{parchment.RelJustifies: {goal.ID}},
	})
	if err != nil {
		return nil, fmt.Errorf("create root: %w", err)
	}

	return &SetGoalResult{Goal: goal, Root: root, Archived: archived}, nil
}

// IsComponentLabel reports whether s matches the Scribe component label format.
func IsComponentLabel(s string) bool {
	return componentLabelRe.MatchString(strings.TrimSpace(s))
}

// SortArtifacts sorts arts in-place by field name.
func SortArtifacts(arts []*parchment.Artifact, field string) {
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

var componentLabelRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*:.+/.+$`)

func (s *Service) RenderBrief(ctx context.Context, since, version string, homeScopes []string) (string, error) { //nolint:gocyclo,cyclop,funlen // brief report is inherently multi-check
	m, err := s.Brief(ctx)
	if err != nil {
		return "", err
	}
	var sections []string
	scopeStr := "all"
	if len(homeScopes) > 0 {
		scopeStr = strings.Join(homeScopes, ", ")
	}
	sections = append(sections, fmt.Sprintf("Scribe %s | Scope: %s", version, scopeStr))

	bugs, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindBug, Status: parchment.StatusOpen})
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
	active, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusActive})
	draft, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusDraft})
	if len(active) > 0 || len(draft) > 0 {
		sections = append(sections, fmt.Sprintf("Active Work: %d active, %d draft", len(active), len(draft)))
	}
	staleThreshold := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	stale, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusDraft, UpdatedBefore: staleThreshold})
	if len(stale) > 0 {
		m.Warnings = append(m.Warnings, fmt.Sprintf("%d draft(s) stale >7 days — run dashboard for details", len(stale)))
	}
	if since != "" { //nolint:nestif // session delta block is inherently nested
		changed, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{UpdatedAfter: since, ExcludeStatus: parchment.StatusArchived})
		if len(changed) > 0 {
			limit := len(changed)
			if limit > 15 {
				limit = 15
			}
			var lines []string
			for _, c := range changed[:limit] {
				lines = append(lines, fmt.Sprintf("  %s %-8s [%s] %s", c.ID, c.Status, c.Kind, c.Title))
			}
			header := fmt.Sprintf("Changed Since %s (%d):", since[:10], len(changed))
			if len(changed) > 15 {
				header = fmt.Sprintf("Changed Since %s (%d, showing 15):", since[:10], len(changed))
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
	scope := ""
	if len(homeScopes) > 0 {
		scope = homeScopes[0]
	}
	if since == "" {
		if memLines := s.BriefMemoryLines(ctx, scope, 3); len(memLines) > 0 {
			sections = append(sections, "Memory:\n"+strings.Join(memLines, "\n"))
		}
	}
	if since == "" {
		sections = append(sections, "→ artifact(action=orient) for vault structure and schema map")
	}
	if len(sections) == 0 {
		return "nothing to report", nil
	}
	return strings.Join(sections, "\n\n"), nil
}

const (
	DetectCheckAll           = "all"
	DetectCheckOverlaps      = "overlaps"
	DetectCheckOrphans       = "orphans"
	DetectCheckKnowledge     = "knowledge"
	DetectCheckKnowledgeFull = "knowledge_full"
	DetectCheckEviction      = "eviction"
	DetectCheckSchema        = "schema"
)

func (s *Service) RenderDetect(ctx context.Context, check, scope, kind, project, status string, staleDays int) (string, error) { //nolint:cyclop,gocyclo // each check is a distinct branch
	if check == "" {
		check = DetectCheckAll
	}
	var parts []string

	if check == DetectCheckOverlaps || check == DetectCheckAll {
		report, err := s.Proto.DetectOverlaps(ctx, parchment.OverlapInput{
			Kind: kind, Status: status, Project: project,
		})
		if err != nil {
			return "", err
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

	if check == DetectCheckOrphans || check == DetectCheckAll {
		report, err := s.Proto.DetectOrphans(ctx, parchment.OrphanInput{
			Scope: scope, Status: status,
		})
		if err != nil {
			return "", err
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

	if check == DetectCheckKnowledge || check == DetectCheckAll {
		kPart := s.DetectKnowledge(ctx, DetectKnowledgeInput{Scope: scope, StaleDays: staleDays})
		if kPart != "" {
			parts = append(parts, kPart)
		}
	}

	if check == DetectCheckEviction {
		ePart, err := s.renderEviction(ctx, scope)
		if err != nil {
			return "", err
		}
		parts = append(parts, ePart)
	}

	if check == DetectCheckKnowledgeFull {
		out, err := s.RenderKnowledgeLint(ctx, scope)
		if err != nil {
			return "", err
		}
		parts = append(parts, out)
	}

	if check == DetectCheckSchema {
		out, err := s.RenderCheck(ctx, scope)
		if err != nil {
			return "", err
		}
		parts = append(parts, out)
	}

	return strings.Join(parts, "\n\n"), nil
}

func (s *Service) renderEviction(ctx context.Context, scope string) (string, error) {
	candidates, err := s.Proto.DetectEvictionCandidates(ctx, parchment.EvictionPolicy{
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

func (s *Service) RenderKnowledgeLint(ctx context.Context, scope string) (string, error) {
	var b strings.Builder
	total := 0
	basic := s.DetectKnowledge(ctx, DetectKnowledgeInput{Scope: scope})
	if !strings.Contains(basic, "0 knowledge issue") {
		fmt.Fprintf(&b, "## Health (fleeting + uncited)\n\n%s\n\n", strings.TrimSpace(basic))
	}
	unresolved := s.LintUnresolvedWikilinks(ctx, scope)
	if len(unresolved) > 0 {
		total += len(unresolved)
		fmt.Fprintf(&b, "## Unresolved [[wikilinks]] (%d)\n\n", len(unresolved))
		for _, entry := range unresolved {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}
	orphan := s.LintOrphanedNotes(ctx, scope)
	if len(orphan) > 0 {
		total += len(orphan)
		fmt.Fprintf(&b, "## Orphaned notes (%d)\n\n", len(orphan))
		for _, entry := range orphan {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}
	gaps := s.LintClusterGaps(ctx, scope)
	if len(gaps) > 0 {
		total += len(gaps)
		fmt.Fprintf(&b, "## Cluster synthesis gaps (%d)\n\n", len(gaps))
		for _, entry := range gaps {
			fmt.Fprintln(&b, "  "+entry)
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "Lint clean — no issues found.", nil
	}
	fmt.Fprintf(&b, "Total issues: %d", total)
	return b.String(), nil
}

func (s *Service) RenderCheck(ctx context.Context, scope string) (string, error) {
	report, err := s.Proto.Check(ctx, scope)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(report)
	return string(data), nil
}

func (s *Service) RenderChangelog(ctx context.Context, since, scope string) (string, error) {
	if since == "" {
		return "", fmt.Errorf("since parameter is required for changelog (RFC 3339 timestamp)") //nolint:err113 // user-facing hint
	}
	sinceTime, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return "", fmt.Errorf("invalid since timestamp %q: %w", since, err)
	}
	events, err := s.Proto.GetEvents(ctx, sinceTime, parchment.EventFilter{Scope: scope})
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		return fmt.Sprintf("no changes since %s", since[:10]), nil
	}

	// Collect distinct artifact IDs from events, preserving first-seen order.
	seen := make(map[string]bool)
	var ids []string
	for _, e := range events {
		if e.ArtifactID != "" && !seen[e.ArtifactID] {
			seen[e.ArtifactID] = true
			ids = append(ids, e.ArtifactID)
		}
	}

	byScope := make(map[string][]string) // scope → lines
	for _, id := range ids {
		art, aerr := s.Proto.GetArtifact(ctx, id)
		if aerr != nil {
			continue
		}
		sc := art.Scope
		if sc == "" {
			sc = scopeNone
		}
		byScope[sc] = append(byScope[sc], fmt.Sprintf("  %-16s %-8s %-8s %s", art.ID, art.Kind, art.Status, art.Title))
	}

	scopes := make([]string, 0, len(byScope))
	for sc := range byScope {
		scopes = append(scopes, sc)
	}
	sort.Strings(scopes)

	var sections []string
	for _, sc := range scopes {
		sections = append(sections, fmt.Sprintf("[%s] (%d):\n%s", sc, len(byScope[sc]), strings.Join(byScope[sc], "\n")))
	}
	return fmt.Sprintf("Changes since %s (%d artifacts):\n", since[:10], len(ids)) + strings.Join(sections, "\n\n"), nil
}

func (s *Service) RenderBriefCompact(ctx context.Context, version string) (string, error) {
	active, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusActive})
	draft, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Status: parchment.StatusDraft})
	bugs, _ := s.Proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindBug, Status: parchment.StatusOpen})
	return fmt.Sprintf("Scribe %s | %d active, %d draft, %d open bugs",
		version, len(active), len(draft), len(bugs)), nil
}

func (s *Service) RenderDashboard(ctx context.Context, staleDays int) (string, error) {
	if staleDays <= 0 {
		staleDays = 30
	}
	report, err := s.Dashboard(ctx, staleDays)
	if err != nil {
		return "", err
	}
	type scopeLabelEntry struct {
		Scope  string   `json:"scope"`
		Key    string   `json:"key"`
		Labels []string `json:"labels,omitempty"`
	}
	type dashboardOutput struct {
		*DashboardResult
		ScopeLabels []scopeLabelEntry `json:"scope_labels,omitempty"`
	}
	infos, _ := s.Proto.ListScopeInfo(ctx)
	out := dashboardOutput{DashboardResult: report}
	for _, info := range infos {
		out.ScopeLabels = append(out.ScopeLabels, scopeLabelEntry{
			Scope: info.Scope, Key: info.Key, Labels: info.Labels,
		})
	}
	data, _ := json.Marshal(out)
	return string(data), nil
}

func (s *Service) SessionStart(ctx context.Context, name string) (string, error) {
	if s.Snapshotter == nil {
		return "", fmt.Errorf("snapshot system not configured — cannot start session") //nolint:err113 // user-facing hint
	}
	if name == "" {
		name = fmt.Sprintf("session-%d", time.Now().UnixMilli())
	}
	meta, err := s.Snapshotter.Create(ctx, name)
	if err != nil {
		return "", fmt.Errorf("session_start: %w", err)
	}
	return fmt.Sprintf("session started: key=%s ts=%s artifacts=%d",
		meta.Key, meta.Timestamp.Format(time.RFC3339), meta.Artifacts), nil
}

func (s *Service) SessionCommit(target string) string {
	return fmt.Sprintf("session committed (SQLite WAL is always durable; no explicit commit required). Use session_diff(target=%s) to inspect changes.", target)
}

func (s *Service) SessionDiff(ctx context.Context, key string) (string, error) {
	if s.Snapshotter == nil {
		return "", fmt.Errorf("snapshot system not configured — cannot diff session") //nolint:err113 // user-facing hint
	}
	diff, err := s.Snapshotter.Diff(ctx, key)
	if err != nil {
		return "", fmt.Errorf("session_diff: %w", err)
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
		return "no changes since session baseline", nil
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Service) SessionMerge(ctx context.Context, key, scope string) (string, error) {
	if s.Snapshotter == nil {
		return "", fmt.Errorf("snapshot system not configured — cannot merge session") //nolint:err113 // user-facing hint
	}
	diff, err := s.Snapshotter.Diff(ctx, key)
	if err != nil {
		return "", fmt.Errorf("session_merge diff: %w", err)
	}
	toMerge := make([]string, 0, len(diff.Added)+len(diff.Modified))
	toMerge = append(toMerge, diff.Added...)
	toMerge = append(toMerge, diff.Modified...)
	if len(toMerge) == 0 {
		return "nothing to merge — no changes since session baseline", nil
	}
	var merged, failed []string
	for _, id := range toMerge {
		if _, err := s.Proto.SetField(ctx, []string{id}, parchment.FieldScope, scope, parchment.SetFieldOptions{Force: true}); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		merged = append(merged, id)
	}
	var lines []string
	if len(merged) > 0 {
		lines = append(lines, fmt.Sprintf("merged %d artifact(s) to scope %q: %s", len(merged), scope, strings.Join(merged, ", ")))
	}
	if len(failed) > 0 {
		lines = append(lines, fmt.Sprintf("failed: %s", strings.Join(failed, "; ")))
	}
	return strings.Join(lines, "\n"), nil
}

func (s *Service) SnapshotAction(ctx context.Context, action, name string) (string, error) {
	if s.Snapshotter == nil {
		return "", fmt.Errorf("snapshot system not configured") //nolint:err113 // user-facing hint
	}
	switch action {
	case "create":
		meta, err := s.Snapshotter.Create(ctx, name)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("snapshot created: %s (%d artifacts, %d bytes)", meta.Key, meta.Artifacts, meta.SizeBytes), nil
	case "list":
		snapshots, err := s.Snapshotter.List(ctx)
		if err != nil {
			return "", err
		}
		if len(snapshots) == 0 {
			return "no snapshots found", nil
		}
		var lines []string
		for _, snap := range snapshots {
			n := snap.Name
			if n == "" {
				n = "(auto)"
			}
			lines = append(lines, fmt.Sprintf("  %-20s %s  %d bytes",
				n, snap.Timestamp.Format("2006-01-02 15:04:05"), snap.SizeBytes))
		}
		return fmt.Sprintf("Snapshots (%d):\n%s", len(snapshots), strings.Join(lines, "\n")), nil
	case "diff":
		if name == "" {
			return "", fmt.Errorf("snapshot_name required for diff") //nolint:err113 // user-facing hint
		}
		diff, err := s.Snapshotter.Diff(ctx, name)
		if err != nil {
			return "", err
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
			return "no differences", nil
		}
		return strings.Join(parts, "\n"), nil
	case "restore":
		if name == "" {
			return "", fmt.Errorf("snapshot_name required for restore (use list to find keys)") //nolint:err113 // user-facing hint
		}
		if err := s.Snapshotter.Restore(ctx, name); err != nil {
			return "", err
		}
		return fmt.Sprintf("database restored from snapshot: %s (pre-restore backup created)", name), nil
	default:
		return "", fmt.Errorf("unknown snapshot action %q (valid: create, list, diff, restore)", action) //nolint:err113 // user-facing hint
	}
}
