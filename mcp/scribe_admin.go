package mcp

// scribe_admin.go — session, dashboard, and drain operations that encode
// Scribe's domain model. Use only public parchment.Protocol methods.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// MotdResult is the message-of-the-day payload for Scribe.
type MotdResult struct {
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

// SetGoalResult is returned by scriveSetGoal.
type SetGoalResult struct {
	Goal     *parchment.Artifact   `json:"goal"`
	Root     *parchment.Artifact   `json:"root"`
	Archived []*parchment.Artifact `json:"archived,omitempty"`
}

// DrainEntry represents a discovered legacy markdown file.
type DrainEntry struct {
	Path     string `json:"path"`
	Dir      string `json:"dir"`
	Filename string `json:"filename"`
	SizeB    int64  `json:"size_bytes"`
}

// --- Motd ---

// Motd returns the message of the day for Scribe sessions.
func Motd(ctx context.Context, proto *parchment.Protocol) (*MotdResult, error) {
	return scriveMotd(ctx, proto)
}

// Dashboard returns storage and staleness statistics.
func Dashboard(ctx context.Context, proto *parchment.Protocol, staleDays int) (*DashboardResult, error) {
	return scriveDashboard(ctx, proto, staleDays)
}

// Inventory returns a count-by-kind/status summary.
func Inventory(ctx context.Context, proto *parchment.Protocol) (*InventoryResult, error) {
	return scriveInventory(ctx, proto)
}

// SetGoal archives existing active goal and creates a new one.
func SetGoal(ctx context.Context, proto *parchment.Protocol, in SetGoalInput) (*SetGoalResult, error) {
	return scriveSetGoal(ctx, proto, in)
}

// DrainDiscover lists legacy .md files under path.
func DrainDiscover(ctx context.Context, path string) ([]DrainEntry, error) {
	return scriveDrainDiscover(ctx, path)
}

// DrainCleanup deletes legacy .md files under path.
func DrainCleanup(ctx context.Context, path string) (int, error) {
	return scriveDrainCleanup(ctx, path)
}

// IsComponentLabel reports whether s is a Scribe component label.
func IsComponentLabel(s string) bool {
	return isComponentLabel(s)
}

// scriveMotd returns the message of the day: active campaigns, goals,
// domain context docs, and schema health warnings.
// Rewritten from parchment.Protocol.Motd using only public Protocol methods.
func scriveMotd(ctx context.Context, proto *parchment.Protocol) (*MotdResult, error) { //nolint:cyclop,funlen,gocyclo // motd report is inherently multi-check
	result := &MotdResult{
		SchemaHash: proto.Schema().Hash(),
	}
	schema := proto.Schema()

	// Active campaigns + goals.
	for kind, def := range schema.MotdKinds() { //nolint:gocritic // rangeValCopy: acceptable
		arts, _ := proto.ListArtifacts(ctx, parchment.ListInput{
			Kind: kind, Status: def.ActiveStatus,
		})
		if def.IsGoalKind {
			result.Goals = append(result.Goals, arts...)
		} else {
			result.Campaigns = append(result.Campaigns, arts...)
		}
	}

	all, _ := proto.ListArtifacts(ctx, parchment.ListInput{})

	// Should-section gaps intentionally omitted from motd.
	// These are database health metrics — they belong in dashboard, not session start context.
	// At scale (thousands of artifacts) they drown out actionable signals.

	// Unknown kinds, stale drafts, completable goals, unimplemented specs.
	unknownCounts := make(map[string]int)
	staleDrafts, completable, unimplemented := 0, 0, 0
	staleCutoff := time.Now().Add(-7 * 24 * time.Hour)

	for _, art := range all {
		if schema.UnknownKind(art.Kind) {
			unknownCounts[art.Kind]++
		}
		if !schema.IsTerminal(art.Status) && !art.UpdatedAt.IsZero() && art.UpdatedAt.Before(staleCutoff) {
			staleDrafts++
		}
		isEffortKind := art.Kind == parchment.KindCampaign || art.Kind == parchment.KindGoal
		if !schema.IsTerminal(art.Status) && isEffortKind { //nolint:nestif // motd check is inherently nested
			children, _ := proto.ListArtifacts(ctx, parchment.ListInput{Parent: art.ID})
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
			backlinks, _ := proto.Backlinks(ctx, art.ID, parchment.RelImplements)
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
	if staleDrafts > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d artifact(s) stale (not updated in 7+ days)", staleDrafts))
	}
	if completable > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d campaign/goal(s) completable (all children terminal)", completable))
	}
	if unimplemented > 0 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("%d spec/bug(s) have no implementing task", unimplemented))
	}

	// Domain context intentionally omitted from motd.
	// At scale this section becomes hundreds of lines of noise.
	// Session priming is handled by the Memory section (top-3 evergreen notes).

	return result, nil
}

// --- Dashboard ---

const defaultDashboardStaleDays = 30
const defaultDashboardStaleCap = 10

