// Package migrations provides a tracked, ordered data migration system for Scribe.
//
// Each migration has a unique string ID (e.g. "0001_uuid_ids"), a description,
// and a Run function that receives the Protocol and returns an error.
// Applied migration IDs are stored in the migrations table via MigrationStore.
// Stores that do not implement MigrationStore (e.g. MemoryStore in tests) run
// every migration on each call — idempotent migrations handle this gracefully.
//
// Usage:
//
//	if err := migrations.RunPending(ctx, proto, false); err != nil {
//	    log.Fatal(err)
//	}
package migrations

import (
	"context"
	"fmt"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

const logKeyMigrationID = "migration_id"
const logKeyDescription = "description"

// Migration is a single, named data transformation.
type Migration struct {
	// ID is the unique identifier for this migration, used as the key in the
	// migrations table. Format: NNNN_short_description (e.g. "0001_uuid_ids").
	ID string
	// Description is a one-line human-readable summary shown by migrate --status.
	Description string
	// Run applies the migration. It must be idempotent — re-running it on an
	// already-migrated database must be safe and produce no unintended changes.
	Run func(ctx context.Context, proto *parchment.Protocol) error
}

// All is the ordered registry of all data migrations.
// Append new migrations to the end; never reorder or remove existing entries.
var All = []Migration{
	{
		ID:          "0001_uuid_ids",
		Description: "Rename legacy sequential IDs (LCS-TSK-42 style) to UUIDs",
		Run:         migrateUUIDs,
	},
	{
		ID:          "0002_kind_namespace",
		Description: "Rename flat kind labels to dot-namespaced (task → effort.task)",
		Run:         migrateKindNamespace,
	},
	{
		ID:          "0003_investigation_case",
		Description: "Rename tautological kind investigation.investigation → investigation.case",
		Run:         migrateInvestigationCase,
	},
	{
		ID:          "0004_unkinded_legacy",
		Description: "Assign kind:knowledge.note to legacy artifacts with no kind label",
		Run:         migrateUnkindedLegacy,
	},
	{
		ID:          "0005_fix_timestamps",
		Description: "Convert space-separated timestamps to RFC3339 format",
		Run:         migrateFixTimestamps,
	},
	{
		ID:          "0006_status_namespace",
		Description: "Rename legacy statuses (active→work.active, done→work.complete) per kind lifecycle",
		Run:         migrateStatusNamespace,
	},
	{
		ID:          "0007_schema_kinds",
		Description: "Rename old schema kinds (rule→knowledge.concept, definition→support.config, skill→support.template)",
		Run:         migrateSchemaKinds,
	},
	{
		ID:          "0008_resolve_legacy_ids",
		Description: "Replace stale HGM-*/LCS-*/SCR-* ID references in sections with UUIDs via alias table",
		Run:         migrateResolveLegacyIDs,
	},
	{
		ID:          "0009_scope_to_project",
		Description: "Rename scope:X labels to project:X for composable multi-label organization",
		Run:         migrateScopeToProject,
	},
	{
		ID:          "0010_archive_orphans",
		Description: "Archive disconnected artifacts with no edges (orphan housekeeping)",
		Run:         migrateArchiveOrphans,
	},
	{
		ID:          "0011_slug_ids",
		Description: "Rename UUID IDs to human-readable title-derived slugs",
		Run:         migrateSlugIDs,
	},
	{
		ID:          "0012_fix_alias_refs",
		Description: "Fix stale artifact_aliases references left by slug ID migration",
		Run:         migrateFixAliasRefs,
	},
	{
		ID:          "0013_fix_alias_cascade",
		Description: "Cascade artifact_aliases references after slug ID rename",
		Run:         migrateFixAliasCascade,
	},
}

// RunPending applies all migrations that have not yet been recorded as applied.
// If dryRun is true, migrations are described but not executed and not marked applied.
// If the store does not implement MigrationStore, all migrations are run every time.
func RunPending(ctx context.Context, proto *parchment.Protocol, dryRun bool) error {
	ms, tracked := proto.Store().(parchment.MigrationStore)

	applied := map[string]bool{}
	if tracked {
		ids, err := ms.AppliedMigrations(ctx)
		if err != nil {
			return fmt.Errorf("migrations: load applied: %w", err)
		}
		for _, id := range ids {
			applied[id] = true
		}
	}

	for _, m := range All {
		if applied[m.ID] {
			continue
		}
		if dryRun {
			fmt.Printf("pending  %s  %s\n", m.ID, m.Description)
			continue
		}
		slog.InfoContext(ctx, "running migration", slog.String(logKeyMigrationID, m.ID), slog.String(logKeyDescription, m.Description))
		if err := m.Run(ctx, proto); err != nil {
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
		if tracked {
			if err := ms.MarkMigrated(ctx, m.ID); err != nil {
				return fmt.Errorf("migration %s: mark applied: %w", m.ID, err)
			}
		}
		slog.InfoContext(ctx, "migration applied", slog.String(logKeyMigrationID, m.ID))
	}
	return nil
}

// Status returns all migrations with a flag indicating whether each has been applied.
// Returns an error only if reading the applied set fails.
func Status(ctx context.Context, proto *parchment.Protocol) ([]StatusEntry, error) {
	ms, tracked := proto.Store().(parchment.MigrationStore)

	applied := map[string]bool{}
	if tracked {
		ids, err := ms.AppliedMigrations(ctx)
		if err != nil {
			return nil, fmt.Errorf("migrations: load applied: %w", err)
		}
		for _, id := range ids {
			applied[id] = true
		}
	}

	entries := make([]StatusEntry, len(All))
	for i, m := range All {
		entries[i] = StatusEntry{Migration: m, Applied: applied[m.ID]}
	}
	return entries, nil
}

// StatusEntry pairs a Migration with its applied state.
type StatusEntry struct {
	Migration
	Applied bool
}
