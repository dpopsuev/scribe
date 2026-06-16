package migrations

import (
	"context"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

func migrateSlugIDs(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var renamed int
	for _, art := range all {
		if art.Title == "" {
			continue
		}
		slug := parchment.Slugify(art.Title)
		if slug == art.ID {
			continue
		}
		if err := proto.Store().RenameID(ctx, art.ID, slug); err != nil {
			slog.WarnContext(ctx, "migration 0011: rename failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		renamed++
	}

	slog.InfoContext(ctx, "migration 0011: renamed IDs to title slugs",
		slog.Int(parchment.LogKeyCount, renamed))
	return nil
}
