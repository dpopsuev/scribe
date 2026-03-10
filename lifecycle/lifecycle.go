package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/store"
)

var (
	ErrArchived    = errors.New("artifact is archived and read-only")
	ErrNotArchived = errors.New("only archived artifacts can be deleted; use --force to override")
)

// GuardPut rejects updates to archived artifacts.
func GuardPut(ctx context.Context, s store.Store, art *model.Artifact) error {
	existing, err := s.Get(ctx, art.ID)
	if err != nil {
		return nil // new artifact, no guard
	}
	if existing.Status == "archived" {
		return fmt.Errorf("%w: %s", ErrArchived, art.ID)
	}
	return nil
}

// GuardDelete rejects deletion of non-archived artifacts unless forced.
func GuardDelete(ctx context.Context, s store.Store, id string, force bool) error {
	if force {
		return nil
	}
	art, err := s.Get(ctx, id)
	if err != nil {
		return nil // doesn't exist, let store return the real error
	}
	if art.Status != "archived" {
		return fmt.Errorf("%w: %s (status: %s)", ErrNotArchived, id, art.Status)
	}
	return nil
}

// Archive transitions an artifact (and optionally its subtree) to archived status.
// Without cascade, it rejects if the artifact has non-archived children.
func Archive(ctx context.Context, s store.Store, id string, cascade bool) error {
	art, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if art.Status == "archived" {
		return nil
	}

	children, err := s.Children(ctx, id)
	if err != nil {
		return err
	}

	if cascade {
		for _, ch := range children {
			if err := Archive(ctx, s, ch.ID, true); err != nil {
				return fmt.Errorf("cascade archive %s: %w", ch.ID, err)
			}
		}
	} else {
		for _, ch := range children {
			if ch.Status != "archived" {
				return fmt.Errorf("cannot archive %s: child %s is %s (use --cascade to archive the whole tree)", id, ch.ID, ch.Status)
			}
		}
	}

	art.Status = "archived"
	return s.Put(ctx, art)
}

// Vacuum removes archived artifacts whose UpdatedAt is older than maxAge.
// If scope is non-empty, only artifacts in that scope are considered.
// Returns the IDs of deleted artifacts.
func Vacuum(ctx context.Context, s store.Store, maxAge time.Duration, scope string) ([]string, error) {
	f := model.Filter{Status: "archived"}
	if scope != "" {
		f.Scope = scope
	}
	arts, err := s.List(ctx, f)
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().UTC().Add(-maxAge)
	var deleted []string
	for _, art := range arts {
		if art.UpdatedAt.Before(cutoff) {
			if err := s.Delete(ctx, art.ID); err != nil {
				return deleted, fmt.Errorf("vacuum %s: %w", art.ID, err)
			}
			deleted = append(deleted, art.ID)
		}
	}
	return deleted, nil
}
