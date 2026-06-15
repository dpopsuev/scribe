//nolint:goconst,misspell // migration-only status map; "cancelled" is the stored value (British spelling)
package migrations

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

var statusRenames = map[string]map[string]string{
	"effort.task": {
		"draft": "work.draft", "active": "work.active", "current": "work.active",
		"open": "work.active", "todo": "work.draft",
		"done": "work.complete", "complete": "work.complete", "mature": "work.complete",
		"cancelled": "cancelled", "canceled": "cancelled",
	},
	"effort.goal": {
		"draft": "work.draft", "active": "work.active", "current": "work.active",
		"open": "work.active",
		"done": "work.complete", "complete": "work.complete", "mature": "work.complete",
		"cancelled": "cancelled", "canceled": "cancelled",
		"retired": "archived",
	},
	"effort.campaign": {
		"draft": "work.draft", "active": "work.active", "current": "work.active",
		"open": "work.active",
		"done": "work.complete", "complete": "work.complete",
		"cancelled": "cancelled", "canceled": "cancelled",
	},
	"intent.bug": {
		"draft": "work.draft", "active": "work.active",
		"done": "work.complete", "complete": "work.complete",
		"cancelled": "cancelled", "canceled": "cancelled",
		"retired": "archived",
	},
	"intent.spec": {
		"draft": "work.draft", "active": "decision.proposed", "current": "decision.proposed",
		"complete":  "decision.accepted",
		"proposed":  "decision.proposed",
		"cancelled": "cancelled", "canceled": "cancelled",
	},
	"intent.decision": {
		"draft": "decision.proposed", "active": "decision.proposed",
		"proposed": "decision.proposed",
	},
	"intent.need": {
		"draft": "work.draft", "active": "decision.proposed",
		"open":      "work.draft",
		"complete":  "decision.accepted",
		"cancelled": "cancelled", "canceled": "cancelled",
	},
	"knowledge.note": {
		"active": "note.fleeting", "draft": "note.fleeting",
		"mature":   "note.mature",
		"fleeting": "note.fleeting",
	},
	"support.doc": {
		"draft": "work.draft", "active": "work.active",
		"done": "work.complete", "complete": "work.complete",
	},
	"support.ref": {
		"draft": "work.draft", "active": "work.active",
	},
}

func migrateStatusNamespace(ctx context.Context, proto *parchment.Protocol) error {
	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var migrated int
	for _, art := range all {
		kind := art.Label(parchment.LabelPrefixKind)
		if kind == "" {
			continue
		}
		renames, ok := statusRenames[kind]
		if !ok {
			continue
		}
		status := parchment.StatusFromLabels(art.Labels)
		newStatus, ok := renames[status]
		if !ok || newStatus == status {
			continue
		}

		art.Labels = replaceStatusLabel(art.Labels, status, newStatus)
		if err := proto.Store().Put(ctx, art); err != nil {
			slog.WarnContext(ctx, "migration 0006: put failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		migrated++
	}
	slog.InfoContext(ctx, "migration 0006: renamed legacy statuses",
		slog.Int(parchment.LogKeyCount, migrated))
	return nil
}

func replaceStatusLabel(labels []string, oldStatus, newStatus string) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		if l == oldStatus || l == "status:"+oldStatus {
			continue
		}
		out = append(out, l)
	}
	if !slices.Contains(out, newStatus) && !slices.Contains(out, "status:"+newStatus) {
		if strings.Contains(newStatus, ".") {
			out = append(out, newStatus)
		} else {
			out = append(out, "status:"+newStatus)
		}
	}
	return out
}
