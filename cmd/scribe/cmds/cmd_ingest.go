package cmds

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// IngestCmd materializes a git repository into Scribe's artifact graph.
func IngestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ingest <repo-path>",
		Short: "Ingest a git repository — specs, docs, and commit→ticket links",
		Long: `Walks a git repository, parses markdown files from configured source
directories (specs/, docs/ by default), extracts ticket references from
git commits, and builds a cross-layer artifact graph in Scribe.

Configure via .scribe.yaml in the repo root, or use defaults.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			result, err := svc.RepoIngest(context.Background(), args[0])
			if err != nil {
				return err
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			fmt.Println(string(data))
			return nil
		},
	}
}
