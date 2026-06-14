package cmds

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
)

// uuidRe matches the canonical UUID format produced by GenerateUUID.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(id string) bool { return uuidRe.MatchString(id) }

// kindRenameMap maps old flat kind names to new namespaced kind names.
var kindRenameMap = map[string]string{
	"task":           "effort.task",
	"goal":           "effort.goal",
	"campaign":       "effort.campaign",
	"bug":            "intent.bug",
	"spec":           "intent.spec",
	"decision":       "intent.decision",
	"need":           "intent.need",
	"note":           "knowledge.note",
	"concept":        "knowledge.concept",
	"source":         "knowledge.source",
	"journal":        "knowledge.journal",
	"context":        "knowledge.context",
	"config":         "support.config",
	"doc":            "support.doc",
	"mirror":         "support.mirror",
	"paragraph":      "support.paragraph",
	"ref":            "support.ref",
	"rule":           "support.rule",
	"section":        "support.section",
	"template":       "support.template",
	"cause":          "investigation.cause",
	"investigation":  "investigation.investigation",
	"observation":    "investigation.observation",
	"code-interface": "code.interface",
	"code-test":      "code.test",
	"ctx-session":    "ctx.session",
	"ctx-tool-call":  "ctx.tool-call",
	"ctx-turn":       "ctx.turn",
}

// MigrateKindsCmd renames all stored kind labels from flat names to namespaced names.
func MigrateKindsCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate-kinds",
		Short: "Rename flat kind labels (task → effort.task) to namespaced format",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			ctx := context.Background()

			all, err := svc.Proto.Store().List(ctx, parchment.Filter{})
			if err != nil {
				return err
			}

			var migrated, skipped int
			for _, art := range all {
				current := art.Label(parchment.LabelPrefixKind)
				if current == "" {
					skipped++
					continue
				}
				newKind, ok := kindRenameMap[current]
				if !ok {
					skipped++
					continue
				}
				if dryRun {
					fmt.Printf("would rename kind:%s → kind:%s  [%s] %s\n", current, newKind, art.ID, art.Title)
					migrated++
					continue
				}
				newLabels := make([]string, 0, len(art.Labels))
				for _, l := range art.Labels {
					if strings.HasPrefix(l, parchment.LabelPrefixKind) {
						newLabels = append(newLabels, parchment.LabelPrefixKind+newKind)
					} else {
						newLabels = append(newLabels, l)
					}
				}
				art.Labels = newLabels
				if err := svc.Proto.Store().Put(ctx, art); err != nil {
					return fmt.Errorf("update %s: %w", art.ID, err)
				}
				fmt.Printf("migrated kind:%s → kind:%s  [%s] %s\n", current, newKind, art.ID, art.Title)
				migrated++
			}

			fmt.Printf("\n%d migrated, %d skipped (already namespaced or no kind)\n", migrated, skipped)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print renames without applying")
	return cmd
}

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
