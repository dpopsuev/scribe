package main

import (
	"fmt"
	"os"

	"github.com/dpopsuev/scribe/cmd/scribe/cmds"
	"github.com/spf13/cobra"
)

var Version = "dev"

func main() {
	cmds.Version = Version
	root := &cobra.Command{
		Use:   "scribe",
		Short: "Lean artifact store with native DAG support",
	}
	root.PersistentFlags().StringVar(&cmds.DBPath, "db", "", "database path (overrides config file and $SCRIBE_DB)")
	root.PersistentFlags().StringVar(&cmds.ConfigPath, "config", "", "config file path (default: ./scribe.yaml or ~/.scribe/scribe.yaml)")

	root.AddCommand(
		&cobra.Command{
			Use:   "version",
			Short: "Print the version",
			Run:   func(cmd *cobra.Command, args []string) { fmt.Printf("scribe %s\n", cmds.Version) },
		},
		cmds.CreateCmd(),
		cmds.ShowCmd(),
		cmds.ListCmd(),
		cmds.SetCmd(),
		cmds.DeleteCmd(),
		cmds.TreeCmd(),
		cmds.BriefingCmd(),
		cmds.SectionCmd(),
		cmds.SearchCmd(),

		cmds.VacuumCmd(),

		cmds.LinkCmd(),
		cmds.UnlinkCmd(),
		cmds.OverlapsCmd(),
		cmds.OrphansCmd(),
		cmds.ScopeKeysCmd(),
		cmds.ServeCmd(),
		cmds.ToolsCmd(),
		cmds.UICmd(),
		cmds.VocabCmd(),
		cmds.LintCmd(),
		cmds.CheckCmd(),
		cmds.ConfigCmd(),
		cmds.ExportCmd(),
		cmds.ImportCmd(),
		cmds.ExportMdCmd(),
		cmds.SeedDirCmd(),
		cmds.CapsuleCmd(),
		cmds.SyncCmd(),
		cmds.DaemonCmd(),
		cmds.ApplyCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
