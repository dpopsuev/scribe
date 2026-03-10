package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/directive"
	"github.com/dpopsuev/scribe/mcp"
	"github.com/dpopsuev/scribe/mcpclient"
	"github.com/dpopsuev/scribe/web"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/render"
	"github.com/dpopsuev/scribe/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var (
	dbPath     string
	configPath string
)

func main() {
	root := &cobra.Command{
		Use:   "scribe",
		Short: "Lean artifact store with native DAG support",
	}
	root.PersistentFlags().StringVar(&dbPath, "db", "", "database path (overrides config file and $SCRIBE_DB)")
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path (default: ./scribe.yaml or ~/.scribe/scribe.yaml)")

	root.AddCommand(
		createCmd(),
		showCmd(),
		listCmd(),
		setCmd(),
		deleteCmd(),
		treeCmd(),
		sectionCmd(),
		searchCmd(),
		goalCmd(),
		archiveCmd(),
		vacuumCmd(),
		dfCmd(),
		motdCmd(),
		drainCmd(),
		inventoryCmd(),
		linkCmd(),
		unlinkCmd(),
		contextCmd(),
		overlapsCmd(),
		orphansCmd(),
		serveCmd(),
		reseedCmd(),
		seedCmd(),
		toolsCmd(),
		uiCmd(),
		vocabCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func mustConfig() *config.Config {
	cfg, err := config.Resolve(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		os.Exit(1)
	}
	if dbPath != "" {
		cfg.DB = dbPath
	}
	return cfg
}

func mustProto() (*protocol.Protocol, func()) {
	cfg := mustConfig()
	s, err := store.OpenSQLite(cfg.DB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	var vocab []string
	if cfg.Vocabulary != nil {
		vocab = cfg.Vocabulary.Kinds
	}
	return protocol.New(s, cfg.Schema, nil, vocab), func() { s.Close() }
}

func mustStore() *store.SQLiteStore {
	cfg := mustConfig()
	s, err := store.OpenSQLite(cfg.DB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	return s
}

// --- create ---

func createCmd() *cobra.Command {
	var in protocol.CreateInput
	var explicitID string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()

			if explicitID != "" {
				s := p.Store()
				art := &model.Artifact{
					ID: explicitID, Kind: in.Kind, Scope: in.Scope,
					Status: "draft", Parent: in.Parent,
					Title: in.Title, Goal: in.Goal,
					DependsOn: in.DependsOn, Labels: in.Labels,
				}
				if err := s.Put(context.Background(), art); err != nil {
					return err
				}
				fmt.Println(explicitID)
				return nil
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

// --- show ---

func showCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "show <ID>",
		Short: "Show a single artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
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
				fmt.Print(render.Markdown(art))
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "md", "output format (md, json)")
	return cmd
}

// --- list ---

func listCmd() *cobra.Command {
	var in protocol.ListInput
	var format string
	var count bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts with optional filters",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			arts, err := p.ListArtifacts(context.Background(), in)
			if err != nil {
				return err
			}
			if in.Sort != "" {
				sortArts(arts, in.Sort)
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
				if in.GroupBy != "" {
					fmt.Print(render.GroupedTable(arts, in.GroupBy))
				} else {
					fmt.Print(render.Table(arts))
				}
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
	cmd.Flags().StringVar(&format, "format", "table", "output format (table, json)")
	cmd.Flags().StringVar(&in.Sort, "sort", "id", "sort field")
	cmd.Flags().StringVar(&in.GroupBy, "group-by", "", "group output by field")
	cmd.Flags().BoolVar(&count, "count", false, "print count only")
	cmd.Flags().IntVar(&in.Limit, "limit", 0, "max rows to show (0 = all)")
	return cmd
}

// --- set ---

func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <ID> <field> <value>",
		Short: "Set a field on an artifact",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			results, err := p.SetField(context.Background(), []string{args[0]}, args[1], args[2])
			if err != nil {
				return err
			}
			r := results[0]
			if !r.OK {
				return fmt.Errorf("%s", r.Error)
			}
			fmt.Printf("%s.%s = %s\n", r.ID, args[1], args[2])
			if r.Error != "" {
				fmt.Println(r.Error)
			}
			return nil
		},
	}
}

// --- delete ---

func deleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <ID>",
		Short: "Delete an artifact (must be archived unless --force)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			return p.DeleteArtifact(context.Background(), args[0], force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "bypass archive-required guard")
	return cmd
}

// --- section ---

func sectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "section",
		Short: "Manage named text sections on an artifact",
	}
	var file string
	addCmd := &cobra.Command{
		Use:   "add <ID> <name> [text]",
		Short: "Add or replace a named section",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
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
				data, err := os.ReadFile(file)
				if err != nil {
					return fmt.Errorf("read %s: %w", file, err)
				}
				body = string(data)
			case len(args) == 3:
				body = args[2]
			default:
				return fmt.Errorf("provide text as third argument, or use --file / --file=-")
			}
			replaced, err := p.AttachSection(context.Background(), id, name, body)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			t, err := p.GetSection(context.Background(), args[0], args[1])
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
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			art, err := p.GetArtifact(context.Background(), args[0])
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
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			ctx := context.Background()
			art, err := p.GetArtifact(ctx, args[0])
			if err != nil {
				return err
			}
			var kept []model.Section
			found := false
			for _, sec := range art.Sections {
				if sec.Name == args[1] {
					found = true
					continue
				}
				kept = append(kept, sec)
			}
			if !found {
				return fmt.Errorf("section %q not found on %s", args[1], args[0])
			}
			art.Sections = kept
			if err := p.Store().Put(ctx, art); err != nil {
				return err
			}
			fmt.Printf("%s: section %q removed\n", args[0], args[1])
			return nil
		},
	}

	cmd.AddCommand(addCmd, showSectionCmd, listSectionCmd, rmCmd)
	return cmd
}

