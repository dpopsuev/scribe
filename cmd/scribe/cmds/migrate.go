package cmds

import (
	"context"
	"fmt"

	"github.com/dpopsuev/scribe/migrations"
	"github.com/spf13/cobra"
)

// MigrateCmd applies pending data migrations to the database.
func MigrateCmd() *cobra.Command {
	var dryRun bool
	var status bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending data migrations",
		Long: `Apply all pending data migrations in order. Each migration is tracked
in the migrations table and skipped on subsequent runs.

Use --status to list which migrations have been applied and which are pending.
Use --dry-run to preview pending migrations without applying them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()

			if status {
				entries, err := migrations.Status(ctx, svc.Proto)
				if err != nil {
					return err
				}
				for _, e := range entries {
					mark := "pending"
					if e.Applied {
						mark = "applied"
					}
					fmt.Printf("%-8s %s  %s\n", mark, e.ID, e.Description)
				}
				return nil
			}

			return migrations.RunPending(ctx, svc.Proto, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "describe pending migrations without applying them")
	cmd.Flags().BoolVar(&status, "status", false, "list all migrations and their applied state")
	return cmd
}
