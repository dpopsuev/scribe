package migrations

import (
	"context"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

func migrateFixTimestamps(ctx context.Context, proto *parchment.Protocol) error {
	sqlStore, ok := proto.Store().(interface {
		Writer() interface {
			ExecContext(context.Context, string, ...any) (any, error)
		}
	})
	if !ok {
		return nil
	}
	w := sqlStore.Writer()
	for _, col := range []string{"created_at", "updated_at", "inserted_at"} {
		q := "UPDATE artifacts SET " + col + " = REPLACE(" + col + ", ' ', 'T') || 'Z' WHERE " + col + " NOT LIKE '%T%' AND " + col + " != ''" //nolint:gosec // col is a compile-time constant from the loop
		res, err := w.ExecContext(ctx, q)
		if err != nil {
			slog.WarnContext(ctx, "migration 0005: fix timestamps failed",
				slog.String(parchment.LogKeyField, col), slog.Any(parchment.LogKeyError, err))
			continue
		}
		if r, ok := res.(interface{ RowsAffected() (int64, error) }); ok {
			if n, _ := r.RowsAffected(); n > 0 {
				slog.InfoContext(ctx, "migration 0005: fixed timestamps",
					slog.String(parchment.LogKeyField, col), slog.Int64(parchment.LogKeyCount, n))
			}
		}
	}
	return nil
}
