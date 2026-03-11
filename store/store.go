package store

import (
	"context"

	"github.com/dpopsuev/scribe/model"
)

// Direction constrains edge traversal.
type Direction int

const (
	Outgoing Direction = iota
	Incoming
	Both
)

// WalkFn is called for each edge during graph traversal.
// Return false to stop walking.
type WalkFn func(depth int, edge model.Edge) (cont bool)

// Store is the persistence interface for all work graph artifacts.
type Store interface {
	// Artifact CRUD. Put reconciles edges from Parent, DependsOn, and Links.
	Put(ctx context.Context, art *model.Artifact) error
	Get(ctx context.Context, id string) (*model.Artifact, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, f model.Filter) ([]*model.Artifact, error)

	// Explicit edge operations.
	AddEdge(ctx context.Context, e model.Edge) error
	RemoveEdge(ctx context.Context, e model.Edge) error
	Neighbors(ctx context.Context, id string, rel string, dir Direction) ([]model.Edge, error)
	Walk(ctx context.Context, root string, rel string, dir Direction, maxDepth int, fn WalkFn) error

	// Children returns artifacts that have parentID as their parent.
	Children(ctx context.Context, parentID string) ([]*model.Artifact, error)

	// NextID atomically generates the next ID for the given prefix (e.g. "CON").
	NextID(ctx context.Context, prefix string) (string, error)

	// SeedSequence sets the counter for prefix so the next NextID call returns at least val.
	// If force is true, the counter is set unconditionally (even if lowering).
	SeedSequence(ctx context.Context, prefix string, val uint64, force bool) error

	// NextScopedID atomically generates the next scoped ID for scope_key+kind_code.
	NextScopedID(ctx context.Context, scopeKey, kindCode string) (string, error)

	// NextSeq atomically returns the next sequence number for an arbitrary key.
	NextSeq(ctx context.Context, key string) (int64, error)

	// Scope key registry.
	GetScopeKey(ctx context.Context, scope string) (key string, auto bool, err error)
	SetScopeKey(ctx context.Context, scope, key string, auto bool) error
	ListScopeKeys(ctx context.Context) (map[string]string, error)

	// Scope labels.
	SetScopeLabels(ctx context.Context, scope string, labels []string) error
	GetScopeLabels(ctx context.Context, scope string) ([]string, error)
	ScopesByLabel(ctx context.Context, label string) ([]string, error)
	ListScopeInfo(ctx context.Context) ([]ScopeInfo, error)

	Close() error
}

// DBSizer is an optional interface for stores that can report database size.
// SQLiteStore implements this.
type DBSizer interface {
	DBSizeBytes(ctx context.Context) (int64, error)
}
