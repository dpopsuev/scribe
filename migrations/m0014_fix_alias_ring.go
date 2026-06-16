package migrations

import (
	"context"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

func migrateFixAliasRing(ctx context.Context, proto *parchment.Protocol) error {
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

	// Step 1: For stale rows where artifact_id is an old UUID, resolve
	// via the alias ring: if artifact_id appears as an alias in another row
	// that DOES point to a valid artifact, copy that artifact_id.
	_, _ = w.ExecContext(ctx, `
		UPDATE artifact_aliases AS target
		SET artifact_id = (
			SELECT source.artifact_id FROM artifact_aliases AS source
			WHERE source.alias = target.artifact_id
			  AND source.artifact_id IN (SELECT id FROM artifacts)
			LIMIT 1
		)
		WHERE target.artifact_id NOT IN (SELECT id FROM artifacts)
	`)

	// Step 2: Delete any remaining orphaned rows.
	res, _ := w.ExecContext(ctx, `
		DELETE FROM artifact_aliases
		WHERE artifact_id NOT IN (SELECT id FROM artifacts)
	`)
	var cleaned int64
	if r, ok := res.(interface{ RowsAffected() (int64, error) }); ok {
		cleaned, _ = r.RowsAffected()
	}

	slog.InfoContext(ctx, "migration 0014: fixed alias ring references",
		slog.Int64(parchment.LogKeyCount, cleaned))
	return nil
}
