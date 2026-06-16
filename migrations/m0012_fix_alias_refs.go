package migrations

import (
	"context"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

func migrateFixAliasRefs(ctx context.Context, proto *parchment.Protocol) error {
	store := proto.Store()

	sqlStore, ok := store.(interface {
		Writer() interface {
			ExecContext(context.Context, string, ...any) (any, error)
		}
	})
	if !ok {
		return nil
	}
	w := sqlStore.Writer()

	// Update artifact_aliases rows where artifact_id no longer matches any
	// artifact.id (stale from m0011 rename). Resolve via the artifacts.alias
	// column which was set to the old UUID during RenameID.
	res, err := w.ExecContext(ctx, `
		UPDATE artifact_aliases
		SET artifact_id = (
			SELECT a.id FROM artifacts a WHERE a.alias = artifact_aliases.artifact_id
		)
		WHERE artifact_id NOT IN (SELECT id FROM artifacts)
		  AND artifact_id IN (SELECT alias FROM artifacts WHERE alias != '')
	`)
	if err != nil {
		slog.WarnContext(ctx, "migration 0012: fix alias refs failed", slog.Any(parchment.LogKeyError, err))
		return nil
	}

	var fixed int64
	if r, ok := res.(interface{ RowsAffected() (int64, error) }); ok {
		fixed, _ = r.RowsAffected()
	}

	slog.InfoContext(ctx, "migration 0012: fixed stale alias references",
		slog.Int64(parchment.LogKeyCount, fixed))
	return nil
}
