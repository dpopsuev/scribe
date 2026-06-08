package cmds

import (
	"context"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
)

func MigrateCmd() *cobra.Command {
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Database migration utilities",
	}
	migrate.AddCommand(migrateLabelsCmd())
	return migrate
}

func migrateLabelsCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "labels",
		Short: "Backfill kind:X, scope:X, status:X labels onto pre-ECS artifacts",
		Long: `Backfills system labels onto artifacts that were created before
parchment began seeding kind:, scope:, status:, and priority: labels
automatically. Safe to run multiple times — idempotent.

Run on a database clone first to validate:
  cp scribe.sqlite scribe.sqlite.backup
  scribe migrate labels --dry-run
  scribe migrate labels`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := MustConfig()

			s, err := parchment.OpenSQLiteConfig(cfg.SQLiteConfig())
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close() //nolint:errcheck // deferred close in CLI command

			ctx := context.Background()

			// Count before.
			artsBefore, err := s.List(ctx, parchment.Filter{})
			if err != nil {
				return fmt.Errorf("count before: %w", err)
			}
			labelledBefore := 0
			for _, art := range artsBefore {
				for _, l := range art.Labels {
					if len(l) >= 5 && l[:5] == "kind:" {
						labelledBefore++
						break
					}
				}
			}

			if dryRun {
				fmt.Printf("dry-run: %d total artifacts, %d already have kind: label, %d would be migrated\n",
					len(artsBefore), labelledBefore, len(artsBefore)-labelledBefore)
				return nil
			}

			toMigrate := len(artsBefore) - labelledBefore
			fmt.Printf("migrating %d artifacts (progress logged every 500)...\n", toMigrate)

			if err := parchment.MigrateSystemLabels(ctx, s); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}

			// Count after.
			artsAfter, _ := s.List(ctx, parchment.Filter{})
			labelledAfter := 0
			for _, art := range artsAfter {
				for _, l := range art.Labels {
					if len(l) >= 5 && l[:5] == "kind:" {
						labelledAfter++
						break
					}
				}
			}
			migrated := labelledAfter - labelledBefore
			fmt.Printf("migrated %d artifacts — %d now carry system labels (kind:, scope:, status:, priority:)\n",
				migrated, labelledAfter)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be changed without writing")
	return cmd
}