// --- tree ---

func treeCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "tree <ID>",
		Short: "Show the parent-child tree rooted at an artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			tree, err := p.ContractTree(context.Background(), args[0])
			if err != nil {
				return err
			}
			if format == "json" {
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

func printTree(node *protocol.TreeNode, prefix string, last bool, b *strings.Builder) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	fmt.Fprintf(b, "%s%s%s [%s] %s\n", prefix, connector, node.ID, node.Status, node.Title)
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

// --- search ---

func searchCmd() *cobra.Command {
	var scope, kind, status, format string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search artifacts by substring across title, goal, sections, and extra",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			li := protocol.ListInput{Kind: kind, Scope: scope, Status: status}
			matched, err := p.SearchArtifacts(context.Background(), args[0], li)
			if err != nil {
				return err
			}
			if len(matched) == 0 {
				fmt.Printf("no artifacts matching %q\n", args[0])
				return nil
			}
			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(matched)
			default:
				fmt.Print(render.Table(matched))
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

// --- goal ---

func goalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "goal",
		Short: "Manage the current goal (short-term north star)",
	}
	var in protocol.SetGoalInput
	setGoalCmd := &cobra.Command{
		Use:   "set <title>",
		Short: "Set the current goal (retires any previous, creates a root delivery artifact)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			in.Title = args[0]
			res, err := p.SetGoal(context.Background(), in)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			m, _ := p.Motd(context.Background())
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

// --- archive ---

func archiveCmd() *cobra.Command {
	var cascade, dryRun bool
	var scope, kind, status, idPrefix, excludeKind string
	cmd := &cobra.Command{
		Use:   "archive [ID...]",
		Short: "Archive artifacts (marks read-only; use --cascade for subtrees). With filter flags and no IDs, bulk-archives matching artifacts.",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			hasFilter := scope != "" || kind != "" || status != "" || idPrefix != "" || excludeKind != ""
			if hasFilter && len(args) == 0 {
				in := protocol.BulkMutationInput{
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
				return fmt.Errorf("provide IDs or filter flags (--scope, --kind, --status, --id-prefix, --exclude-kind)")
			}
			results, err := p.ArchiveArtifact(context.Background(), args, cascade)
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

// --- vacuum ---

func vacuumCmd() *cobra.Command {
	var days int
	var scope string
	var force bool
	cmd := &cobra.Command{
		Use:   "vacuum",
		Short: "Delete archived artifacts older than --days (default 90). Protected kinds (spec, bug) are skipped unless --force.",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			deleted, err := p.Vacuum(context.Background(), days, scope, force)
			if err != nil {
				return err
			}
			if len(deleted) == 0 {
				fmt.Println("nothing to vacuum")
				return nil
			}
			for _, id := range deleted {
				fmt.Printf("deleted %s\n", id)
			}
			fmt.Printf("%d archived artifacts vacuumed\n", len(deleted))
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 90, "minimum age in days")
	cmd.Flags().StringVar(&scope, "scope", "", "limit to artifacts in this scope")
	cmd.Flags().BoolVar(&force, "force", false, "delete protected kinds (spec, bug)")
	return cmd
}

// --- df ---

func dfCmd() *cobra.Command {
	var staleDays int
	var format string
	cmd := &cobra.Command{
		Use:   "df",
		Short: "Housekeeping dashboard: storage, staleness, scope health",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			report, err := p.Dashboard(context.Background(), staleDays)
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			fmt.Printf("DB size: %d bytes\n\n", report.DBSizeBytes)
			fmt.Println("Scopes:")
			for _, ds := range report.Scopes {
				fmt.Printf("  %-15s total=%d active=%d archived=%d sections=%d edges=%d stale=%d\n",
					ds.Scope, ds.Total, ds.Active, ds.Archived, ds.Sections, ds.Edges, ds.Stale)
			}
			if len(report.StaleArts) > 0 {
				fmt.Println("\nTop stale artifacts (by updated_at):")
				for _, a := range report.StaleArts {
					fmt.Printf("  %s [%s] %s\n", a.ID, a.Status, a.Title)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&staleDays, "stale-days", 30, "staleness threshold in days")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

// --- motd ---

func motdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "motd",
		Short: "Message of the day: due reminders, recent notes, and current goal",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			m, err := p.Motd(context.Background())
			if err != nil {
				return err
			}
			var sections []string
			if len(m.Goals) > 0 {
				var lines []string
				for _, g := range m.Goals {
					prefix := ""
					if g.Scope != "" {
						prefix = "[" + g.Scope + "] "
					}
					lines = append(lines, fmt.Sprintf("  %s %s%s", g.ID, prefix, g.Title))
				}
				sections = append(sections, "Goal:\n"+strings.Join(lines, "\n"))
			}
			now := time.Now().UTC()
			if len(m.DueReminders) > 0 {
				var lines []string
				for _, n := range m.DueReminders {
					r, _ := n.Extra["remind_at"].(string)
					lines = append(lines, fmt.Sprintf("  %s %s (due: %s)", n.ID, n.Title, r))
				}
				sections = append(sections, fmt.Sprintf("Due reminders (%d):\n%s", len(m.DueReminders), strings.Join(lines, "\n")))
			}
			if len(m.RecentNotes) > 0 {
				var lines []string
				for _, n := range m.RecentNotes {
					age := now.Sub(n.CreatedAt).Truncate(time.Minute)
					lines = append(lines, fmt.Sprintf("  %s %s (%s ago)", n.ID, n.Title, age))
				}
				sections = append(sections, fmt.Sprintf("Recent notes (%d):\n%s", len(m.RecentNotes), strings.Join(lines, "\n")))
			}
			if len(sections) == 0 {
				fmt.Println("nothing to report")
				return nil
			}
			fmt.Println(strings.Join(sections, "\n\n"))
			return nil
		},
	}
}

// --- drain ---

func drainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drain",
		Short: "Discover or clean up legacy .cursor/contracts markdown files",
	}
	discoverCmd := &cobra.Command{
		Use:   "discover <path>",
		Short: "List .md files under a directory for agent-driven migration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			entries, err := p.DrainDiscover(context.Background(), args[0])
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Println("no .md files found")
				return nil
			}
			format, _ := cmd.Flags().GetString("format")
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}
			for _, e := range entries {
				fmt.Printf("%-50s  [dir: %-15s  %d bytes]\n", e.Path, e.Dir, e.SizeB)
			}
			fmt.Printf("\n%d files discovered.\n", len(entries))
			return nil
		},
	}
	discoverCmd.Flags().String("format", "text", "output format (text, json)")

	cleanupCmd := &cobra.Command{
		Use:   "cleanup <path>",
		Short: "Delete .md files under a directory after migration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			n, err := p.DrainCleanup(context.Background(), args[0])
			if err != nil {
				return err
			}
			fmt.Printf("removed %d files\n", n)
			return nil
		},
	}
	cmd.AddCommand(discoverCmd, cleanupCmd)
	return cmd
}

