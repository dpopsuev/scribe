//nolint:goconst // CLI flag wiring; each command is distinct despite similar structure
package cmds

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func LensCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lens",
		Short: "Manage context lenses (stored graph projections)",
	}
	cmd.AddCommand(lensCreateCmd(), lensListCmd(), lensApplyCmd())
	return cmd
}

func lensCreateCmd() *cobra.Command {
	var title, scope, scoreBy string
	var anchor, anchorOr, exclude, include, traverse []string
	var maxDepth int

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a stored context lens",
		Long: `Create a knowledge.context artifact with lens projection rules.

Example:
  scribe lens create --title "PTP world" \
    --anchor project:ptp \
    --traverse depends_on:both:3 \
    --traverse implements:outgoing:2 \
    --exclude status:archived`,
		RunE: func(_ *cobra.Command, _ []string) error {
			rules := make([]map[string]any, 0, len(traverse))
			for _, t := range traverse {
				parts := strings.SplitN(t, ":", 3)
				rule := map[string]any{"relation": parts[0]}
				if len(parts) > 1 {
					rule["direction"] = parts[1]
				}
				if len(parts) > 2 {
					if d, err := strconv.Atoi(parts[2]); err == nil {
						rule["max_depth"] = d
					}
				}
				rules = append(rules, rule)
			}
			return RunOp("lens_create", map[string]any{
				"title":     title,
				"scope":     scope,
				"anchor":    anchor,
				"anchor_or": anchorOr,
				"traverse":  rules,
				"exclude":   exclude,
				"include":   include,
				"max_depth": maxDepth,
				"score_by":  scoreBy,
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "lens name (required)")
	cmd.Flags().StringVar(&scope, "scope", "", "project scope")
	cmd.Flags().StringSliceVar(&anchor, "anchor", nil, "anchor labels (AND, repeatable)")
	cmd.Flags().StringSliceVar(&anchorOr, "anchor-or", nil, "anchor labels (OR, repeatable)")
	cmd.Flags().StringSliceVar(&traverse, "traverse", nil, "traversal rules as relation:direction:depth (repeatable)")
	cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "labels to exclude (repeatable)")
	cmd.Flags().StringSliceVar(&include, "include", nil, "labels to force-include (repeatable)")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 0, "global traversal depth cap")
	cmd.Flags().StringVar(&scoreBy, "score-by", "edges", "scoring mode: edges, pagerank, recency, weight")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func lensListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored context lenses",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("lens_list", map[string]string{})
		},
	}
}

func lensApplyCmd() *cobra.Command {
	var contextID string
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply a stored lens and show the projected subgraph",
		RunE: func(_ *cobra.Command, _ []string) error {
			return RunOp("analyze", map[string]any{
				"mode":       "lens",
				"context_id": contextID,
			})
		},
	}
	cmd.Flags().StringVar(&contextID, "id", "", "lens artifact ID (required)")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}
