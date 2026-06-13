package cmds

import (
	"context"
	"fmt"
	"regexp"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
)

// uuidRe matches the canonical UUID format produced by GenerateUUID.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(id string) bool { return uuidRe.MatchString(id) }

// MigrateIDsCmd renames all non-UUID artifact IDs to UUIDs.
// System artifacts in _schema scope are skipped — their IDs are read by
// the seeding code and must stay stable.
func MigrateIDsCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate-ids",
		Short: "Rename all legacy sequential IDs (LCS-TSK-42) to UUIDs",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()

			all, err := svc.Proto.ListArtifacts(ctx, parchment.ListInput{})
			if err != nil {
				return err
			}

			var renamed, skipped int
			for _, art := range all {
				if isUUID(art.ID) {
					skipped++
					continue
				}
				// Skip system artifacts — seeding code reads them by ID.
				if art.Label(parchment.LabelPrefixScope) == parchment.SchemaScope {
					skipped++
					continue
				}
				newID := parchment.GenerateUUID()
				if dryRun {
					fmt.Printf("would rename %s → %s  %s\n", art.ID, newID[:8], art.Title)
					renamed++
					continue
				}
				if err := svc.Proto.Store().RenameID(ctx, art.ID, newID); err != nil {
					return fmt.Errorf("rename %s: %w", art.ID, err)
				}
				fmt.Printf("renamed %s → %s  %s\n", art.ID, newID[:8], art.Title)
				renamed++
			}

			fmt.Printf("\n%d renamed, %d already UUID or skipped\n", renamed, skipped)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print renames without applying")
	return cmd
}
