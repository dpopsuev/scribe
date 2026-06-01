package cmds

import (
	"context"
	"fmt"
	"log/slog"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dpopsuev/scribe/service"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

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
