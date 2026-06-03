package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
)

func CreateCmd() *cobra.Command {
	var in parchment.CreateInput
	var explicitID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			if explicitID != "" {
				in.ExplicitID = explicitID
			}
			art, err := svc.Proto.CreateArtifact(context.Background(), in)
			if err != nil {
				return err
			}
			fmt.Println(art.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&in.Kind, "kind", "task", "artifact kind")
	cmd.Flags().StringVar(&in.Title, "title", "", "artifact title")
	cmd.Flags().StringVar(&in.Scope, "scope", "", "owning repository")
	cmd.Flags().StringVar(&in.Goal, "goal", "", "goal statement")
	cmd.Flags().StringVar(&in.Parent, "parent", "", "parent artifact ID")
	cmd.Flags().StringVar(&in.Prefix, "prefix", "", "ID prefix override")
	cmd.Flags().StringVar(&explicitID, "id", "", "explicit ID (skips auto-generation)")
	cmd.Flags().StringSliceVar(&in.Labels, "label", nil, "labels (repeatable)")
	cmd.Flags().StringSliceVar(&in.DependsOn, "depends-on", nil, "dependency IDs (repeatable)")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func ShowCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "show <ID>",
		Short: "Show a single artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			art, err := svc.Proto.GetArtifact(context.Background(), args[0])
			if err != nil {
				return err
			}
			switch format {
			case formatJSON:
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(art)
			default:
				fmt.Print(parchment.RenderMarkdown(art))
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "md", "output format (md, json)")
	return cmd
}

func ListCmd() *cobra.Command {
	var kind, scope, status, parent, sprint, idPrefix, excludeKind, excludeStatus string
	var labels, labelsOr, excludeLabels []string
	var format, sortField, groupBy, query string
	var count bool
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts with optional filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunOp("list", map[string]any{
				"kind": kind, "scope": scope, "status": status,
				"parent": parent, "sprint": sprint, "id_prefix": idPrefix,
				"exclude_kind": excludeKind, "exclude_status": excludeStatus,
				"labels": labels, "labels_or": labelsOr, "exclude_labels": excludeLabels,
				"query": query, "sort": sortField, "group_by": groupBy,
				"format": format, "count": count, "limit": limit,
			})
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind")
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&parent, "parent", "", "filter by parent ID")
	cmd.Flags().StringVar(&sprint, "sprint", "", "filter by sprint ID")
	cmd.Flags().StringVar(&idPrefix, "id-prefix", "", "filter by ID prefix")
	cmd.Flags().StringVar(&excludeKind, "exclude-kind", "", "exclude artifacts of this kind")
	cmd.Flags().StringVar(&excludeStatus, "exclude-status", "", "exclude artifacts with this status")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "filter by label (AND, repeatable)")
	cmd.Flags().StringSliceVar(&labelsOr, "label-or", nil, "filter by label (OR, repeatable)")
	cmd.Flags().StringSliceVar(&excludeLabels, "exclude-label", nil, "exclude by label (NOT, repeatable)")
	cmd.Flags().StringVar(&format, "format", "table", "output format (table, json)")
	cmd.Flags().StringVar(&sortField, "sort", "id", "sort field")
	cmd.Flags().StringVar(&groupBy, "group-by", "", "group output by field")
	cmd.Flags().StringVar(&query, "query", "", "full-text search query")
	cmd.Flags().BoolVar(&count, "count", false, "print count only")
	cmd.Flags().IntVar(&limit, "limit", 0, "max rows to show (0 = all)")
	return cmd
}

func SetCmd() *cobra.Command {
	var bypassGuards bool
	cmd := &cobra.Command{
		Use:   "set <ID> <field> <value>",
		Short: "Set a field on an artifact",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			params := map[string]any{"id": args[0], "field": args[1], "value": args[2]}
			if bypassGuards {
				params["bypass_guards"] = true
			}
			return RunOp("set", params)
		},
	}
	cmd.Flags().BoolVar(&bypassGuards, "bypass-guards", false, "skip lifecycle guards (use for force status transitions)")
	return cmd
}

func DeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <ID>",
		Short: "Delete an artifact (must be archived unless --force)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			return svc.Proto.DeleteArtifact(context.Background(), args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "bypass archive-required guard")
	return cmd
}

func ArchiveCmd() *cobra.Command {
	var cascade, dryRun bool
	var scope, kind, status, idPrefix, excludeKind string
	cmd := &cobra.Command{
		Use:   "archive [ID...]",
		Short: "Archive artifacts (marks read-only; use --cascade for subtrees). With filter flags and no IDs, bulk-archives matching artifacts.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			hasFilter := scope != "" || kind != "" || status != "" || idPrefix != "" || excludeKind != ""
			if hasFilter && len(args) == 0 {
				in := parchment.BulkMutationInput{
					Scope: scope, Kind: kind, Status: status,
					IDPrefix: idPrefix, ExcludeKind: excludeKind, DryRun: dryRun,
				}
				res, err := svc.Proto.BulkArchive(context.Background(), in)
				if err != nil {
					return err
				}
				if dryRun {
					fmt.Printf("dry run: would archive %d artifacts\n", res.Count)
					for _, id := range res.AffectedIDs {
						fmt.Printf("  %s\n", id)
					}
				} else {
					fmt.Printf("archived %d artifacts\n", res.Count)
				}
				return nil
			}
			if len(args) == 0 {
				return fmt.Errorf("provide IDs or filter flags (--scope, --kind, --status, --id-prefix, --exclude-kind)") //nolint:err113 // user-facing hint
			}
			results, err := svc.Proto.ArchiveArtifact(context.Background(), args, false)
			if err != nil {
				return err
			}
			for _, r := range results {
				if r.OK {
					fmt.Printf("%s -> archived\n", r.ID)
				} else {
					fmt.Fprintf(os.Stderr, "%s -> error: %s\n", r.ID, r.Error)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&cascade, "cascade", false, "recursively archive child subtrees")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview bulk archive without applying")
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope (bulk mode)")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind (bulk mode)")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (bulk mode)")
	cmd.Flags().StringVar(&idPrefix, "id-prefix", "", "filter by ID prefix (bulk mode)")
	cmd.Flags().StringVar(&excludeKind, "exclude-kind", "", "exclude kind (bulk mode)")
	return cmd
}
