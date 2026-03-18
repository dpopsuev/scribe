package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/directive"
	"github.com/dpopsuev/scribe/mcp"
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
	Version    = "dev"
)

func main() {
	root := &cobra.Command{
		Use:   "scribe",
		Short: "Lean artifact store with native DAG support",
	}
	root.PersistentFlags().StringVar(&dbPath, "db", "", "database path (overrides config file and $SCRIBE_DB)")
	root.PersistentFlags().StringVar(&configPath, "config", "", "config file path (default: ./scribe.yaml or ~/.scribe/scribe.yaml)")

	root.AddCommand(
		&cobra.Command{
			Use:   "version",
			Short: "Print the version",
			Run:   func(cmd *cobra.Command, args []string) { fmt.Printf("scribe %s\n", Version) },
		},
		createCmd(),
		showCmd(),
		listCmd(),
		setCmd(),
		deleteCmd(),
		treeCmd(),
		briefingCmd(),
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
		overlapsCmd(),
		orphansCmd(),
		scopeKeysCmd(),
		kindCodesCmd(),
		serveCmd(),
		reseedCmd(),
		seedCmd(),
		toolsCmd(),
		uiCmd(),
		vocabCmd(),
		lintCmd(),
		checkCmd(),
		migrateCmd(),
		configCmd(),
		exportCmd(),
		importCmd(),
		seedDirCmd(),
		capsuleCmd(),
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
		cfg.DB.SQLite.Path = dbPath
	}
	return cfg
}

