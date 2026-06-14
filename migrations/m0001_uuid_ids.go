package migrations

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	parchment "github.com/dpopsuev/parchment"
)

const logKeyOldID = "old_id"

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// migrateUUIDs renames all non-UUID artifact IDs to UUIDs.
// System artifacts in _schema scope are skipped — their IDs are referenced by
// seeding code and must remain stable.
// Already-UUID IDs are skipped. The migration is safe to re-run.
func migrateUUIDs(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}
	var renamed int
	for _, art := range all {
		if uuidRe.MatchString(art.ID) {
			continue
		}
		if art.Label(parchment.LabelPrefixScope) == parchment.SchemaScope {
			continue
		}
		newID := parchment.GenerateUUID()
		if err := proto.Store().RenameID(ctx, art.ID, newID); err != nil {
			slog.WarnContext(ctx, "migrate: could not rename artifact ID; skipping",
				slog.String(logKeyOldID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		renamed++
	}
	if renamed > 0 {
		fmt.Printf("  renamed %d sequential IDs to UUIDs\n", renamed)
	}
	return nil
}
