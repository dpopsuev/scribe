package migrations

import (
	"context"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

func migrateFixAliasCascade(ctx context.Context, proto *parchment.Protocol) error {
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

	// Stale alias references: artifact_aliases.artifact_id points to an old
	// UUID that was renamed by m0011. The old UUID is now stored as an alias
	// itself (added by RenameID step 6). Resolve: find the artifact whose
	// alias column matches the stale artifact_id, then update to its current id.
	res, err := w.ExecContext(ctx, `
		UPDATE artifact_aliases
		SET artifact_id = (
			SELECT a.id FROM artifacts a WHERE a.alias = artifact_aliases.artifact_id LIMIT 1
		)
		WHERE artifact_id NOT IN (SELECT id FROM artifacts)
	`)
	if err != nil {
		slog.WarnContext(ctx, "migration 0013: fix alias cascade failed", slog.Any(parchment.LogKeyError, err))
		return nil
	}

	var fixed int64
	if r, ok := res.(interface{ RowsAffected() (int64, error) }); ok {
		fixed, _ = r.RowsAffected()
	}

	// Remove orphaned aliases that couldn't be resolved (artifact_id still invalid).
	res2, _ := w.ExecContext(ctx, `
		DELETE FROM artifact_aliases
		WHERE artifact_id NOT IN (SELECT id FROM artifacts)
	`)
	var cleaned int64
	if r, ok := res2.(interface{ RowsAffected() (int64, error) }); ok {
		cleaned, _ = r.RowsAffected()
	}

	slog.InfoContext(ctx, "migration 0013: fixed alias cascade",
		slog.Int64(parchment.LogKeyCount, fixed), slog.Int64("cleaned_orphans", cleaned)) //nolint:sloglint // one-off migration key
	return nil
}