func mustProto() (*protocol.Protocol, func()) {
	cfg := mustConfig()
	s, err := store.OpenSQLiteConfig(cfg.SQLiteConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	return protocol.New(s, cfg.Schema, nil, nil, cfg.ProtocolIDConfig()), func() { s.Close() }
}

func mustStore() *store.SQLiteStore {
	cfg := mustConfig()
	s, err := store.OpenSQLiteConfig(cfg.SQLiteConfig())
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
				if in.GroupBy == "scope_label" {
					scopeLabels := make(map[string][]string)
					infos, err := p.ListScopeInfo(context.Background())
					if err == nil {
						for _, info := range infos {
							if len(info.Labels) > 0 {
								scopeLabels[info.Scope] = info.Labels
							}
						}
					}
					fmt.Print(render.GroupedTableByScopeLabel(arts, scopeLabels))
				} else if in.GroupBy != "" {
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
			found, err := p.DetachSection(context.Background(), args[0], args[1])
			if err != nil {
				return err
			}
			if !found {
				return fmt.Errorf("section %q not found on %s", args[1], args[0])
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
			tree, err := p.ArtifactTree(context.Background(), protocol.TreeInput{ID: args[0]})
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

// --- briefing ---

func briefingCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "briefing <ID>",
		Short: "Recursive edge-aware traversal showing the full context chain from any artifact",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			tree, err := p.ArtifactTree(context.Background(), protocol.TreeInput{
				ID:        args[0],
				Relation:  "*",
				Direction: "both",
			})
			if err != nil {
				return err
			}
			if format == "json" {
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

func printBriefing(node *protocol.TreeNode, prefix string, last bool, b *strings.Builder) {
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

	kindStatus := node.Status
	if node.Kind != "" {
		kindStatus = node.Kind + "|" + node.Status
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
			if len(m.Warnings) > 0 {
				var lines []string
				for _, w := range m.Warnings {
					lines = append(lines, "  ⚠ "+w)
				}
				sections = append(sections, "Warnings:\n"+strings.Join(lines, "\n"))
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
			for kind, arts := range inv.Tracked {
				if len(arts) == 0 {
					continue
				}
				fmt.Printf("\nTracked %s:\n", kind)
				for _, a := range arts {
					prefix := ""
					if a.Scope != "" {
						prefix = "[" + a.Scope + "] "
					}
					fmt.Printf("  %s %s%s\n", a.ID, prefix, a.Title)
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
		Long:  "Relations: parent_of, depends_on, justifies, implements, documents",
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

// --- scope-keys ---

func scopeKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scope-keys [set SCOPE KEY | set-labels SCOPE LABEL,...]",
		Short: "List or manage scope key mappings and labels",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			if len(args) == 0 {
				infos, err := p.ListScopeInfo(context.Background())
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
				return p.SetScopeKey(context.Background(), args[1], args[2])
			}
			if len(args) == 3 && args[0] == "set-labels" {
				labels := strings.Split(args[2], ",")
				for i := range labels {
					labels[i] = strings.TrimSpace(labels[i])
				}
				return p.SetScopeLabels(context.Background(), args[1], labels)
			}
			return fmt.Errorf("usage: scope-keys [set SCOPE KEY | set-labels SCOPE LABEL,...]")
		},
	}
}

// --- kind-codes ---

func kindCodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kind-codes",
		Short: "List kind code mappings",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			codes := p.ListKindCodes()
			for kind, code := range codes {
				fmt.Printf("%s → %s\n", kind, code)
			}
			return nil
		},
	}
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

func initLogger() {
	level := slog.LevelInfo
	if v := os.Getenv("SCRIBE_LOG_LEVEL"); v != "" {
		switch strings.ToLower(v) {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
}

// crashDir returns the crash dump directory, creating it if needed.
func crashDir() string {
	dir := envOr("SCRIBE_CRASH_DIR", filepath.Join(envOr("SCRIBE_ROOT", filepath.Join(os.Getenv("HOME"), ".scribe")), "crash"))
	os.MkdirAll(dir, 0o755)
	return dir
}

// dumpHeapProfile writes a heap profile to the crash directory.
func dumpHeapProfile(label string) string {
	path := filepath.Join(crashDir(), fmt.Sprintf("%s-%s.pb.gz", label, time.Now().Format("20060102-150405")))
	f, err := os.Create(path)
	if err != nil {
		slog.Error("crash dump: create file failed", "path", path, "error", err)
		return ""
	}
	defer f.Close()
	if err := pprof.WriteHeapProfile(f); err != nil {
		slog.Error("crash dump: write heap profile failed", "error", err)
		return ""
	}
	return path
}

// dumpGoroutineProfile writes a goroutine profile to the crash directory.
func dumpGoroutineProfile(label string) string {
	path := filepath.Join(crashDir(), fmt.Sprintf("%s-goroutine-%s.txt", label, time.Now().Format("20060102-150405")))
	f, err := os.Create(path)
	if err != nil {
		slog.Error("crash dump: create goroutine file failed", "error", err)
		return ""
	}
	defer f.Close()
	if p := pprof.Lookup("goroutine"); p != nil {
		p.WriteTo(f, 1)
	}
	return path
}

// startMemoryWatchdog launches a background goroutine that samples memory every 60s.
// On threshold breach, it auto-captures heap profiles to the crash directory.
func startMemoryWatchdog(ctx context.Context) {
	warnMB := 512
	critMB := 2048
	if v := os.Getenv("SCRIBE_MEM_WARN_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			warnMB = n
		}
	}
	if v := os.Getenv("SCRIBE_MEM_CRIT_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			critMB = n
		}
	}

	slog.Info("memory watchdog started", "warn_mb", warnMB, "crit_mb", critMB)

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		warnFired := false
		critFired := false

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				heapMB := int(m.HeapAlloc / (1024 * 1024))
				sysMB := int(m.HeapSys / (1024 * 1024))
				goroutines := runtime.NumGoroutine()

				slog.Debug("watchdog sample",
					"heap_alloc_mb", heapMB,
					"heap_sys_mb", sysMB,
					"goroutines", goroutines,
				)

				if heapMB >= critMB && !critFired {
					critFired = true
					slog.Error("watchdog CRITICAL: heap exceeds critical threshold",
						"heap_mb", heapMB, "threshold_mb", critMB)
					heapPath := dumpHeapProfile("critical")
					goroutinePath := dumpGoroutineProfile("critical")
					slog.Error("crash dumps captured",
						"heap", heapPath, "goroutine", goroutinePath)
				} else if heapMB >= warnMB && !warnFired {
					warnFired = true
					slog.Warn("watchdog WARNING: heap exceeds warn threshold",
						"heap_mb", heapMB, "threshold_mb", warnMB)
					heapPath := dumpHeapProfile("warning")
					slog.Warn("heap dump captured", "path", heapPath)
				}

				// Reset flags if memory drops back below thresholds
				if heapMB < warnMB {
					warnFired = false
					critFired = false
				}
			}
		}
	}()
}

func serveCmd() *cobra.Command {
	var scopes []string
	var transport, addr string
	var enableUI bool
	var uiAddr string
	var enablePprof bool
	var pprofAddr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio or HTTP)",
		Long: `Start the Scribe MCP server.

  stdio (default): reads/writes JSON-RPC over stdin/stdout.
  http:            starts a Streamable HTTP server on --addr.
  --ui:            also starts a read-only web UI on --ui-addr.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			initLogger()
			cfg := mustConfig()

			slog.Info("starting scribe",
				"version", Version,
				"db", cfg.DBPath(),
				"id_format", cfg.IDFormat,
			)

			s, err := store.OpenSQLiteConfig(cfg.SQLiteConfig())
			if err != nil {
				slog.Error("failed to open store", "db", cfg.DBPath(), "error", err)
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			// Auto-seed from config seed_dir if DB has zero templates
			if cfg.SeedDir != "" {
				proto := protocol.New(s, cfg.Schema, nil, nil, cfg.ProtocolIDConfig())
				templates, _ := proto.ListArtifacts(context.Background(), protocol.ListInput{Kind: "template"})
				if len(templates) == 0 {
					result, err := proto.Seed(context.Background(), cfg.SeedDir)
					if err != nil {
						slog.Warn("auto-seed failed", "dir", cfg.SeedDir, "error", err)
					} else if len(result.Created) > 0 {
						slog.Info("auto-seed completed", "dir", cfg.SeedDir, "created", len(result.Created))
					}
				}
			}

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
				homeScopes = cfg.ResolvedScopes()
			}
			if len(homeScopes) == 0 {
				homeScopes = detectScopes()
			}

			idc := cfg.ProtocolIDConfig()

			// Populate scope_keys from ScopeConfigs
			if len(cfg.ScopeConfigs) > 0 {
				for name, sc := range cfg.ScopeConfigs {
					if sc.Key != "" {
						_ = s.SetScopeKey(context.Background(), name, sc.Key, false)
					}
					if len(sc.Labels) > 0 {
						_ = s.SetScopeLabels(context.Background(), name, sc.Labels)
					}
				}
			}

			if ws := os.Getenv("SCRIBE_WORKSPACE"); ws != "" && cfg.Workspaces != nil {
				if wsScopes, ok := cfg.Workspaces[ws]; ok {
					homeScopes = wsScopes
					slog.Info("workspace override from env", "workspace", ws, "scopes", wsScopes)
				}
			}

			srv, _ := mcp.NewServer(s, homeScopes, nil, idc, Version)

			slog.Info("server configured",
				"transport", t,
				"scopes", homeScopes,
			)

			if enableUI {
				proto := protocol.New(s, nil, homeScopes, nil, idc)
				uiSrv := web.NewServer(proto)
				go func() {
					slog.Info("UI listening", "addr", uiAddr)
					if err := http.ListenAndServe(uiAddr, uiSrv); err != nil {
						slog.Error("UI server error", "error", err)
					}
				}()
			}

			// Start memory watchdog
			watchdogCtx, watchdogCancel := context.WithCancel(context.Background())
			defer watchdogCancel()
			startMemoryWatchdog(watchdogCtx)

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()

			if t == "http" {
				var serverCache sync.Map
				serverCache.Store("", srv)

				handler := sdkmcp.NewStreamableHTTPHandler(
					func(r *http.Request) *sdkmcp.Server {
						ws := r.URL.Query().Get("workspace")
						if ws == "" {
							return srv
						}
						if cached, ok := serverCache.Load(ws); ok {
							return cached.(*sdkmcp.Server)
						}
						wsScopes, ok := cfg.Workspaces[ws]
						if !ok {
							slog.Warn("unknown workspace, using default", "workspace", ws)
							return srv
						}
						wsSrv, _ := mcp.NewServer(s, wsScopes, nil, idc, Version)
						serverCache.Store(ws, wsSrv)
						slog.Info("created workspace server", "workspace", ws, "scopes", wsScopes)
						return wsSrv
					},
					&sdkmcp.StreamableHTTPOptions{
						SessionTimeout: sessionTimeout(),
					},
				)

				if enablePprof {
					go func() {
						slog.Info("pprof listening", "addr", pprofAddr)
						if err := http.ListenAndServe(pprofAddr, nil); err != nil {
							slog.Error("pprof server error", "error", err)
						}
					}()
				}

				httpSrv := &http.Server{Addr: a, Handler: handler}
				go func() {
					<-ctx.Done()
					slog.Info("shutdown signal received, draining connections")

					// Capture crash dumps on shutdown
					if heapPath := dumpHeapProfile("shutdown"); heapPath != "" {
						slog.Info("shutdown heap dump captured", "path", heapPath)
					}
					if goroutinePath := dumpGoroutineProfile("shutdown"); goroutinePath != "" {
						slog.Info("shutdown goroutine dump captured", "path", goroutinePath)
					}

					shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					httpSrv.Shutdown(shutCtx)
				}()

				slog.Info("listening", "addr", a, "session_timeout", sessionTimeout())
				if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
					return err
				}
				slog.Info("server stopped, closing store")
				return nil
			}
			slog.Info("serving via stdio")
			return srv.Run(ctx, &sdkmcp.StdioTransport{})
		},
	}
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "home scopes (repeatable)")
	cmd.Flags().StringVar(&transport, "transport", "stdio", "transport type: stdio, http")
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address for http transport")
	cmd.Flags().BoolVar(&enableUI, "ui", false, "start the read-only web UI alongside the MCP server")
	cmd.Flags().StringVar(&uiAddr, "ui-addr", ":8082", "listen address for the web UI")
	cmd.Flags().BoolVar(&enablePprof, "pprof", false, "enable pprof profiling endpoint (localhost only)")
	cmd.Flags().StringVar(&pprofAddr, "pprof-addr", "127.0.0.1:6060", "listen address for pprof")
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
			fmt.Printf("removed kind %q\n", args[0])
			return nil
		},
	}

	cmd.AddCommand(listVocabCmd, addVocabCmd, removeVocabCmd)
	return cmd
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

func sessionTimeout() time.Duration {
	if v := os.Getenv("SCRIBE_SESSION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("invalid SCRIBE_SESSION_TIMEOUT, using default", "value", v)
	}
	return 4 * time.Hour
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
			s, err := store.OpenSQLiteConfig(cfg.SQLiteConfig())
			if err != nil {
				return fmt.Errorf("open store: %w", err)
			}
			defer s.Close()

			scopes := cfg.Scopes
			if len(scopes) == 0 {
				scopes = detectScopes()
			}
			proto := protocol.New(s, cfg.Schema, scopes, nil, cfg.ProtocolIDConfig())
			uiSrv := web.NewServer(proto)

			fmt.Fprintf(os.Stderr, "scribe: UI listening on %s\n", addr)
			return http.ListenAndServe(addr, uiSrv)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8082", "listen address for the web UI")
	return cmd
}

func lintCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate the resolved schema for internal consistency",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			results := p.Lint()
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}
			errors, warnings := 0, 0
			for _, r := range results {
				switch r.Level {
				case "error":
					errors++
					fmt.Printf("ERROR %s\n", r.Message)
				case "warn":
					warnings++
					fmt.Printf("WARN  %s\n", r.Message)
				}
			}
			if errors == 0 && warnings == 0 {
				fmt.Println("OK    schema is valid")
			} else {
				fmt.Printf("OK    schema validated (%d error(s), %d warning(s))\n", errors, warnings)
			}
			if errors > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func checkCmd() *cobra.Command {
	var scope, format string
	var fix bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate DB artifacts against the resolved schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			ctx := context.Background()

			if fix {
				report, fixes, err := p.CheckFix(ctx, scope)
				if err != nil {
					return err
				}
				if format == "json" {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(map[string]any{
						"report": report,
						"fixes":  fixes,
					})
				}
				for _, v := range report.Violations {
					fmt.Printf("%-18s %-18s %s\n", v.ID, v.Category, v.Detail)
				}
				if len(fixes) > 0 {
					fmt.Println("\nFixes applied:")
					for _, f := range fixes {
						fmt.Printf("  %s\n", f)
					}
				}
				fmt.Printf("\nScanned %d, passed %d, violations %d, fixes %d\n",
					report.TotalScanned, report.TotalPassed, report.TotalViolations, len(fixes))
				if report.TotalViolations > 0 {
					os.Exit(1)
				}
				return nil
			}

			report, err := p.Check(ctx, scope)
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(report)
			}
			for _, v := range report.Violations {
				fmt.Printf("%-18s %-18s %s\n", v.ID, v.Category, v.Detail)
			}
			fmt.Printf("\nScanned %d, passed %d, violations %d\n",
				report.TotalScanned, report.TotalPassed, report.TotalViolations)
			if report.TotalViolations > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "limit to a specific scope")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	cmd.Flags().BoolVar(&fix, "fix", false, "auto-repair fixable violations")
	return cmd
}

func migrateCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Run DB migration: remove legacy edges, validate and fix artifacts against schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			result, err := p.Migrate(context.Background())
			if err != nil {
				return err
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}
			if result.SatisfiesRemoved > 0 {
				fmt.Printf("Removed %d satisfies edges\n", result.SatisfiesRemoved)
			}
			for _, f := range result.Fixes {
				fmt.Printf("  fix: %s\n", f)
			}
			fmt.Printf("\nScanned %d, passed %d, violations %d, fixes %d\n",
				result.Report.TotalScanned, result.Report.TotalPassed,
				result.Report.TotalViolations, len(result.Fixes))

			lintResults := p.Lint()
			lintErrors := 0
			for _, r := range lintResults {
				if r.Level == "error" {
					lintErrors++
					fmt.Printf("LINT ERROR %s\n", r.Message)
				}
			}
			if lintErrors > 0 {
				fmt.Printf("\n%d lint error(s) found\n", lintErrors)
				os.Exit(1)
			}
			fmt.Println("Schema lint: OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

func configCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Dump resolved configuration as YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Resolve(configPath)
			if err != nil {
				return err
			}
			if dbPath != "" {
				cfg.DB.SQLite.Path = dbPath
			}
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
}

func exportCmd() *cobra.Command {
	var scope, output string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export artifacts as JSON-lines",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			var w io.Writer = os.Stdout
			if output != "" && output != "-" {
				f, err := os.Create(output)
				if err != nil {
					return err
				}
				defer f.Close()
				w = f
			}
			n, err := p.Export(context.Background(), w, scope)
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

func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [file]",
		Short: "Import artifacts from JSON-lines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			n, err := p.Import(context.Background(), f)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "imported %d artifacts\n", n)
			return nil
		},
	}
}

func capsuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capsule",
		Short: "Portable instance encapsulation: export, import, inspect",
	}

	var output string
	exportCapsule := &cobra.Command{
		Use:   "export",
		Short: "Export entire Scribe instance to a .capsule file",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			f, err := os.Create(output)
			if err != nil {
				return err
			}
			defer f.Close()
			m, err := p.CapsuleExport(context.Background(), f, Version)
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
			p, close := mustProto()
			defer close()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			m, err := p.CapsuleImport(context.Background(), f)
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
			defer f.Close()
			m, err := protocol.CapsuleInspect(f)
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

func seedDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed-dir <dir>",
		Short: "Seed templates and config from a directory (idempotent)",
		Long:  "Reads templates from <dir>/templates/*.md and config from <dir>/config/*.yaml. Skips artifacts that already exist.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			result, err := p.Seed(context.Background(), args[0])
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
