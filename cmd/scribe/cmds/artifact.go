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
			p, cleanup := MustProto()
			defer cleanup()

			if explicitID != "" {
				in.ExplicitID = explicitID
			}

			art, err := p.CreateArtifact(context.Background(), in)
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
			p, cleanup := MustProto()
			defer cleanup()
			art, err := p.GetArtifact(context.Background(), args[0])
			if err != nil {
				return err
			}
			switch format {
			case "json":
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
	var in parchment.ListInput
	var format string
	var count bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts with optional filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup := MustProto()
			defer cleanup()
			arts, err := p.ListArtifacts(context.Background(), in)
			if err != nil {
				return err
			}
			if in.Sort != "" {
				SortArts(arts, in.Sort)
			}
			if count {
				fmt.Println(len(arts))
				return nil
			}
			if in.Limit > 0 && in.Limit < len(arts) {
				arts = arts[:in.Limit]
			}
			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(arts)
			default:
				renderList(context.Background(), p, arts, in.GroupBy)
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&in.Kind, "kind", "", "filter by kind")
	cmd.Flags().StringVar(&in.Scope, "scope", "", "filter by scope")
	cmd.Flags().StringVar(&in.Status, "status", "", "filter by status")
	cmd.Flags().StringVar(&in.Parent, "parent", "", "filter by parent ID")
	cmd.Flags().StringVar(&in.Sprint, "sprint", "", "filter by sprint ID")
	cmd.Flags().StringVar(&in.IDPrefix, "id-prefix", "", "filter by ID prefix")
	cmd.Flags().StringVar(&in.ExcludeKind, "exclude-kind", "", "exclude artifacts of this kind")
	cmd.Flags().StringVar(&in.ExcludeStatus, "exclude-status", "", "exclude artifacts with this status")
	cmd.Flags().StringSliceVar(&in.Labels, "label", nil, "filter by label (AND, repeatable)")
	cmd.Flags().StringSliceVar(&in.LabelsOr, "label-or", nil, "filter by label (OR, repeatable)")
	cmd.Flags().StringSliceVar(&in.ExcludeLabels, "exclude-label", nil, "exclude by label (NOT, repeatable)")
	cmd.Flags().StringVar(&format, "format", "table", "output format (table, json)")
	cmd.Flags().StringVar(&in.Sort, "sort", "id", "sort field")
	cmd.Flags().StringVar(&in.GroupBy, "group-by", "", "group output by field (status, scope, kind, sprint, scope_label)")
	cmd.Flags().BoolVar(&count, "count", false, "print count only")
	cmd.Flags().IntVar(&in.Limit, "limit", 0, "max rows to show (0 = all)")
	return cmd
}

func renderList(ctx context.Context, p *parchment.Protocol, arts []*parchment.Artifact, groupBy string) {
	switch groupBy {
	case "scope_label":
		scopeLabels := make(map[string][]string)
		infos, err := p.ListScopeInfo(ctx)
		if err == nil {
			for _, info := range infos {
				if len(info.Labels) > 0 {
					scopeLabels[info.Scope] = info.Labels
				}
			}
		}
		fmt.Print(parchment.RenderGroupedTableByScopeLabel(arts, scopeLabels))
	case "":
		fmt.Print(parchment.RenderTable(arts))
	default:
		fmt.Print(parchment.RenderGroupedTable(arts, groupBy))
	}
}

func SetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <ID> <field> <value>",
		Short: "Set a field on an artifact",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup := MustProto()
			defer cleanup()
			results, err := p.SetField(context.Background(), []string{args[0]}, args[1], args[2])
			if err != nil {
				return err
			}
			r := results[0]
			if !r.OK {
				return fmt.Errorf("%s", r.Error) //nolint:err113 // user-facing protocol error
			}
			fmt.Printf("%s.%s = %s\n", r.ID, args[1], args[2])
			if r.Error != "" {
				fmt.Println(r.Error)
			}
			return nil
		},
	}
}

func DeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <ID>",
		Short: "Delete an artifact (must be archived unless --force)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup := MustProto()
			defer cleanup()
			return p.DeleteArtifact(context.Background(), args[0], force)
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
			p, cleanup := MustProto()
			defer cleanup()
			hasFilter := scope != "" || kind != "" || status != "" || idPrefix != "" || excludeKind != ""
			if hasFilter && len(args) == 0 {
				in := parchment.BulkMutationInput{
					Scope: scope, Kind: kind, Status: status,
					IDPrefix: idPrefix, ExcludeKind: excludeKind, DryRun: dryRun,
				}
				res, err := p.BulkArchive(context.Background(), in)
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
			results, err := p.ArchiveArtifact(context.Background(), args, false)
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