// --- inventory ---

func inventoryCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "inventory",
		Short: "Show a dashboard summary of all artifacts",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			inv, err := p.Inventory(context.Background())
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(inv)
			}
			fmt.Printf("Total artifacts: %d\n\n", inv.Total)
			fmt.Println("By kind:")
			for k, v := range inv.ByKind {
				fmt.Printf("  %-15s %d\n", k, v)
			}
			fmt.Println("\nBy status:")
			for s, v := range inv.ByStatus {
				fmt.Printf("  %-15s %d\n", s, v)
			}
			if len(inv.ActiveSprints) > 0 {
				fmt.Println("\nActive sprints:")
				for _, sp := range inv.ActiveSprints {
					fmt.Printf("  %s %s\n", sp.ID, sp.Title)
				}
			}
			if len(inv.Goals) > 0 {
				fmt.Println("\nCurrent goals:")
				for _, g := range inv.Goals {
					prefix := ""
					if g.Scope != "" {
						prefix = "[" + g.Scope + "] "
					}
					fmt.Printf("  %s %s%s\n", g.ID, prefix, g.Title)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

// --- link / unlink ---

func linkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <ID> <relation> <target> [target...]",
		Short: "Add a directed relationship between artifacts",
		Long:  "Relations: parent_of, depends_on, justifies, implements, documents, satisfies",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			results, err := p.LinkArtifacts(context.Background(), args[0], args[1], args[2:])
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

func unlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <ID> <relation> <target> [target...]",
		Short: "Remove a directed relationship between artifacts",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			results, err := p.UnlinkArtifacts(context.Background(), args[0], args[1], args[2:])
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

// --- context ---

func contextCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "context <ID>",
		Short: "Query Locus for codebase context related to an artifact",
		Long: `Read an artifact's scope and labels, call Locus scan_project,
and return architecture context relevant to the artifact.

Requires Locus to be running (default: http://localhost:8081/).
Set LOCUS_URL to override the endpoint.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			art, err := p.GetArtifact(context.Background(), args[0])
			if err != nil {
				return err
			}
			scanPath := path
			if scanPath == "" {
				scanPath = art.Scope
			}
			if scanPath == "" {
				return fmt.Errorf("no --path and no scope on artifact %s", args[0])
			}

			locus := mcpclient.New(mcpclient.DefaultLocusURL())
			defer locus.Close()

			ctx := context.Background()
			scanData, err := locus.ScanProject(ctx, scanPath)
			if err != nil {
				return fmt.Errorf("locus scan_project: %w", err)
			}
			fmt.Printf("# Context for %s: %s\n\n", art.ID, art.Title)
			fmt.Printf("Scope: %s\n\n", scanPath)
			fmt.Printf("## Architecture Scan\n\n```json\n%s\n```\n", string(scanData))

			if cycles, err := locus.GetCycles(ctx, scanPath); err == nil {
				fmt.Printf("\n## Cycles\n\n```json\n%s\n```\n", string(cycles))
			}
			if surface, err := locus.GetAPISurface(ctx, scanPath); err == nil {
				fmt.Printf("\n## API Surface\n\n```json\n%s\n```\n", string(surface))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "repository path to scan (overrides artifact scope)")
	return cmd
}

// --- overlaps ---

func overlapsCmd() *cobra.Command {
	var project, kind, status, format string
	cmd := &cobra.Command{
		Use:   "overlaps",
		Short: "Detect artifacts sharing component labels (scope conflicts)",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			in := protocol.OverlapInput{Kind: kind, Status: status, Project: project}
			report, err := p.DetectOverlaps(context.Background(), in)
			if err != nil {
				return err
			}
			if format == "json" {
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

// --- orphans ---

func orphansCmd() *cobra.Command {
	var scope, status, format string
	cmd := &cobra.Command{
		Use:   "orphans",
		Short: "Detect tasks without specs/bugs, and specs/bugs without tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			in := protocol.OrphanInput{Scope: scope, Status: status}
			report, err := p.DetectOrphans(context.Background(), in)
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			if len(report.Orphans) == 0 {
				fmt.Printf("No orphans found across %d artifacts.\n", report.TotalScanned)
				return nil
			}
			for _, o := range report.Orphans {
				fmt.Printf("%-16s %-5s [%s] %s\n  → %s\n\n", o.ID, o.Kind, o.Status, o.Title, o.Reason)
			}
			fmt.Printf("%d orphan(s) across %d artifacts\n", report.TotalOrphans, report.TotalScanned)
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().StringVar(&status, "status", "", "filter by status (default: non-terminal)")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

// --- serve ---

func serveCmd() *cobra.Command {
	var scopes []string
	var transport, addr string
	var enableUI bool
	var uiAddr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio or HTTP)",
		Long: `Start the Scribe MCP server.

  stdio (default): reads/writes JSON-RPC over stdin/stdout.
  http:            starts a Streamable HTTP server on --addr.
  --ui:            also starts a read-only web UI on --ui-addr.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			s, err := store.OpenSQLite(cfg.DB)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			t := cfg.Transport
			if cmd.Flags().Changed("transport") {
				t = transport
			}
			a := cfg.Addr
			if cmd.Flags().Changed("addr") {
				a = addr
			}
			homeScopes := scopes
			if len(homeScopes) == 0 {
				homeScopes = cfg.Scopes
			}
			if len(homeScopes) == 0 {
				homeScopes = detectScopes()
			}

			var vocabKinds []string
			if cfg.Vocabulary != nil {
				vocabKinds = cfg.Vocabulary.Kinds
			}
			srv, _ := mcp.NewServer(s, homeScopes, vocabKinds)

			if enableUI {
				proto := protocol.New(s, nil, homeScopes, nil)
				uiSrv := web.NewServer(proto)
				go func() {
					fmt.Fprintf(os.Stderr, "scribe: UI listening on %s\n", uiAddr)
					if err := http.ListenAndServe(uiAddr, uiSrv); err != nil {
						fmt.Fprintf(os.Stderr, "scribe: UI error: %v\n", err)
					}
				}()
			}

			if t == "http" {
				handler := sdkmcp.NewStreamableHTTPHandler(
					func(r *http.Request) *sdkmcp.Server { return srv },
					nil,
				)
				fmt.Fprintf(os.Stderr, "scribe: listening on %s\n", a)
				return http.ListenAndServe(a, handler)
			}
			return srv.Run(context.Background(), &sdkmcp.StdioTransport{})
		},
	}
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "home scopes (repeatable)")
	cmd.Flags().StringVar(&transport, "transport", "stdio", "transport type: stdio, http")
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address for http transport")
	cmd.Flags().BoolVar(&enableUI, "ui", false, "start the read-only web UI alongside the MCP server")
	cmd.Flags().StringVar(&uiAddr, "ui-addr", ":8082", "listen address for the web UI")
	return cmd
}

