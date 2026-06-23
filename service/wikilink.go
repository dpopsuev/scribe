package service

import (
	"context"
	"log/slog"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// CrossSourceResolver resolves wikilink targets across multiple scopes.
// It extends parchment's built-in title resolution with scope-qualified
// lookups: [[scope/Target]] resolves "Target" within the named scope.
type CrossSourceResolver struct {
	store parchment.Store
}

// NewCrossSourceResolver creates a resolver backed by the given store.
func NewCrossSourceResolver(store parchment.Store) *CrossSourceResolver {
	return &CrossSourceResolver{store: store}
}

// ResolvedRef is a wikilink target that has been resolved to an artifact ID,
// optionally across a scope boundary.
type ResolvedRef struct {
	SourceRef string // original wikilink text
	TargetID  string // resolved artifact ID
	Scope     string // scope where the target was found (empty = unscoped)
	Relation  string // relation type (empty = "mentions")
}

// ResolveRef resolves a single wikilink reference. If the target contains
// a "/" separator, the prefix is treated as a scope qualifier.
//
// Resolution order:
//  1. scope/Target — search within the named scope
//  2. Target (no slash) — search by exact title match across all scopes
//  3. Fallback to FTS prefix search
func (r *CrossSourceResolver) ResolveRef(ctx context.Context, ref parchment.WikilinkRef) ResolvedRef {
	result := ResolvedRef{
		SourceRef: ref.Target,
		Relation:  ref.Relation,
	}
	if result.Relation == "" {
		result.Relation = edgeMentions
	}

	scope, target := splitScopeTarget(ref.Target)
	if scope != "" {
		result.Scope = scope
		result.TargetID = r.resolveInScope(ctx, target, scope)
		return result
	}

	result.TargetID = r.resolveGlobal(ctx, target)
	return result
}

// ResolveAll resolves all wikilink refs extracted from text, returning
// only those that successfully resolved to artifact IDs.
func (r *CrossSourceResolver) ResolveAll(ctx context.Context, text string) []ResolvedRef {
	refs := parchment.ExtractWikilinkRefs(text)
	var resolved []ResolvedRef
	for _, ref := range refs {
		rr := r.ResolveRef(ctx, ref)
		if rr.TargetID != "" {
			resolved = append(resolved, rr)
		}
	}
	return resolved
}

// SyncCrossSourceWikilinks scans all sections of an artifact for wikilinks,
// resolves them across scopes, and syncs edges using wikilink provenance.
func (r *CrossSourceResolver) SyncCrossSourceWikilinks(ctx context.Context, store parchment.Store, artID string) ([]ResolvedRef, error) {
	art, err := store.Get(ctx, artID)
	if err != nil {
		return nil, err
	}

	var allRefs []ResolvedRef
	seen := make(map[string]bool)
	for _, sec := range art.Sections {
		for _, rr := range r.ResolveAll(ctx, sec.Text) {
			key := rr.Relation + "\x00" + rr.TargetID
			if seen[key] || rr.TargetID == artID {
				continue
			}
			seen[key] = true
			allRefs = append(allRefs, rr)
		}
	}

	for _, rr := range allRefs {
		if err := store.AddEdgeSource(ctx, artID, rr.Relation, rr.TargetID, parchment.EdgeSourceWikilink); err != nil {
			slog.WarnContext(ctx, "cross-source wikilink add failed",
				slog.String("from", artID),     //nolint:sloglint // domain-specific key
				slog.String("to", rr.TargetID), //nolint:sloglint // domain-specific key
				slog.Any("error", err))         //nolint:sloglint // domain-specific key
		}
	}

	return allRefs, nil
}

func splitScopeTarget(target string) (scope, name string) {
	idx := strings.Index(target, "/")
	if idx < 0 {
		return "", target
	}
	return target[:idx], target[idx+1:]
}

func (r *CrossSourceResolver) resolveInScope(ctx context.Context, title, scope string) string {
	arts, err := r.store.List(ctx, parchment.Filter{
		Labels: []string{parchment.LabelPrefixScope + scope},
	})
	if err != nil {
		return ""
	}
	for _, art := range arts {
		if strings.EqualFold(art.Title, title) {
			return art.ID
		}
	}
	for _, art := range arts {
		if strings.HasPrefix(strings.ToLower(art.ID), strings.ToLower(title)) {
			return art.ID
		}
	}
	return ""
}

func (r *CrossSourceResolver) resolveGlobal(ctx context.Context, title string) string {
	ids, err := r.store.Search(ctx, title)
	if err != nil || len(ids) == 0 {
		return ""
	}
	for _, id := range ids {
		art, err := r.store.Get(ctx, id)
		if err != nil {
			continue
		}
		if strings.EqualFold(art.Title, title) {
			return id
		}
	}
	return ids[0]
}
