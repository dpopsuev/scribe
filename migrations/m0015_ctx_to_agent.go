package migrations

import (
	"context"
	"log/slog"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

func migrateCtxToAgent(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var migrated int
	for _, art := range all {
		var changed bool
		for i, l := range art.Labels {
			rewritten := strings.Replace(l, "kind:ctx.", "kind:agent.", 1)
			if rewritten == l {
				rewritten = strings.Replace(l, "ctx.active", "agent.active", 1)
			}
			if rewritten == l {
				rewritten = strings.Replace(l, "ctx.archived", "agent.archived", 1)
			}
			if rewritten == l {
				rewritten = strings.Replace(l, "ctx.ephemeral", "agent.ephemeral", 1)
			}
			if rewritten == l {
				rewritten = strings.Replace(l, "ctx.promoted", "agent.promoted", 1)
			}
			if rewritten == l {
				rewritten = strings.Replace(l, "ctx.permanent", "agent.permanent", 1)
			}
			if rewritten != l {
				art.Labels[i] = rewritten
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := proto.Store().Put(ctx, art); err != nil {
			slog.WarnContext(ctx, "migration 0015: put failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		migrated++
	}
	slog.InfoContext(ctx, "migration 0015: renamed ctx.* to agent.*",
		slog.Int(parchment.LogKeyCount, migrated))
	return nil
}
