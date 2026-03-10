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

// Store is the persistence interface for all governance artifacts.
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

	Close() error
}

// DBSizer is an optional interface for stores that can report database size.
// SQLiteStore implements this.
type DBSizer interface {
	DBSizeBytes(ctx context.Context) (int64, error)
}
