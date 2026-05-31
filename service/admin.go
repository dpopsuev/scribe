package service

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

// --- Result types ---

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

// SetGoalResult is returned by SetGoal.
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

// --- Service methods ---

const defaultDashboardStaleDays = 30
const defaultDashboardStaleCap = 10

// Motd returns the message of the day: active campaigns, goals, and schema warnings.
func (s *Service) Motd(ctx context.Context) (*MotdResult, error) { //nolint:cyclop,funlen,gocyclo // motd report is inherently multi-check
	result := &MotdResult{
		SchemaHash: s.Proto.Schema().Hash(),
	}
	schema := s.Proto.Schema()

	for kind, def := range schema.MotdKinds() { //nolint:gocritic // rangeValCopy: acceptable
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
		if !schema.IsTerminal(art.Status) && isEffortKind { //nolint:nestif // motd check is inherently nested
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
			sc = "(none)"
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
	motdKinds := s.Proto.Schema().MotdKinds()
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

// DrainDiscover lists legacy .md files under path.
func (s *Service) DrainDiscover(_ context.Context, path string) ([]DrainEntry, error) {
	if path == "" {
		return nil, fmt.Errorf("path is required") //nolint:err113 // agent-facing error
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

// DrainCleanup removes all drain-candidate files under path.
func (s *Service) DrainCleanup(ctx context.Context, path string) (int, error) {
	entries, err := s.DrainDiscover(ctx, path)
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
