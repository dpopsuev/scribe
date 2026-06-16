package migrations

import (
	"context"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

const kindKnowledgeConcept = "knowledge.concept"
const kindSupportConfig = "support.config"

func migrateArchiveOrphans(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var archived int
	for _, art := range all {
		kind := art.Label(parchment.LabelPrefixKind)
		if kind == "" || kind == kindKnowledgeConcept || kind == kindSupportConfig {
			continue
		}

		out, _ := proto.Store().Neighbors(ctx, art.ID, "", parchment.Outgoing)
		in, _ := proto.Store().Neighbors(ctx, art.ID, "", parchment.Incoming)
		if len(out) > 0 || len(in) > 0 {
			continue
		}

		status := parchment.StatusFromLabels(art.Labels)
		if status == "status:archived" || status == "status:retired" {
			continue
		}

		_, err := proto.SetField(ctx, []string{art.ID}, "status", "status:archived", parchment.SetFieldOptions{Force: true})
		if err != nil {
			slog.WarnContext(ctx, "migration 0010: archive failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		archived++
	}

	slog.InfoContext(ctx, "migration 0010: archived orphan artifacts",
		slog.Int(parchment.LogKeyCount, archived))
	return nil
}