func detectScopes() []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	if isGitRepo(cwd) {
		return []string{filepath.Base(cwd)}
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return nil
	}
	var scopes []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if isGitRepo(filepath.Join(cwd, e.Name())) {
			scopes = append(scopes, e.Name())
		}
	}
	return scopes
}

func isGitRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

// --- reseed / seed (store-level plumbing) ---

var idRe = regexp.MustCompile(`^([A-Z]+)-\d+-(\d+)$`)

func reseedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reseed",
		Short: "Scan all artifacts and fix sequence counters",
		RunE: func(cmd *cobra.Command, args []string) error {
			s := mustStore()
			defer s.Close()
			ctx := context.Background()
			arts, err := s.List(ctx, model.Filter{})
			if err != nil {
				return err
			}
			maxSeq := make(map[string]uint64)
			for _, a := range arts {
				m := idRe.FindStringSubmatch(a.ID)
				if m == nil {
					continue
				}
				prefix := m[1]
				seq, _ := strconv.ParseUint(m[2], 10, 64)
				if seq > maxSeq[prefix] {
					maxSeq[prefix] = seq
				}
			}
			for prefix, mx := range maxSeq {
				next := mx + 1
				if err := s.SeedSequence(ctx, prefix, next, false); err != nil {
					return fmt.Errorf("seed %s: %w", prefix, err)
				}
				fmt.Printf("%s -> next seq = %d\n", prefix, next)
			}
			return nil
		},
	}
}

func seedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed <PREFIX> <next-seq>",
		Short: "Force-set the sequence counter for a prefix",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			s := mustStore()
			defer s.Close()
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

// --- vocab ---

func vocabCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vocab",
		Short: "Manage the enforced kind vocabulary",
	}

	listVocabCmd := &cobra.Command{
		Use:   "list",
		Short: "Show registered artifact kinds",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			for _, k := range p.VocabList() {
				fmt.Println(k)
			}
			return nil
		},
	}

	addVocabCmd := &cobra.Command{
		Use:   "add <kind>",
		Short: "Register a new artifact kind",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			if err := p.VocabAdd(args[0]); err != nil {
				return err
			}
			cfg := mustConfig()
			if err := persistVocab(cfg, p.Vocab()); err != nil {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			if err := p.VocabRemove(context.Background(), args[0]); err != nil {
				return err
			}
			cfg := mustConfig()
			if err := persistVocab(cfg, p.Vocab()); err != nil {
				return err
			}
			fmt.Printf("removed kind %q\n", args[0])
			return nil
		},
	}

	var dryRun bool
	migrateVocabCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Rewrite artifact kinds using the canonical absorption table",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			result, err := p.VocabMigrate(context.Background(), dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Println("DRY RUN (no changes written)")
			}
			if len(result.Rewrites) == 0 && result.Archived == 0 {
				fmt.Println("nothing to migrate")
				return nil
			}
			keys := make([]string, 0, len(result.Rewrites))
			for k := range result.Rewrites {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("  %s: %d\n", k, result.Rewrites[k])
			}
			if result.Archived > 0 {
				fmt.Printf("  rule → archived: %d\n", result.Archived)
			}
			fmt.Printf("total: %d artifacts affected\n", result.Total)
			return nil
		},
	}
	migrateVocabCmd.Flags().BoolVar(&dryRun, "dry-run", true, "preview changes without writing (use --dry-run=false to commit)")

	cmd.AddCommand(listVocabCmd, addVocabCmd, removeVocabCmd, migrateVocabCmd)
	return cmd
}

