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

	"github.com/dpopsuev/scribe/mcp"
	"github.com/dpopsuev/scribe/mcpclient"
	"github.com/dpopsuev/scribe/model"
	"github.com/dpopsuev/scribe/protocol"
	"github.com/dpopsuev/scribe/render"
	"github.com/dpopsuev/scribe/store"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

var dbPath string

func main() {
	root := &cobra.Command{
		Use:   "scribe",
		Short: "Lean artifact store with native DAG support",
	}
	root.PersistentFlags().StringVar(&dbPath, "db", envOr("SCRIBE_DB", store.DefaultSQLitePath()), "database path")

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
		motdCmd(),
		drainCmd(),
		inventoryCmd(),
		linkCmd(),
		unlinkCmd(),
		contextCmd(),
		overlapsCmd(),
		serveCmd(),
		reseedCmd(),
		seedCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func mustProto() (*protocol.Protocol, func()) {
	s, err := store.OpenSQLite(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	return protocol.New(s, nil, nil), func() { s.Close() }
}

func mustStore() *store.SQLiteStore {
	s, err := store.OpenSQLite(dbPath)
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
	cmd.Flags().StringVar(&in.Kind, "kind", "contract", "artifact kind")
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
		Short: "Set the current goal (retires any previous, creates a root epic)",
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
	setGoalCmd.Flags().StringVar(&in.Kind, "kind", "epic", "kind for the root delivery artifact")

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
	var cascade bool
	cmd := &cobra.Command{
		Use:   "archive <ID> [ID...]",
		Short: "Archive artifacts (marks read-only; use --cascade for subtrees)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
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
	return cmd
}

// --- vacuum ---

func vacuumCmd() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "vacuum",
		Short: "Delete archived artifacts older than --days (default 90)",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, close := mustProto()
			defer close()
			deleted, err := p.Vacuum(context.Background(), days)
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
	cmd.Flags().StringVar(&kind, "kind", "contract", "artifact kind to scan")
	cmd.Flags().StringVar(&status, "status", "active", "artifact status to scan")
	cmd.Flags().StringVar(&format, "format", "text", "output format (text, json)")
	return cmd
}

// --- serve ---

func serveCmd() *cobra.Command {
	var scopes []string
	var transport, addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio or HTTP)",
		Long: `Start the Scribe MCP server.

  stdio (default): reads/writes JSON-RPC over stdin/stdout.
  http:            starts a Streamable HTTP server on --addr.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := mustStore()
			defer s.Close()
			homeScopes := scopes
			if len(homeScopes) == 0 {
				homeScopes = detectScopes()
			}
			srv := mcp.NewServer(s, homeScopes)
			if transport == "http" {
				handler := sdkmcp.NewStreamableHTTPHandler(
					func(r *http.Request) *sdkmcp.Server { return srv },
					nil,
				)
				fmt.Fprintf(os.Stderr, "scribe: listening on %s\n", addr)
				return http.ListenAndServe(addr, handler)
			}
			return srv.Run(context.Background(), &sdkmcp.StdioTransport{})
		},
	}
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "home scopes (repeatable)")
	cmd.Flags().StringVar(&transport, "transport", envOr("SCRIBE_TRANSPORT", "stdio"), "transport type: stdio, http ($SCRIBE_TRANSPORT)")
	cmd.Flags().StringVar(&addr, "addr", envOr("SCRIBE_ADDR", ":8080"), "listen address for http transport ($SCRIBE_ADDR)")
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
