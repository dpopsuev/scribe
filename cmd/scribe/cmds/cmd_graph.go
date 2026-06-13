package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
)

func TreeCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "tree <ID>",
		Short: "Show the parent-child tree rooted at an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			tree, err := svc.Proto.ArtifactTree(context.Background(), parchment.TreeInput{ID: args[0]})
			if err != nil {
				return err
			}
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(tree)
			}
			var b strings.Builder
			printTree(tree, "", true, &b)
			fmt.Print(b.String())
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func printTree(node *parchment.TreeNode, prefix string, last bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	nodeStatus := labelVal(node.Labels, parchment.LabelPrefixStatus)
	fmt.Fprintf(b, "%s%s%s [%s] %s\n", prefix, connector, node.ID, nodeStatus, node.Title)
	cp := prefix
	if prefix != "" {
		if last {
			cp += "    "
		} else {
			cp += "│   "
		}
	}
	for i, ch := range node.Children {
		printTree(ch, cp, i == len(node.Children)-1, b)
	}
}

func BriefingCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "briefing <ID>",
		Short: "Recursive edge-aware traversal showing the full context chain from any artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			tree, err := svc.Proto.ArtifactTree(context.Background(), parchment.TreeInput{
				ID:        args[0],
				Relation:  "*",
				Direction: "both",
			})
			if err != nil {
				return err
			}
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(tree)
			}
			var b strings.Builder
			printBriefing(tree, "", true, &b)
			fmt.Print(b.String())
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func printBriefing(node *parchment.TreeNode, prefix string, last bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	edgeLabel := ""
	if node.Edge != "" {
		arrow := " -> "
		if node.Direction == "incoming" {
			arrow = " <- "
		}
		edgeLabel = node.Edge + arrow
	}
	nodeKind := labelVal(node.Labels, parchment.LabelPrefixKind)
	nodeStatus := labelVal(node.Labels, parchment.LabelPrefixStatus)
	kindStatus := nodeStatus
	if nodeKind != "" {
		kindStatus = nodeKind + "|" + nodeStatus
	}
	fmt.Fprintf(b, "%s%s%s%s [%s] %s\n", prefix, connector, edgeLabel, node.ID, kindStatus, node.Title)
	cp := prefix
	if prefix != "" {
		if last {
			cp += "    "
		} else {
			cp += "│   "
		}
	}
	for i, ch := range node.Children {
		printBriefing(ch, cp, i == len(node.Children)-1, b)
	}
}

func LinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <ID> <relation> <target> [target...]",
		Short: "Add a directed relationship between artifacts",
		Long:  "Relations: parent_of, depends_on, follows, justifies, implements, documents, blocks, duplicates, relates_to, clones, mentions, tested_by, supersedes, cites, elaborates, traces_to, calls, explains, causes, resolves",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			results, err := svc.Proto.LinkArtifacts(context.Background(), args[0], args[1], args[2:], 0)
			if err != nil {
				return err
			}
			for _, r := range results {
				if r.OK {
					fmt.Printf("%s -[%s]-> %s\n", args[0], args[1], r.ID)
				} else {
					fmt.Fprintf(os.Stderr, "%s -> error: %s\n", r.ID, r.Error)
				}
			}
			return nil
		},
	}
}

func UnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <ID> <relation> <target> [target...]",
		Short: "Remove a directed relationship between artifacts",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			results, err := svc.Proto.UnlinkArtifacts(context.Background(), args[0], args[1], args[2:])
			if err != nil {
				return err
			}
			for _, r := range results {
				if r.OK {
					fmt.Printf("unlinked %s -[%s]-> %s\n", args[0], args[1], r.ID)
				} else {
					fmt.Fprintf(os.Stderr, "%s -> error: %s\n", r.ID, r.Error)
				}
			}
			return nil
		},
	}
}

func OverlapsCmd() *cobra.Command {
	var project, kind, status, format string
	cmd := &cobra.Command{
		Use:   "overlaps",
		Short: "Detect artifacts sharing component labels (scope conflicts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			var overlapLabels []string
			if kind != "" {
				overlapLabels = append(overlapLabels, parchment.LabelPrefixKind+kind)
			}
			if status != "" {
				overlapLabels = append(overlapLabels, parchment.LabelPrefixStatus+status)
			}
			in := parchment.OverlapInput{Labels: overlapLabels, Project: project}
			report, err := svc.Proto.DetectOverlaps(context.Background(), in)
			if err != nil {
				return err
			}
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			if len(report.Overlaps) == 0 {
				fmt.Printf("No overlaps found across %d artifacts.\n", report.TotalScanned)
				return nil
			}
			for _, o := range report.Overlaps {
				fmt.Printf("%s\n", o.Label)
				for _, a := range o.Artifacts {
					fmt.Printf("  %-16s %s\n", a.ID, a.Title)
				}
				fmt.Println()
			}
			fmt.Printf("%d overlap(s) across %d artifacts\n", report.TotalOverlaps, report.TotalScanned)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "filter by project prefix")
	cmd.Flags().StringVar(&kind, "kind", "task", "artifact kind to scan")
	cmd.Flags().StringVar(&status, "status", "active", "artifact status to scan")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func OrphansCmd() *cobra.Command {
	var scope, format string
	cmd := &cobra.Command{
		Use:   "orphans",
		Short: "Detect tasks without specs/bugs, and specs/bugs without tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			var orphanLabels []string
			if scope != "" {
				orphanLabels = []string{parchment.LabelPrefixScope + scope}
			}
			in := parchment.OrphanInput{Labels: orphanLabels}
			report, err := svc.Proto.DetectOrphans(context.Background(), in)
			if err != nil {
				return err
			}
			if format == formatJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			if len(report.Orphans) == 0 {
				fmt.Printf("No orphans found across %d artifacts.\n", report.TotalScanned)
				return nil
			}
			for _, o := range report.Orphans {
				oKind := labelVal(o.Labels, parchment.LabelPrefixKind)
				oStatus := labelVal(o.Labels, parchment.LabelPrefixStatus)
				fmt.Printf("%-16s %-5s [%s] %s\n  → %s\n\n", o.ID, oKind, oStatus, o.Title, o.Reason)
			}
			fmt.Printf("%d orphan(s) across %d artifacts\n", report.TotalOrphans, report.TotalScanned)
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func ScopeKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scope-keys [set-labels SCOPE LABEL,...]",
		Short: "List scope info or set scope labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			if len(args) == 0 {
				infos, err := svc.Proto.ListScopeInfo(context.Background())
				if err != nil {
					return err
				}
				if len(infos) == 0 {
					fmt.Println("no scope info registered")
					return nil
				}
				for _, info := range infos {
					labels := ""
					if len(info.Labels) > 0 {
						labels = " [" + strings.Join(info.Labels, ",") + "]"
					}
					fmt.Printf("%s%s\n", info.Scope, labels)
				}
				return nil
			}
			if len(args) == 3 && args[0] == "set-labels" {
				labels := strings.Split(args[2], ",")
				for i := range labels {
					labels[i] = strings.TrimSpace(labels[i])
				}
				return svc.Proto.SetScopeLabels(context.Background(), args[1], labels)
			}
			return fmt.Errorf("usage: scope-keys [set-labels SCOPE LABEL,...]") //nolint:err113 // user-facing hint
		},
	}
}
