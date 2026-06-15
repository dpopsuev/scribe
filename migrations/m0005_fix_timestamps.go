package migrations

import (
	"context"
	"database/sql"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

func migrateFixTimestamps(ctx context.Context, proto *parchment.Protocol) error {
	db, ok := proto.Store().(interface{ Writer() *sql.DB })
	if !ok {
		return nil
	}
	w := db.Writer()
	for _, col := range []string{"created_at", "updated_at", "inserted_at"} {
		q := "UPDATE artifacts SET " + col + " = REPLACE(" + col + ", ' ', 'T') || 'Z' WHERE " + col + " NOT LIKE '%T%' AND " + col + " != ''" //nolint:gosec // col is a compile-time constant from the loop
		res, err := w.ExecContext(ctx, q)
		if err != nil {
			slog.WarnContext(ctx, "migration 0005: fix timestamps failed",
				slog.String(parchment.LogKeyField, col), slog.Any(parchment.LogKeyError, err))
			continue
		}
		n, _ := res.RowsAffected()
		if n > 0 {
			slog.InfoContext(ctx, "migration 0005: fixed timestamps",
				slog.String(parchment.LogKeyField, col), slog.Int64(parchment.LogKeyCount, n))
		}
	}
	return nil
}
