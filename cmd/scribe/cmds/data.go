package cmds

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/spf13/cobra"
)

var idRe = regexp.MustCompile(`^([A-Z]+)-\d+-(\d+)$`)

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

func SeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed <PREFIX> <next-seq>",
		Short: "Force-set the sequence counter for a prefix",
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
	return &cobra.Command{ //nolint:wsl // grouping with SeedDirCmd
		Use:   "seed-dir <dir>",
		Short: "Seed templates and config from a directory (idempotent)",
		Long:  "Reads templates from <dir>/templates/*.md and config from <dir>/config/*.yaml. Skips artifacts that already exist.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup := MustProto()
			defer cleanup()
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

func ExportCmd() *cobra.Command {
	var scope, output string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export artifacts as JSON-lines",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup := MustProto()
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

func ImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import [file]",
		Short: "Import artifacts from JSON-lines",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, cleanup := MustProto()
			defer cleanup()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			n, err := p.Import(context.Background(), f)
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
			p, cleanup := MustProto()
			defer cleanup()
			f, err := os.Create(output) //nolint:gosec // operator-supplied output path
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
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
			p, cleanup := MustProto()
			defer cleanup()
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
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