func persistVocab(cfg *config.Config, vocab []string) error {
	if cfg.Vocabulary == nil {
		cfg.Vocabulary = &config.Vocabulary{}
	}
	cfg.Vocabulary.Kinds = vocab
	return config.Save(cfg)
}

// --- sort helper ---

func sortArts(arts []*model.Artifact, field string) {
	sort.Slice(arts, func(i, j int) bool {
		switch field {
		case "title":
			return arts[i].Title < arts[j].Title
		case "status":
			return arts[i].Status < arts[j].Status
		case "scope":
			return arts[i].Scope < arts[j].Scope
		case "kind":
			return arts[i].Kind < arts[j].Kind
		case "sprint":
			return arts[i].Sprint < arts[j].Sprint
		default:
			return arts[i].ID < arts[j].ID
		}
	})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- tools ---

func toolsCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List all available MCP tools with descriptions",
		Long:  "Print a table of every MCP tool Scribe exposes, with name, description, keywords, and categories. Useful for discovering what the agent can do without reading the README.",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := mcp.ToolRegistry()

			var tools []directive.ToolMeta
			if category != "" {
				tools = reg.ByCategory(category)
			} else {
				tools = reg.List()
			}

			if len(tools) == 0 {
				fmt.Println("No tools found.")
				return nil
			}

			nameW, descW, kwW := 4, 11, 8
			for _, t := range tools {
				if len(t.Name) > nameW {
					nameW = len(t.Name)
				}
				if len(t.Description) > descW {
					descW = len(t.Description)
				}
				kw := strings.Join(t.Keywords, ", ")
				if len(kw) > kwW {
					kwW = len(kw)
				}
			}

			fmtStr := fmt.Sprintf("%%-%ds  %%-%ds  %%-%ds  %%s\n", nameW, descW, kwW)
			fmt.Printf(fmtStr, "NAME", "DESCRIPTION", "KEYWORDS", "CATEGORIES")
			fmt.Printf(fmtStr,
				strings.Repeat("-", nameW),
				strings.Repeat("-", descW),
				strings.Repeat("-", kwW),
				strings.Repeat("-", 10),
			)
			for _, t := range tools {
				fmt.Printf(fmtStr,
					t.Name,
					t.Description,
					strings.Join(t.Keywords, ", "),
					strings.Join(t.Categories, ", "),
				)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&category, "category", "", "filter tools by category")
	return cmd
}

// --- ui ---

func uiCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Start the read-only web UI (standalone, no MCP server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := mustConfig()
			s, err := store.OpenSQLite(cfg.DB)
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			scopes := cfg.Scopes
			if len(scopes) == 0 {
				scopes = detectScopes()
			}
			proto := protocol.New(s, cfg.Schema, scopes, nil)
			uiSrv := web.NewServer(proto)

			fmt.Fprintf(os.Stderr, "scribe: UI listening on %s\n", addr)
			return http.ListenAndServe(addr, uiSrv)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8082", "listen address for the web UI")
	return cmd
}
