package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dpopsuev/ordo/registry"
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/cmd/scribe/cmds"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/directive"
	"github.com/dpopsuev/scribe/mcp"
	"github.com/dpopsuev/scribe/service"
	"github.com/dpopsuev/scribe/web"
	"github.com/fsnotify/fsnotify"
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
		serveCmd(),
		cmds.ReseedCmd(),
		cmds.SeedCmd(),
		toolsCmd(),
		uiCmd(),
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
	dir := cmds.EnvOr("SCRIBE_CRASH_DIR", filepath.Join(cmds.EnvOr("SCRIBE_ROOT", filepath.Join(os.Getenv("HOME"), ".scribe")), "crash"))
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

			// Resolve homeScopes: flag > CWD detection > config-derived.
			homeScopes := scopes
			if len(homeScopes) == 0 {
				cwd, _ := os.Getwd()
				if sc := cfg.ScopeForDir(cwd); sc != "" {
					homeScopes = []string{sc}
				} else {
					homeScopes = cfg.ResolvedScopes()
				}
			}

			// Single construction path via service.Open.
			svc, cleanup, err := service.Open(cfg, homeScopes)
			if err != nil {
				slog.Error("failed to open store", "db", cfg.DBPath(), "error", err)
				return err
			}
			defer cleanup()

			// Seed once on first run; skip if templates already exist.
			if cfg.SeedDir != "" {
				templates, _ := svc.Proto.ListArtifacts(context.Background(), parchment.ListInput{Kind: "template"})
				if len(templates) == 0 {
					result, err := svc.Proto.Seed(context.Background(), cfg.SeedDir)
					if err != nil {
						slog.Warn("auto-seed failed", "dir", cfg.SeedDir, "error", err)
					} else if len(result.Created) > 0 {
						slog.Info("auto-seed completed", "dir", cfg.SeedDir, "created", len(result.Created))
					}
				}
			}

			// Apply scope config (key + labels).
			if len(cfg.ScopeConfigs) > 0 {
				store := svc.Proto.Store()
				for name, sc := range cfg.ScopeConfigs {
					if sc.Key != "" {
						_ = store.SetScopeKey(context.Background(), name, sc.Key, false)
					}
					if len(sc.Labels) > 0 {
						_ = store.SetScopeLabels(context.Background(), name, sc.Labels)
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

			srv, _ := mcp.NewServer(svc, nil, Version)

			slog.Info("server configured",
				"transport", t,
				"scopes", homeScopes,
			)

			if enableUI {
				uiSrv := web.NewServer(svc.Proto)
				go func() {
					slog.Info("UI listening", "addr", uiAddr)
					if err := http.ListenAndServe(uiAddr, uiSrv); err != nil {
						slog.Error("UI server error", "error", err)
					}
				}()
			}

			watchdogCtx, watchdogCancel := context.WithCancel(context.Background())
			defer watchdogCancel()
			startMemoryWatchdog(watchdogCtx)

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()

			if t == "http" {
				handler := sdkmcp.NewStreamableHTTPHandler(
					func(r *http.Request) *sdkmcp.Server { return srv },
					&sdkmcp.StreamableHTTPOptions{
						SessionTimeout: cmds.SessionTimeout(),
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

				mux := http.NewServeMux()
				mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"version":%q}`, Version)
				})
				mux.Handle("/", handler)

				httpSrv := &http.Server{Addr: a, Handler: mux, ReadHeaderTimeout: 10 * time.Second} //nolint:mnd // standard timeout
				go func() {
					<-ctx.Done()
					slog.Info("shutdown signal received, draining connections")
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

				slog.InfoContext(cmd.Context(), "listening", slog.String(logKeyAddr, a), slog.Duration(logKeySessionTimeout, cmds.SessionTimeout()))
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
			cwd, _ := os.Getwd()
			uiScopes := []string{cfg.ScopeForDir(cwd)}
			if uiScopes[0] == "" {
				uiScopes = cfg.ResolvedScopes()
			}
			svc, cleanup, err := service.Open(cfg, uiScopes)
			if err != nil {
				return err
			}
			defer cleanup()
			uiSrv := web.NewServer(svc.Proto)
			fmt.Fprintf(os.Stderr, "scribe: UI listening on %s\n", addr)
			return http.ListenAndServe(addr, uiSrv) //nolint:gosec // operator-configured address
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8082", "listen address for the web UI")
	return cmd
}

const (
	lexiconDaemonPollInterval = 30 * time.Second

	// slog key constants for lexicon commands — sloglint no-raw-keys.
	logKeyAddr           = "addr"
	logKeySessionTimeout = "session_timeout"
	logKeyLexRoot        = "lex_root"
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


