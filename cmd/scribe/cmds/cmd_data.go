package cmds

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

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
			n, err := svc.ExportScope(cmd.Context(), args[0], args[1], false)
			if err != nil {
				return err
			}
			fmt.Printf("exported %d artifact(s) to %s\n", n, args[1])
			return nil
		},
	}
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
