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

	"github.com/dpopsuev/ordo/registry"
	"github.com/dpopsuev/scribe/service"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

const (
	lexiconDaemonPollInterval = 30 * time.Second

	// slog key constants for lexicon daemon — sloglint no-raw-keys.
	logKeyLexRoot     = "lex_root"
	logKeyLexFile     = "file"
	logKeyLexInterval = "interval"
)

func LexiconCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lexicon",
		Short: "Manage lexicon sync to Parchment",
	}

	syncSubCmd := &cobra.Command{
		Use:   "sync",
		Short: "One-shot sync all lexicon rules and skills to Parchment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, cleanup := MustService()
			defer cleanup()
			lexRoot := EnvOr("LEX_ROOT", registry.DefaultRoot())
			n, err := svc.SyncLexicon(cmd.Context(), lexRoot)
			if err != nil {
				return err
			}
			fmt.Printf("synced %d lexicon artifact(s) to Parchment\n", n)
			return nil
		},
	}

	daemonSubCmd := &cobra.Command{
		Use:   "daemon",
		Short: "Watch lexicon directory and sync to Parchment on change",
		Long: `Runs sync once at startup, then watches the lexicon directory.
Re-syncs on any .yaml/.yml/.md change. Exits on SIGTERM/SIGINT.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, cleanup := MustService()
			defer cleanup()

			lexRoot := EnvOr("LEX_ROOT", registry.DefaultRoot())

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer cancel()

			slog.InfoContext(ctx, "scribe lexicon daemon starting", slog.String(logKeyLexRoot, lexRoot))
			if _, err := svc.SyncLexicon(ctx, lexRoot); err != nil {
				slog.WarnContext(ctx, "initial sync failed", slog.Any(logKeyError, err))
			}

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				slog.WarnContext(ctx, "fsnotify unavailable, falling back to polling", slog.Any(logKeyError, err))
				return lexiconDaemonPoll(ctx, svc, lexRoot)
			}
			defer func() { _ = watcher.Close() }()

			if err := watcher.Add(lexRoot); err != nil {
				slog.WarnContext(ctx, "cannot watch lex root, falling back to polling", slog.Any(logKeyError, err))
				return lexiconDaemonPoll(ctx, svc, lexRoot)
			}
			slog.InfoContext(ctx, "watching for lexicon changes", slog.String(logKeyPath, lexRoot))

			for {
				select {
				case <-ctx.Done():
					slog.InfoContext(ctx, "scribe lexicon daemon stopping")
					return nil
				case event, ok := <-watcher.Events:
					if !ok {
						return nil
					}
					ext := strings.ToLower(filepath.Ext(event.Name))
					if ext != ".yaml" && ext != ".yml" && ext != ".md" {
						continue
					}
					slog.InfoContext(ctx, "lexicon changed, re-syncing", slog.String(logKeyLexFile, event.Name))
					if _, err := svc.SyncLexicon(ctx, lexRoot); err != nil {
						slog.WarnContext(ctx, "re-sync failed", slog.Any(logKeyError, err))
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

	cmd.AddCommand(syncSubCmd, daemonSubCmd)
	return cmd
}

func lexiconDaemonPoll(ctx context.Context, svc *service.Service, lexRoot string) error {
	slog.InfoContext(ctx, "polling for lexicon changes",
		slog.String(logKeyLexRoot, lexRoot),
		slog.Duration(logKeyLexInterval, lexiconDaemonPollInterval))
	ticker := time.NewTicker(lexiconDaemonPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.InfoContext(ctx, "scribe lexicon daemon stopping")
			return nil
		case <-ticker.C:
			if _, err := svc.SyncLexicon(ctx, lexRoot); err != nil {
				slog.WarnContext(ctx, "poll sync failed", slog.Any(logKeyError, err))
			}
		}
	}
}
