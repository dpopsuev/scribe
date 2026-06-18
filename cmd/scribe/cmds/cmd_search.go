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

func SearchCmd() *cobra.Command {
	var scope, kind, status, format string
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search artifacts by substring across title, goal, sections, and extra",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSearch(args[0], scope, kind, status, format)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "", "filter by scope")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind")
	cmd.Flags().StringVar(&status, "status", "", "filter by status")
	cmd.Flags().StringVar(&format, "format", "table", "output format (table, json)")
	return cmd
}

func runSearch(query, scope, kind, status, format string) error {
	svc, cleanup := MustService()
	defer cleanup()
	labels := buildFilterLabels(scope, kind, status)
	matched, err := svc.Proto.SearchArtifacts(context.Background(), query, parchment.ListInput{Labels: labels})
	if err != nil {
		return err
	}
	if len(matched) == 0 {
		fmt.Printf("no artifacts matching %q\n", query)
		return nil
	}
	if format == formatJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(matched)
	}
	fmt.Print(parchment.RenderTable(matched))
	return nil
}

func buildFilterLabels(scope, kind, status string) []string {
	var labels []string
	if kind != "" {
		labels = append(labels, parchment.LabelPrefixKind+kind)
	}
	if status != "" {
		labels = append(labels, parchment.LabelPrefixStatus+status)
	}
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}
	return labels
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
			return runSectionAdd(args, file)
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

func runSectionAdd(args []string, file string) error {
	svc, cleanup := MustService()
	defer cleanup()
	id, name := args[0], args[1]
	body, err := readSectionBody(args, file)
	if err != nil {
		return err
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
}

func readSectionBody(args []string, file string) (string, error) {
	switch {
	case file == "-":
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	case file != "":
		data, err := os.ReadFile(file) //nolint:gosec // operator-supplied path
		if err != nil {
			return "", fmt.Errorf("read %s: %w", file, err)
		}
		return string(data), nil
	case len(args) == 3:
		return args[2], nil
	default:
		return "", fmt.Errorf("provide text as third argument, or use --file / --file=-") //nolint:err113 // user-facing hint
	}
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
