package service

import (
	"context"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

// ResolvedContent holds live content fetched from a source backend.
type ResolvedContent struct {
	Sections []parchment.Section `json:"sections,omitempty"`
	Extra    map[string]any      `json:"extra,omitempty"`
	Fresh    bool                `json:"fresh"`
	Stale    bool                `json:"stale"`
}

// Resolver fetches live content from an external source backend.
type Resolver interface {
	Resolve(ctx context.Context, refID string) (*ResolvedContent, error)
}

// Resolve fetches live content for a referenced artifact.
// Returns the stored artifact merged with fresh content from the source.
// On resolver failure, returns the stored artifact with stale=true, fresh=false.
func Resolve(ctx context.Context, svc *Service, id string, resolvers map[string]Resolver) (*ResolvedContent, error) {
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("artifact %s: %w", id, err)
	}

	backend := RefBackend(art)
	refID := RefID(art)
	if backend == "" || refID == "" {
		return nil, fmt.Errorf("artifact %s has no ref_backend/ref_id", id) //nolint:err113 // user-facing hint
	}

	resolver, ok := resolvers[backend]
	if !ok {
		return &ResolvedContent{
			Sections: art.Sections,
			Extra:    art.Extra,
			Fresh:    false,
			Stale:    IsStale(art),
		}, nil
	}

	resolved, err := resolver.Resolve(ctx, refID)
	if err != nil {
		return &ResolvedContent{
			Sections: art.Sections,
			Extra:    art.Extra,
			Fresh:    false,
			Stale:    true,
		}, nil
	}

	return resolved, nil
}
