package migrations

import (
	"context"
	"log/slog"
	"slices"

	parchment "github.com/dpopsuev/parchment"
)

func migrateUnkindedLegacy(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	const kindLabel = parchment.LabelPrefixKind + "knowledge.note"
	var migrated int
	for _, art := range all {
		if art.Label(parchment.LabelPrefixKind) != "" {
			continue
		}
		if art.Label(parchment.LabelPrefixScope) == parchment.SchemaScope {
			continue
		}
		if slices.Contains(art.Labels, kindLabel) {
			continue
		}
		art.Labels = append(art.Labels, kindLabel)
		if err := proto.Store().Put(ctx, art); err != nil {
			slog.WarnContext(ctx, "migration 0004: put failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		migrated++
	}
	slog.InfoContext(ctx, "migration 0004: assigned kind:knowledge.note to unkinded artifacts",
		slog.Int(parchment.LogKeyCount, migrated))
	return nil
}
