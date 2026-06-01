package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
)

// RegisterKnowledge adds search, section, and vocab commands to root.
func RegisterKnowledge(root *cobra.Command) {
	root.AddCommand(SearchCmd(), SectionCmd(), VocabCmd())
}

// SearchCmd returns the search command.
func SearchCmd() *cobra.Command {
	var scope, kind, status, format string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search artifacts by substring across title, goal, sections, and extra",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			li := parchment.ListInput{Kind: kind, Scope: scope, Status: status}
			matched, err := svc.Proto.SearchArtifacts(context.Background(), args[0], li)
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

// SectionCmd returns the section command group.
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

// VocabCmd returns the vocab command group.
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
