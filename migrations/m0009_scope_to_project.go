package migrations

import (
	"context"
	"log/slog"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

const oldScopePrefix = "scope:"
const newProjectPrefix = "project:"

func migrateScopeToProject(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var migrated int
	for _, art := range all {
		changed := false
		for i, l := range art.Labels {
			if strings.HasPrefix(l, oldScopePrefix) {
				art.Labels[i] = newProjectPrefix + strings.TrimPrefix(l, oldScopePrefix)
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := proto.Store().Put(ctx, art); err != nil {
			slog.WarnContext(ctx, "migration 0009: put failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		migrated++
	}

	store := proto.Store()
	if sqlStore, ok := store.(interface {
		Writer() interface {
			ExecContext(context.Context, string, ...any) (any, error)
		}
	}); ok {
		w := sqlStore.Writer()
		_, _ = w.ExecContext(ctx, "UPDATE artifact_labels SET label = REPLACE(label, 'scope:', 'project:') WHERE label LIKE 'scope:%'")
	}

	slog.InfoContext(ctx, "migration 0009: renamed scope: → project: labels",
		slog.Int(parchment.LogKeyCount, migrated))
	return nil
}
