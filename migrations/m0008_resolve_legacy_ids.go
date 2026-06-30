package migrations

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

var legacyIDPattern = regexp.MustCompile(`\b([A-Z]{2,4}-(?:TSK|GOL|CMP|SPC|BUG|ADR|REF|DOC|NED)-\d+)\b`)

func migrateResolveLegacyIDs(ctx context.Context, proto *parchment.Protocol) error {
	aliasMap, err := buildAliasMap(ctx, proto.Store())
	if err != nil {
		return err
	}
	if len(aliasMap) == 0 {
		return nil
	}

	all, err := proto.Store().List(ctx, parchment.Filter{})
	if err != nil {
		return err
	}

	var migrated int
	for _, art := range all {
		changed := false
		for i, sec := range art.Sections {
			replaced := legacyIDPattern.ReplaceAllStringFunc(sec.Text, func(match string) string {
				if uuid, ok := aliasMap[match]; ok {
					changed = true
					return uuid
				}
				return match
			})
			if changed {
				art.Sections[i].Text = replaced
			}
		}
		if !changed {
			continue
		}
		if err := proto.Store().Put(ctx, art); err != nil {
			slog.WarnContext(ctx, "migration 0008: put failed",
				slog.String(parchment.LogKeyID, art.ID), slog.Any(parchment.LogKeyError, err))
			continue
		}
		migrated++
	}
	slog.InfoContext(ctx, "migration 0008: resolved legacy ID references in sections",
		slog.Int(parchment.LogKeyCount, migrated))
	return nil
}

func buildAliasMap(ctx context.Context, store parchment.Store) (map[string]string, error) {
	all, err := store.List(ctx, parchment.Filter{})
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, art := range all {
		aliases, _ := store.ListAliases(ctx, art.ID)
		for _, a := range aliases {
			if strings.Contains(a, "-") {
				m[a] = art.ID
			}
		}
		extra, ok := art.Extra["aliases"]
		if !ok {
			continue
		}
		raw, err := json.Marshal(extra)
		if err != nil {
			continue
		}
		var extraAliases []string
		if json.Unmarshal(raw, &extraAliases) != nil {
			continue
		}
		for _, a := range extraAliases {
			m[a] = art.ID
		}
	}
	return m, nil
}
