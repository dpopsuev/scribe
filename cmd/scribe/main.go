package main

import (
	"context"
	"fmt"
	"log/slog"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dpopsuev/ordo/registry"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/cmd/scribe/cmds"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/service"
	"github.com/fsnotify/fsnotify"
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
		cmds.CreateCmd(),
		cmds.ShowCmd(),
		cmds.ListCmd(),
		cmds.SetCmd(),
		cmds.DeleteCmd(),
		cmds.TreeCmd(),
		cmds.BriefingCmd(),
		cmds.SectionCmd(),
		cmds.SearchCmd(),
		cmds.GoalCmd(),
		cmds.ArchiveCmd(),
		cmds.VacuumCmd(),
		cmds.DfCmd(),
		cmds.MotdCmd(),
		cmds.DrainCmd(),
		cmds.InventoryCmd(),
		cmds.LinkCmd(),
		cmds.UnlinkCmd(),
		cmds.OverlapsCmd(),
		cmds.OrphansCmd(),
		cmds.ScopeKeysCmd(),
		cmds.KindCodesCmd(),
		cmds.ServeCmd(),
		cmds.ReseedCmd(),
		cmds.SeedCmd(),
		cmds.ToolsCmd(),
		cmds.UICmd(),
		cmds.VocabCmd(),
		cmds.LintCmd(),
		cmds.CheckCmd(),
		cmds.MigrateCmd(),
		cmds.MigrateIDsCmd(),
		cmds.ConfigCmd(),
		cmds.ExportCmd(),
		cmds.ImportCmd(),
		cmds.SeedDirCmd(),
		cmds.CapsuleCmd(),
		lexiconCmd(),
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

func mustProto() (proto *parchment.Protocol, cleanup func()) {
	cfg := mustConfig()
	s, err := parchment.OpenSQLiteConfig(cfg.SQLiteConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	return parchment.New(s, nil, nil, nil, cfg.ProtocolIDConfig()), func() { _ = s.Close() }
}

// mustService is the single construction path for CLI commands.
// Uses service.Open so homeScopes and schema loading are identical to the MCP server.
func mustService() (svc *service.Service, cleanup func()) {
	cfg := mustConfig()
	s, cl, err := service.Open(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return s, cl
}

func mustStore() *parchment.SQLiteStore {
	cfg := mustConfig()
	s, err := parchment.OpenSQLiteConfig(cfg.SQLiteConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	return s
}

const (
	lexiconDaemonPollInterval = 30 * time.Second

	// slog key constants for lexicon daemon — sloglint no-raw-keys.
	logKeyLexRoot     = "lex_root"
	logKeyLexPath     = "path"
	logKeyLexFile     = "file"
	logKeyLexError    = "error"
	logKeyLexInterval = "interval"
)

func lexiconCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lexicon",
		Short: "Manage lexicon sync to Parchment",
	}

	syncSubCmd := &cobra.Command{
		Use:   "sync",
		Short: "One-shot sync all lexicon rules and skills to Parchment",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, close := mustService()
			defer close()
			lexRoot := cmds.EnvOr("LEX_ROOT", registry.DefaultRoot())
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
			svc, close := mustService()
			defer close()

			lexRoot := cmds.EnvOr("LEX_ROOT", registry.DefaultRoot())

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer cancel()

			slog.InfoContext(ctx, "scribe lexicon daemon starting", slog.String(logKeyLexRoot, lexRoot))
			if _, err := svc.SyncLexicon(ctx, lexRoot); err != nil {
				slog.WarnContext(ctx, "initial sync failed", slog.Any(logKeyLexError, err))
			}

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				slog.WarnContext(ctx, "fsnotify unavailable, falling back to polling", slog.Any(logKeyLexError, err))
				return lexiconDaemonPoll(ctx, svc, lexRoot)
			}
			defer func() { _ = watcher.Close() }()

			if err := watcher.Add(lexRoot); err != nil {
				slog.WarnContext(ctx, "cannot watch lex root, falling back to polling", slog.Any(logKeyLexError, err))
				return lexiconDaemonPoll(ctx, svc, lexRoot)
			}
			slog.InfoContext(ctx, "watching for lexicon changes", slog.String(logKeyLexPath, lexRoot))

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
						slog.WarnContext(ctx, "re-sync failed", slog.Any(logKeyLexError, err))
					}
				case watchErr, ok := <-watcher.Errors:
					if !ok {
						return nil
					}
					slog.WarnContext(ctx, "watcher error", slog.Any(logKeyLexError, watchErr))
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
				slog.WarnContext(ctx, "poll sync failed", slog.Any(logKeyLexError, err))
			}
		}
	}
}


