//nolint:goconst,dupl // CLI flag wiring; each command is distinct despite similar structure
package cmds

import (
	"github.com/spf13/cobra"
)

func SchemaCmd() *cobra.Command {
	var kind, id string
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Show valid outbound relations for a kind",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("schema", map[string]string{"kind": kind, "id": id})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "kind name (e.g. knowledge.note)")
	cmd.Flags().StringVar(&id, "id", "", "artifact ID (infers kind)")
	return cmd
}

func HistoryCmd() *cobra.Command {
	var id, scope string
	var limit int
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show mutation event log for an artifact or scope",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("history", map[string]any{"id": id, "scope": scope, "limit": limit})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "artifact ID")
	cmd.Flags().StringVar(&scope, "scope", "", "scope name")
	cmd.Flags().IntVar(&limit, "limit", 20, "max events to show")
	return cmd
}

func SynthesizeCmd() *cobra.Command {
	var title, body, scope string
	var sources []string
	cmd := &cobra.Command{
		Use:   "synthesize",
		Short: "Create a knowledge.note with citations to source artifacts",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("synthesize", map[string]any{
				"title": title, "body": body, "scope": scope, "sources": sources,
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "note title (required)")
	cmd.Flags().StringVar(&body, "body", "", "note body text (required)")
	cmd.Flags().StringVar(&scope, "scope", "", "scope")
	cmd.Flags().StringSliceVar(&sources, "source", nil, "source artifact IDs to cite")
	_ = cmd.MarkFlagRequired("title")
	_ = cmd.MarkFlagRequired("body")
	return cmd
}

func AuditCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Check artifact graph for orphans and incomplete artifacts",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("lint", map[string]string{"scope": scope})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "scope to audit (empty = all)")
	return cmd
}

func HygieneCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "hygiene",
		Short: "Run hygiene checks: zombie campaigns, stale tasks, orphans",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("hygiene", map[string]string{"scope": scope})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "scope to check (empty = all)")
	return cmd
}

func DashboardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dashboard",
		Short: "Campaign health overview: goals, tasks, completion scores",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("dashboard", map[string]string{})
		},
	}
}

func RecentCmd() *cobra.Command {
	var scope, since string
	var limit int
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "Show recently changed artifacts",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("recent", map[string]any{"scope": scope, "since": since, "limit": limit})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "project scope")
	cmd.Flags().StringVar(&since, "since", "24h", "duration or RFC3339 timestamp")
	cmd.Flags().IntVar(&limit, "limit", 20, "max results")
	return cmd
}

func BriefCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "brief",
		Short: "Project briefing: campaigns, goals, tasks, recent changes",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("brief", map[string]string{"scope": scope})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "project scope (required)")
	_ = cmd.MarkFlagRequired("scope")
	return cmd
}

func BulkDeleteCmd() *cobra.Command {
	var kind, scope, status, query string
	var dryRun, force bool
	cmd := &cobra.Command{
		Use:   "bulk-delete",
		Short: "Delete artifacts matching query filters",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("delete", map[string]any{
				"kind": kind, "scope": scope, "status": status,
				"query": query, "dry_run": dryRun, "force": force,
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind")
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&query, "query", "", "FTS query")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without deleting")
	cmd.Flags().BoolVar(&force, "force", false, "bypass incoming-edge check")
	return cmd
}