// scriveDashboard returns storage and staleness statistics.
func scriveDashboard(ctx context.Context, proto *parchment.Protocol, staleDays int) (*DashboardResult, error) {
	if staleDays <= 0 {
		staleDays = defaultDashboardStaleDays
	}
	schema := proto.Schema()
	cutoff := time.Now().UTC().Add(-time.Duration(staleDays) * 24 * time.Hour)

	all, err := proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return nil, err
	}

	scopeMap := map[string]*DashboardScope{}
	var staleArts []*parchment.Artifact
	for _, art := range all {
		s := art.Scope
		if s == "" {
			s = "(none)"
		}
		ds, ok := scopeMap[s]
		if !ok {
			ds = &DashboardScope{Scope: s}
			scopeMap[s] = ds
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

	// DB size via optional DBSizer interface.
	if sizer, ok := proto.Store().(parchment.DBSizer); ok {
		result.DBSizeBytes, _ = sizer.DBSizeBytes(ctx)
	}
	return result, nil
}

// --- Inventory ---

// scriveInventory returns a count-by-kind/status summary of all artifacts.
func scriveInventory(ctx context.Context, proto *parchment.Protocol) (*InventoryResult, error) {
	all, err := proto.ListArtifacts(ctx, parchment.ListInput{})
	if err != nil {
		return nil, err
	}
	motdKinds := proto.Schema().MotdKinds()
	r := &InventoryResult{
		Total:    len(all),
		ByKind:   make(map[string]int),
		ByStatus: make(map[string]int),
		Tracked:  make(map[string][]*parchment.Artifact),
	}
	for _, art := range all {
		r.ByKind[art.Kind]++
		r.ByStatus[art.Status]++
		if def, ok := motdKinds[art.Kind]; ok && art.Status == def.ActiveStatus {
			r.Tracked[art.Kind] = append(r.Tracked[art.Kind], art)
		}
	}
	return r, nil
}

// --- SetGoal ---

// scriveSetGoal archives any existing active goal and creates a new one
// with a linked root artifact.
func scriveSetGoal(ctx context.Context, proto *parchment.Protocol, in SetGoalInput) (*SetGoalResult, error) {
	if in.Title == "" {
		return nil, fmt.Errorf("title is required") //nolint:err113 // agent-facing error, inline message is the contract
	}
	schema := proto.Schema()
	goalKind, goalDef := schema.GoalKind()
	if goalKind == "" {
		return nil, fmt.Errorf("no kind with is_goal_kind=true in schema") //nolint:err113 // agent-facing error, inline message is the contract
	}

	scope := in.Scope
	existing, _ := proto.ListArtifacts(ctx, parchment.ListInput{
		Kind: goalKind, Status: goalDef.ActiveStatus, Scope: scope,
	})

	archived := make([]*parchment.Artifact, 0, len(existing))
	for _, old := range existing {
		results, err := proto.SetField(ctx, []string{old.ID}, parchment.FieldStatus,
			schema.ReadonlyStatuses[0], parchment.SetFieldOptions{Force: true})
		if err != nil || (len(results) > 0 && !results[0].OK) {
			continue
		}
		updated, _ := proto.GetArtifact(ctx, old.ID)
		if updated != nil {
			archived = append(archived, updated)
		}
	}

	goal, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: goalKind, Title: in.Title, Scope: scope,
		Status: goalDef.ActiveStatus,
	})
	if err != nil {
		return nil, fmt.Errorf("create goal: %w", err)
	}

	rootKind := in.Kind
	if rootKind == "" {
		rootKind = goalKind
	}
	root, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: rootKind, Title: in.Title, Scope: scope,
		Links: map[string][]string{parchment.RelJustifies: {goal.ID}},
	})
	if err != nil {
		return nil, fmt.Errorf("create root: %w", err)
	}

	return &SetGoalResult{Goal: goal, Root: root, Archived: archived}, nil
}

// --- Drain (filesystem ops, no Protocol dependency) ---

// scriveDrainDiscover walks path and returns all .md files that are
// candidates for legacy drain cleanup.
func scriveDrainDiscover(_ context.Context, path string) ([]DrainEntry, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required") //nolint:err113 // agent-facing error, inline message is the contract
	}
	var entries []DrainEntry
	err := filepath.Walk(path, func(fpath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") || strings.HasPrefix(info.Name(), "_") {
			return nil
		}
		rel, _ := filepath.Rel(path, fpath)
		entries = append(entries, DrainEntry{
			Path: fpath, Dir: filepath.Dir(rel),
			Filename: info.Name(), SizeB: info.Size(),
		})
		return nil
	})
	return entries, err
}

// scriveDrainCleanup removes all drain-candidate files under path.
func scriveDrainCleanup(ctx context.Context, path string) (int, error) {
	entries, err := scriveDrainDiscover(ctx, path)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if err := os.Remove(e.Path); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// --- Component labels (Scribe convention) ---

var componentLabelRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*:.+/.+$`)

// isComponentLabel reports whether s matches the Scribe component label format.
func isComponentLabel(s string) bool {
	return componentLabelRe.MatchString(strings.TrimSpace(s))
}
