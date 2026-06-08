package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

func CreateCmd() *cobra.Command {
	var in parchment.CreateInput
	var kind, explicitID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			if explicitID != "" {
				in.ExplicitID = explicitID
			}
			if kind != "" {
				in.Labels = append([]string{parchment.LabelPrefixKind + kind}, in.Labels...)
			}
			art, err := svc.Proto.CreateArtifact(context.Background(), in)
			if err != nil {
				return err
			}
			fmt.Println(art.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "task", "artifact kind")
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
				var bulkLabels, bulkExclude []string
				if kind != "" {
					bulkLabels = append(bulkLabels, parchment.LabelPrefixKind+kind)
				}
				if status != "" {
					bulkLabels = append(bulkLabels, parchment.LabelPrefixStatus+status)
				}
				if excludeKind != "" {
					bulkExclude = append(bulkExclude, parchment.LabelPrefixKind+excludeKind)
				}
				in := parchment.BulkMutationInput{
					Scope: scope, IDPrefix: idPrefix,
					Labels: bulkLabels, ExcludeLabels: bulkExclude, DryRun: dryRun,
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
		Long:  "Relations: parent_of, depends_on, justifies, implements, documents",
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
			in := parchment.OrphanInput{Scope: scope}
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
		Use:   "scope-keys [set SCOPE KEY | set-labels SCOPE LABEL,...]",
		Short: "List or manage scope key mappings and labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			if len(args) == 0 {
				infos, err := svc.Proto.ListScopeInfo(context.Background())
				if err != nil {
					return err
				}
				if len(infos) == 0 {
					fmt.Println("no scope keys registered")
					return nil
				}
				for _, info := range infos {
					labels := ""
					if len(info.Labels) > 0 {
						labels = " [" + strings.Join(info.Labels, ",") + "]"
					}
					fmt.Printf("%s → %s%s\n", info.Scope, info.Key, labels)
				}
				return nil
			}
			if len(args) == 3 && args[0] == "set" {
				return svc.Proto.SetScopeKey(context.Background(), args[1], args[2])
			}
			if len(args) == 3 && args[0] == "set-labels" {
				labels := strings.Split(args[2], ",")
				for i := range labels {
					labels[i] = strings.TrimSpace(labels[i])
				}
				return svc.Proto.SetScopeLabels(context.Background(), args[1], labels)
			}
			return fmt.Errorf("usage: scope-keys [set SCOPE KEY | set-labels SCOPE LABEL,...]") //nolint:err113 // user-facing hint
		},
	}
}

// RegisterKnowledge adds search, section, and vocab commands to root.
func RegisterKnowledge(root *cobra.Command) {
	root.AddCommand(SearchCmd(), SectionCmd(), VocabCmd())
}

func SearchCmd() *cobra.Command {
	var scope, kind, status, format string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search artifacts by substring across title, goal, sections, and extra",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			var searchLabels []string
			if kind != "" {
				searchLabels = append(searchLabels, parchment.LabelPrefixKind+kind)
			}
			if status != "" {
				searchLabels = append(searchLabels, parchment.LabelPrefixStatus+status)
			}
			li := parchment.ListInput{Labels: searchLabels, Scope: scope}
			matched, err := svc.Proto.SearchArtifacts(context.Background(), args[0], li)
			if err != nil {
				return err
			}
			if len(matched) == 0 {
				fmt.Printf("no artifacts matching %q\n", args[0])
				return nil
			}
			switch format {
			case formatJSON:
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(matched)
			default:
				fmt.Print(parchment.RenderTable(matched))
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind")
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&format, "format", "table", "output format (table, json)")
	return cmd
}

func SectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "section",
		Short: "Manage named text sections on an artifact",
	}
	var file string
	addCmd := &cobra.Command{
		Use:   "add <ID> <name> [text]",
		Short: "Add or replace a named section",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			id, name := args[0], args[1]
			var body string
			switch {
			case file == "-":
				data, err := io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read stdin: %w", err)
				}
				body = string(data)
			case file != "":
				data, err := os.ReadFile(file) //nolint:gosec // operator-supplied path
				if err != nil {
					return fmt.Errorf("read %s: %w", file, err)
				}
				body = string(data)
			case len(args) == 3:
				body = args[2]
			default:
				return fmt.Errorf("provide text as third argument, or use --file / --file=-") //nolint:err113 // user-facing hint
			}
			replaced, err := svc.Proto.AttachSection(context.Background(), id, name, body)
			if err != nil {
				return err
			}
			action := "added"
			if replaced {
				action = "replaced"
			}
			fmt.Printf("%s: section %q %s (%d bytes)\n", id, name, action, len(body))
			return nil
		},
	}
	addCmd.Flags().StringVar(&file, "file", "", "read section text from file (use - for stdin)")

	showSectionCmd := &cobra.Command{
		Use:   "show <ID> <name>",
		Short: "Print the text of a named section",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			t, err := svc.Proto.GetSection(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Print(t)
			return nil
		},
	}

	listSectionCmd := &cobra.Command{
		Use:   "list <ID>",
		Short: "List all section names on an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			art, err := svc.Proto.GetArtifact(context.Background(), args[0])
			if err != nil {
				return err
			}
			if len(art.Sections) == 0 {
				fmt.Println("no sections")
				return nil
			}
			for _, sec := range art.Sections {
				fmt.Printf("%-30s %d bytes\n", sec.Name, len(sec.Text))
			}
			return nil
		},
	}

	rmCmd := &cobra.Command{
		Use:   "rm <ID> <name>",
		Short: "Remove a named section",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			found, err := svc.Proto.DetachSection(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("section %q not found on %s", args[1], args[0]) //nolint:err113 // user-facing hint
			}
			fmt.Printf("%s: section %q removed\n", args[0], args[1])
			return nil
		},
	}

	cmd.AddCommand(addCmd, showSectionCmd, listSectionCmd, rmCmd)
	return cmd
}

func VocabCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vocab",
		Short: "Manage the enforced kind vocabulary",
	}

	listVocabCmd := &cobra.Command{
		Use:   "list",
		Short: "Show registered artifact kinds",
		RunE: func(_ *cobra.Command, _ []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			for _, k := range svc.Proto.VocabList() {
				fmt.Println(k)
			}
			return nil
		},
	}

	addVocabCmd := &cobra.Command{
		Use:   "add <kind>",
		Short: "Register a new artifact kind",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			if err := svc.Proto.VocabAdd(args[0]); err != nil {
				return err
			}
			fmt.Printf("registered kind %q\n", args[0])
			return nil
		},
	}

	removeVocabCmd := &cobra.Command{
		Use:   "remove <kind>",
		Short: "Remove an artifact kind (only if unused)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			if err := svc.Proto.VocabRemove(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Printf("removed kind %q\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(listVocabCmd, addVocabCmd, removeVocabCmd)
	return cmd
}

var artifactIDRegexp = regexp.MustCompile(`^([A-Z]+)-\d+-(\d+)$`)

func ReseedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reseed",
		Short: "Scan all artifacts and fix sequence counters",
		RunE: func(cmd *cobra.Command, args []string) error {
			s := MustStore()
			defer func() { _ = s.Close() }()
			ctx := context.Background()
			arts, err := s.List(ctx, parchment.Filter{})
			if err != nil {
				return err
			}
			maxSequenceByPrefix := make(map[string]uint64)
			for _, a := range arts {
				m := artifactIDRegexp.FindStringSubmatch(a.ID)
				if m == nil {
					continue
				}
				prefix := m[1]
				seq, _ := strconv.ParseUint(m[2], 10, 64)
				if seq > maxSequenceByPrefix[prefix] {
					maxSequenceByPrefix[prefix] = seq
				}
			}
			for prefix, maxSequence := range maxSequenceByPrefix {
				next := maxSequence + 1
				if err := s.SeedSequence(ctx, prefix, next, false); err != nil {
					return fmt.Errorf("seed %s: %w", prefix, err)
				}
				fmt.Printf("%s -> next seq = %d\n", prefix, next)
			}
			return nil
		},
	}
}

func SeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-seq <PREFIX> <next-seq>",
		Short: "Force-set the ID sequence counter for a prefix (repair tool)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s := MustStore()
			defer func() { _ = s.Close() }()
			prefix := strings.ToUpper(args[0])
			val, err := strconv.ParseUint(args[1], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid sequence: %w", err)
			}
			if err := s.SeedSequence(context.Background(), prefix, val, true); err != nil {
				return err
			}
			fmt.Printf("%s -> next seq = %d\n", prefix, val)
			return nil
		},
	}
}

func SeedDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed <dir>",
		Short: "Seed templates and config from a directory (idempotent)",
		Long:  "Reads templates from <dir>/templates/*.md and config from <dir>/config/*.yaml. Skips artifacts that already exist.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			result, err := svc.Proto.Seed(context.Background(), args[0])
			if err != nil {
				return err
			}
			for _, id := range result.Created {
				fmt.Printf("created %s\n", id)
			}
			for _, id := range result.Skipped {
				fmt.Printf("skipped %s (exists)\n", id)
			}
			fmt.Fprintf(os.Stderr, "seed: %d created, %d skipped\n", len(result.Created), len(result.Skipped))
			return nil
		},
	}
}

func ExportCmd() *cobra.Command {
	var scope, output string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export artifacts as JSON-lines",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			var w io.Writer = os.Stdout
			if output != "" && output != "-" {
				f, err := os.Create(output) //nolint:gosec // operator-supplied output path
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				w = f
			}
			n, err := svc.Proto.Export(context.Background(), w, scope)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "exported %d artifacts\n", n)
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope (empty = all)")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "output file (- = stdout)")
	return cmd
}

func ImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [file]",
		Short: "Import artifacts from JSON-lines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			n, err := svc.Proto.Import(context.Background(), f)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "imported %d artifacts\n", n)
			return nil
		},
	}
}

func CapsuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capsule",
		Short: "Portable instance encapsulation: export, import, inspect",
	}

	var output string
	exportCapsule := &cobra.Command{
		Use:   "export",
		Short: "Export entire Scribe instance to a .capsule file",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			f, err := os.Create(output) //nolint:gosec // operator-supplied output path
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			m, err := svc.Proto.CapsuleExport(context.Background(), f, Version)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "capsule exported: %d artifacts, %d edges → %s\n",
				m.ArtifactCount, m.EdgeCount, output)
			return nil
		},
	}
	exportCapsule.Flags().StringVarP(&output, "output", "o", "scribe.capsule", "output file path")

	importCapsule := &cobra.Command{
		Use:   "import <file>",
		Short: "Import a .capsule file (replaces current state)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			m, err := svc.Proto.CapsuleImport(context.Background(), f)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "capsule imported: %d artifacts, %d edges (version: %s)\n",
				m.ArtifactCount, m.EdgeCount, m.Version)
			return nil
		},
	}

	inspectCapsule := &cobra.Command{
		Use:   "inspect <file>",
		Short: "Show capsule manifest without importing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			m, err := parchment.CapsuleInspect(f)
			if err != nil {
				return err
			}
			fmt.Printf("Version:    %s\n", m.Version)
			fmt.Printf("Created:    %s\n", m.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("Artifacts:  %d\n", m.ArtifactCount)
			fmt.Printf("Edges:      %d\n", m.EdgeCount)
			return nil
		},
	}

	cmd.AddCommand(exportCapsule, importCapsule, inspectCapsule)
	return cmd
}

// ExportMdCmd exports artifacts from a scope to .md files with YAML frontmatter.
func ExportMdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export-md <scope> <output-dir>",
		Short: "Export artifacts to .md files (YAML frontmatter + section headings)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			n, err := svc.ExportScope(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			fmt.Printf("exported %d artifact(s) to %s\n", n, args[1])
			return nil
		},
	}
}

func GoalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "goal",
		Short: "Manage the current goal (short-term north star)",
	}
	var in service.SetGoalInput
	setGoalCmd := &cobra.Command{
		Use:   "set <title>",
		Short: "Set the current goal (retires any previous, creates a root delivery artifact)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			in.Title = args[0]
			res, err := svc.SetGoal(context.Background(), in)
			if err != nil {
				return err
			}
			for _, a := range res.Archived {
				fmt.Printf("archived %s: %s\n", a.ID, a.Title)
			}
			fmt.Printf("%s [current] %s\n", res.Goal.ID, res.Goal.Title)
			fmt.Printf("%s [draft] %s (justifies %s)\n", res.Root.ID, res.Root.Title, res.Goal.ID)
			return nil
		},
	}
	setGoalCmd.Flags().StringVar(&in.Scope, "scope", "", "scope for the goal")
	setGoalCmd.Flags().StringVar(&in.Kind, "kind", "goal", "kind for the root delivery artifact")

	showGoalCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the current goal",
		RunE: func(_ *cobra.Command, _ []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			m, _ := svc.Brief(context.Background())
			if len(m.Goals) == 0 {
				fmt.Println("no current goal set")
				return nil
			}
			for _, a := range m.Goals {
				prefix := ""
				if a.Scope != "" {
					prefix = "[" + a.Scope + "] "
				}
				fmt.Printf("%s %s%s\n", a.ID, prefix, a.Title)
			}
			return nil
		},
	}

	cmd.AddCommand(setGoalCmd, showGoalCmd)
	return cmd
}

const syncDaemonPollInterval = 30 * time.Second

// SyncCmd syncs a directory of .md files into Parchment once.
func SyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync <path>",
		Short: "Import .md files from a directory into Parchment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			n, err := svc.SyncDir(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("synced %d artifact(s)\n", n)
			return nil
		},
	}
}

// DaemonCmd watches a directory and re-syncs on any .md change.
func DaemonCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "daemon <path>",
		Short: "Watch a directory and sync .md changes to Parchment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()

			path := args[0]
			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer cancel()

			slog.InfoContext(ctx, "daemon starting", slog.String(logKeyPath, path))
			if _, err := svc.SyncDir(ctx, path); err != nil {
				slog.WarnContext(ctx, "initial sync failed", slog.Any(logKeyError, err))
			}

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				slog.WarnContext(ctx, "fsnotify unavailable, polling", slog.Any(logKeyError, err))
				return syncDaemonPoll(ctx, svc, path)
			}
			defer func() { _ = watcher.Close() }()

			if err := watcher.Add(path); err != nil {
				slog.WarnContext(ctx, "cannot watch path, polling", slog.Any(logKeyError, err))
				return syncDaemonPoll(ctx, svc, path)
			}
			slog.InfoContext(ctx, "watching", slog.String(logKeyPath, path))

			for {
				select {
				case <-ctx.Done():
					return nil
				case event, ok := <-watcher.Events:
					if !ok {
						return nil
					}
					if strings.ToLower(filepath.Ext(event.Name)) != ".md" {
						continue
					}
					if _, err := svc.SyncDir(ctx, path); err != nil {
						slog.WarnContext(ctx, "sync failed", slog.Any(logKeyError, err))
					}
				case watchErr, ok := <-watcher.Errors:
					if !ok {
						return nil
					}
					slog.WarnContext(ctx, "watcher error", slog.Any(logKeyError, watchErr))
				}
			}
		},
	}
}

func syncDaemonPoll(ctx context.Context, svc *service.Service, path string) error {
	ticker := time.NewTicker(syncDaemonPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := svc.SyncDir(ctx, path); err != nil {
				slog.WarnContext(ctx, "poll sync failed", slog.Any(logKeyError, err))
			}
		}
	}
}
