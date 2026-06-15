//nolint:goconst // migration-only kind map
package migrations

import (
	"context"
	"log/slog"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

var schemaKindRenames = map[string]string{
	"rule":       "knowledge.concept",
	"definition": "support.config",
	"skill":      "support.template",
}

func migrateSchemaKinds(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var migrated int
	for _, art := range all {
		newKind, ok := schemaKindRenames[art.Label(parchment.LabelPrefixKind)]
		if !ok {
			continue
		}
		oldLabel := parchment.LabelPrefixKind + art.Label(parchment.LabelPrefixKind)
		newLabel := parchment.LabelPrefixKind + newKind
		var updated []string
		for _, l := range art.Labels {
			if l == oldLabel {
				updated = append(updated, newLabel)
			} else {
				updated = append(updated, l)
			}
		}
		art.Labels = updated
		art.Extra["original_kind"] = strings.TrimPrefix(oldLabel, parchment.LabelPrefixKind)
		if err := proto.Store().Put(ctx, art); err != nil {
			slog.WarnContext(ctx, "migration 0007: put failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		migrated++
	}
	slog.InfoContext(ctx, "migration 0007: renamed schema kinds",
		slog.Int(parchment.LogKeyCount, migrated))
	return nil
}
