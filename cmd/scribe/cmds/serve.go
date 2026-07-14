package cmds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // pprof is intentionally exposed for debug builds
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"syscall"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/embed"
	"github.com/dpopsuev/scribe/mcp"
	"github.com/dpopsuev/scribe/migrations"
	"github.com/dpopsuev/scribe/service"
	"github.com/dpopsuev/scribe/web"
	"github.com/dpopsuev/scribe/workspace"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

const (
	logKeyAddr           = "addr"
	logKeySessionTimeout = "session_timeout"
	logKeyMCPStateless   = "mcp_stateless"

	// slog key constants for serve logging — sloglint no-raw-keys.
	logKeyVersion     = "version"
	logKeyDB          = "db"
	logKeyTransport   = "transport"
	logKeyScopes      = "scopes"
	logKeyDir         = "dir"
	logKeyWorkspace   = "labels"
	logKeyCreated     = "created"
	logKeyError       = "error"
	logKeyPath        = "path"
	logKeyHeap        = "heap"
	logKeyGoroutine   = "goroutine"
	logKeyHeapMB      = "heap_mb"
	logKeyThresholdMB = "threshold_mb"
	logKeyHeapAllocMB = "heap_alloc_mb"
	logKeyHeapSysMB   = "heap_sys_mb"
	logKeyGoroutines  = "goroutines"
	logKeyWarnMB      = "warn_mb"
	logKeyCritMB      = "crit_mb"
	logKeyModel       = "model"
)

func ServeCmd() *cobra.Command {
	var scopes []string
	var transport, addr string
	var enableUI bool
	var uiAddr string
	var webPath string
	var enablePprof bool
	var pprofAddr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio or HTTP)",
		Long: `Start the Scribe MCP server.

  stdio (default): reads/writes JSON-RPC over stdin/stdout.
  http:            starts a Streamable HTTP server on --addr.
  --ui:            also starts a read-only web UI on --ui-addr.
  --web-path PATH: filesystem path to web/ directory containing templates/
                   and static/. Default: "web" (relative to working directory).
                   In the container image, web assets are at /web.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, scopes, transport, addr, uiAddr, webPath, pprofAddr, enableUI, enablePprof)
		},
	}
	cmd.Flags().StringSliceVar(&scopes, "scope", nil, "home scopes (repeatable)")
	cmd.Flags().StringVar(&transport, "transport", "stdio", "transport type: stdio, http")
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address for http transport")
	cmd.Flags().BoolVar(&enableUI, "ui", false, "start the read-only web UI alongside the MCP server")
	cmd.Flags().StringVar(&uiAddr, "ui-addr", ":8082", "listen address for the web UI")
	cmd.Flags().StringVar(&webPath, "web-path", "web", "filesystem path to web/ directory (templates + static)")
	cmd.Flags().BoolVar(&enablePprof, "pprof", false, "enable pprof profiling endpoint (localhost only)")
	cmd.Flags().StringVar(&pprofAddr, "pprof-addr", "127.0.0.1:6060", "listen address for pprof")
	return cmd
}

func runServe(cmd *cobra.Command, scopes []string, transport, addr, uiAddr, webPath, pprofAddr string, enableUI, enablePprof bool) error {
	InitLogger()
	cfg := MustConfig()
	ctx := cmd.Context()

	slog.InfoContext(ctx, "starting scribe",
		slog.String(logKeyVersion, Version),
		slog.String(logKeyDB, cfg.DBPath()),
	)

	// Resolve home context: flag > workspace detection > config-derived.
	homeScopes := scopes
	if len(homeScopes) == 0 {
		cwd, _ := os.Getwd()
		if sc := cfg.ScopeForDir(cwd); sc != "" {
			homeScopes = []string{sc}
		} else {
			homeScopes = cfg.ResolvedScopes()
		}
	}
	// Detect workspace context labels for stdio transport.
	// HTTP transport is multi-tenant; context comes from each client's initialize params.
	var stdioWorkspaceLabels []string
	if transport == "stdio" || !cmd.Flags().Changed("transport") {
		cwd, _ := os.Getwd()
		inputs := workspace.WorkspaceInputs{CWD: cwd}
		stdioWorkspaceLabels = workspace.Detect(inputs, workspace.DefaultDetectors())
		if len(stdioWorkspaceLabels) > 0 {
			slog.InfoContext(ctx, "workspace detected", slog.Any(logKeyWorkspace, stdioWorkspaceLabels))
		}
	}

	// Build optional embedding function — only when configured.
	var embedFunc parchment.EmbeddingFunc
	embedModel := cfg.Embed.Model
	if cfg.Embed.Enabled() {
		if embedModel == "" {
			embedModel = "nomic-embed-text"
		}
		embedFunc = embed.OllamaFunc(cfg.Embed.URL, embedModel)
	}

	svc, cleanup, err := service.Open(cfg, embedFunc, embedModel, homeScopes)
	if err != nil {
		slog.ErrorContext(ctx, "failed to open store", slog.String(logKeyDB, cfg.DBPath()), slog.Any(logKeyError, err))
		return err
	}
	defer cleanup()
	svc.Version = Version
	svc.RecordSession = cfg.RecordSession == nil || *cfg.RecordSession

	if err := migrations.RunPending(ctx, svc.Proto, false); err != nil {
		slog.ErrorContext(ctx, "migration failed", slog.Any(logKeyError, err))
		return err
	}

	// Start embedder sweep only when embedding is configured.
	if embedFunc != nil {
		sweepInterval := time.Duration(cfg.Embed.SweepInterval()) * time.Second
		embedder := embed.New(ctx, svc.Proto, embedModel, sweepInterval, cfg.Embed.Workers(), embedFunc)
		// Extraction sidecar: reuses Ollama, runs in the same sweep after embedding.
		if extractModel := os.Getenv("SCRIBE_EXTRACT_MODEL"); extractModel != "" {
			extractURL := cfg.Embed.URL
			embedder.SetExtractFunc(embed.OllamaExtractFunc(extractURL, extractModel))
			slog.InfoContext(ctx, "extraction sidecar enabled", slog.String(logKeyModel, extractModel))
		}
		defer embedder.Stop()
	}

	freshnessTicker := service.NewFreshnessTicker(svc.Proto, 1*time.Hour)
	defer freshnessTicker.Stop()

	if interval := service.LibrarianIntervalFromEnv(); interval > 0 {
		libTicker := service.NewLibrarianTicker(svc, interval, service.LibrarianPassOpts{})
		defer libTicker.Stop()
		slog.InfoContext(ctx, "librarian ticker enabled", slog.String("interval", interval.String())) //nolint:sloglint // duration display
	}

	if len(cfg.ScopeConfigs) > 0 {
		mat := service.NewMaterializer(svc, cfg.ScopeConfigs, cfg.Materialize.Interval())
		defer mat.Stop()
	}

	runSeedDir(ctx, svc.Proto, cfg)
	applyScopeLabels(svc.Proto.Store(), cfg)

	activeTransport := flagOrDefault(cmd, "transport", transport, cfg.Transport)
	activeAddr := flagOrDefault(cmd, "addr", addr, cfg.Addr)

	srv, _ := mcp.NewServer(svc, nil, Version, stdioWorkspaceLabels...)

	// Per-request factory for HTTP transport: resolves workspace from URL param or
	// X-Workspace-* headers (set by HTTP MCP clients that cannot send init meta).
	// The store is shared; Protocol and Service are created per session.
	store := svc.Proto.Store()
	idc := cfg.ProtocolIDConfig()
	srvFactory := func(r *http.Request) *sdkmcp.Server {
		requestScopes := homeScopes
		if ws := r.URL.Query().Get("workspace"); ws != "" {
			requestScopes = cfg.WorkspaceScopesFor(ws)
		}
		wLabels := workspaceLabelsFromHeaders(r)
		perConnSrv, _ := mcp.NewServerFromStore(store, requestScopes, idc, Version, wLabels...)
		return perConnSrv
	}

	slog.InfoContext(ctx, "server configured",
		slog.String(logKeyTransport, activeTransport),
		slog.Any(logKeyScopes, homeScopes),
	)

	if enableUI {
		uiSrv := web.NewServer(svc.Proto, Version, webPath)
		go func() {
			slog.InfoContext(ctx, "UI listening", slog.String(logKeyAddr, uiAddr))
			if err := http.ListenAndServe(uiAddr, uiSrv); err != nil { //nolint:gosec // operator-configured address
				slog.ErrorContext(ctx, "UI server error", slog.Any(logKeyError, err))
			}
		}()
	}

	otelShutdown, err := service.InitTracer(ctx, Version)
	if err != nil {
		slog.WarnContext(ctx, "otel: tracer init failed", slog.Any(logKeyError, err))
	}
	defer func() { _ = otelShutdown(context.Background()) }()

	watchdogCtx, watchdogCancel := context.WithCancel(context.Background())
	defer watchdogCancel()
	startMemoryWatchdog(watchdogCtx)

	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if activeTransport == "http" {
		return runHTTP(sigCtx, ctx, cmd, srvFactory, store, activeAddr, pprofAddr, enablePprof)
	}
	slog.InfoContext(ctx, "serving via stdio")
	return srv.Run(sigCtx, &sdkmcp.StdioTransport{})
}

func runHTTP(sigCtx, logCtx context.Context, cmd *cobra.Command, srvFactory func(*http.Request) *sdkmcp.Server, store parchment.Store, addr, pprofAddr string, enablePprof bool) error {
	handler := sdkmcp.NewStreamableHTTPHandler(
		srvFactory,
		&sdkmcp.StreamableHTTPOptions{
			SessionTimeout: SessionTimeout(),
			Stateless:      MCPStateless(),
		},
	)

	if enablePprof {
		go func() {
			slog.InfoContext(logCtx, "pprof listening", slog.String(logKeyAddr, pprofAddr))
			if err := http.ListenAndServe(pprofAddr, nil); err != nil { //nolint:gosec // operator-configured address
				slog.ErrorContext(logCtx, "pprof server error", slog.Any(logKeyError, err))
			}
		}()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":%q}`, Version)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var dbSize int64
		if sizer, ok := store.(parchment.DBSizer); ok {
			dbSize, _ = sizer.DBSizeBytes(r.Context())
		}
		fmt.Fprintf(w, `{"status":"ok","version":%q,"db_bytes":%d}`, Version, dbSize)
	})
	mux.Handle("/", handler)

	httpHandler := requestLogMiddleware(mux)
	if token := os.Getenv("SCRIBE_AUTH_TOKEN"); token != "" {
		httpHandler = authMiddleware(token, httpHandler)
		slog.InfoContext(logCtx, "auth enabled via SCRIBE_AUTH_TOKEN")
	}

	httpSrv := &http.Server{Addr: addr, Handler: httpHandler, ReadHeaderTimeout: 10 * time.Second} //nolint:mnd // standard timeout
	go func() {
		<-sigCtx.Done()
		slog.InfoContext(logCtx, "shutdown signal received, draining connections")
		if heapPath := dumpHeapProfile(logCtx, "shutdown"); heapPath != "" {
			slog.InfoContext(logCtx, "shutdown heap dump captured", slog.String(logKeyPath, heapPath))
		}
		if goroutinePath := dumpGoroutineProfile(logCtx, "shutdown"); goroutinePath != "" {
			slog.InfoContext(logCtx, "shutdown goroutine dump captured", slog.String(logKeyPath, goroutinePath))
		}

		shutCtx, cancel := context.WithTimeout(logCtx, 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
	}()

	slog.InfoContext(cmd.Context(), "listening",
		slog.String(logKeyAddr, addr),
		slog.Duration(logKeySessionTimeout, SessionTimeout()),
		slog.Bool(logKeyMCPStateless, MCPStateless()),
	)
	if MCPStateless() {
		slog.InfoContext(logCtx, "mcp http is stateless — stale Mcp-Session-Id after restart is accepted; set SCRIBE_MCP_STATELESS=0 for sticky sessions")
	} else {
		slog.InfoContext(logCtx, "mcp http is sticky — after restart clients must reconnect (session not found → re-initialize); prefer stdio for local Cursor")
	}
	if err := httpSrv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.InfoContext(logCtx, "server stopped, closing store")
	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)
		if r.URL.Path == "/healthz" {
			return
		}
		slog.InfoContext(r.Context(), "http",
			slog.String("method", r.Method), //nolint:sloglint // HTTP-specific key
			slog.String(logKeyPath, r.URL.Path),
			slog.Int("status", rec.status),                           //nolint:sloglint // "status" is HTTP-specific, no shared constant
			slog.String("dur", dur.Round(time.Microsecond).String()), //nolint:sloglint // "dur" is local timing
		)
	})
}

func authMiddleware(token string, next http.Handler) http.Handler {
	bearer := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != bearer {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// workspaceLabelsFromHeaders derives workspace context labels from X-Workspace-*
// HTTP headers. Clients that cannot send MCP init _meta (e.g. OpenCode remote
// MCP) set these headers via their static config instead.
func workspaceLabelsFromHeaders(r *http.Request) []string {
	cwd := r.Header.Get("X-Workspace-CWD")
	if cwd == "" {
		return nil
	}
	inputs := workspace.WorkspaceInputs{
		CWD:       cwd,
		GitRemote: r.Header.Get("X-Workspace-Git-Remote"),
	}
	return workspace.Detect(inputs, workspace.DefaultDetectors())
}

// runSeedDir seeds templates on first run. Skips if templates already exist.
func runSeedDir(ctx context.Context, proto *parchment.Protocol, cfg *config.Config) {
	if cfg.SeedDir == "" {
		return
	}
	templates, _ := proto.ListArtifacts(context.Background(), parchment.ListInput{
		Labels: []string{parchment.LabelPrefixKind + "support.template"},
	})
	if len(templates) > 0 {
		return
	}
	result, err := proto.Seed(context.Background(), cfg.SeedDir)
	if err != nil {
		slog.WarnContext(ctx, "auto-seed failed", slog.String(logKeyDir, cfg.SeedDir), slog.Any(logKeyError, err))
	} else if len(result.Created) > 0 {
		slog.InfoContext(ctx, "auto-seed completed", slog.String(logKeyDir, cfg.SeedDir), slog.Int(logKeyCreated, len(result.Created)))
	}
}

// applyScopeLabels pushes scope labels from config into the store.
func applyScopeLabels(store parchment.Store, cfg *config.Config) {
	for name, sc := range cfg.ScopeConfigs {
		if len(sc.Labels) > 0 {
			_ = store.SetScopeLabels(context.Background(), name, sc.Labels)
		}
	}
}

// flagOrDefault returns flagValue if the named flag was explicitly set, otherwise defaultVal.
func flagOrDefault(cmd *cobra.Command, name, flagValue, defaultVal string) string {
	if cmd.Flags().Changed(name) {
		return flagValue
	}
	return defaultVal
}

func ToolsCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List all available MCP tools with descriptions",
		Long:  "Print a table of every MCP tool Scribe exposes, with name, description, keywords, and categories. Useful for discovering what the agent can do without reading the README.",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := mcp.ToolRegistry()

			var tools []mcp.ToolMeta
			if category != "" {
				tools = reg.ByCategory(category)
			} else {
				tools = reg.List()
			}

			if len(tools) == 0 {
				fmt.Println("No tools found.")
				return nil
			}

			nameWidth, descWidth, keywordsWidth := 4, 11, 8
			for _, t := range tools {
				if len(t.Name) > nameWidth {
					nameWidth = len(t.Name)
				}
				if len(t.Description) > descWidth {
					descWidth = len(t.Description)
				}
				kw := strings.Join(t.Keywords, ", ")
				if len(kw) > keywordsWidth {
					keywordsWidth = len(kw)
				}
			}

			fmtStr := fmt.Sprintf("%%-%ds  %%-%ds  %%-%ds  %%s\n", nameWidth, descWidth, keywordsWidth)
			fmt.Printf(fmtStr, "NAME", "DESCRIPTION", "KEYWORDS", "CATEGORIES")
			fmt.Printf(fmtStr,
				strings.Repeat("-", nameWidth),
				strings.Repeat("-", descWidth),
				strings.Repeat("-", keywordsWidth),
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

func UICmd() *cobra.Command {
	var addr, webPath string
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Start the read-only web UI (standalone, no MCP server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := MustConfig()
			cwd, _ := os.Getwd()
			uiScopes := []string{cfg.ScopeForDir(cwd)}
			if uiScopes[0] == "" {
				uiScopes = cfg.ResolvedScopes()
			}
			svc, cleanup, err := service.Open(cfg, nil, "", uiScopes)
			if err != nil {
				return err
			}
			defer cleanup()
			uiSrv := web.NewServer(svc.Proto, Version, webPath)
			fmt.Fprintf(os.Stderr, "scribe: UI listening on %s\n", addr)
			return http.ListenAndServe(addr, uiSrv) //nolint:gosec // operator-configured address
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8082", "listen address for the web UI")
	cmd.Flags().StringVar(&webPath, "web-path", "web", "filesystem path to web/ directory")
	return cmd
}

// crashDir returns the crash dump directory, creating it if needed.
func crashDir() string {
	dir := EnvOr("SCRIBE_CRASH_DIR", filepath.Join(EnvOr("SCRIBE_ROOT", filepath.Join(os.Getenv("HOME"), ".scribe")), "crash"))
	_ = os.MkdirAll(dir, 0o750) //nolint:gosec // crash dir is created with restrictive perms
	return dir
}

// dumpHeapProfile writes a heap profile to the crash directory.
func dumpHeapProfile(ctx context.Context, label string) string {
	path := filepath.Join(crashDir(), fmt.Sprintf("%s-%s.pb.gz", label, time.Now().Format("20060102-150405")))
	f, err := os.Create(path) //nolint:gosec // path is constructed from trusted EnvOr values
	if err != nil {
		slog.ErrorContext(ctx, "crash dump: create file failed", slog.String(logKeyPath, path), slog.Any(logKeyError, err))
		return ""
	}
	defer func() { _ = f.Close() }()
	if err := pprof.WriteHeapProfile(f); err != nil {
		slog.ErrorContext(ctx, "crash dump: write heap profile failed", slog.Any(logKeyError, err))
		return ""
	}
	return path
}

// dumpGoroutineProfile writes a goroutine profile to the crash directory.
func dumpGoroutineProfile(ctx context.Context, label string) string {
	path := filepath.Join(crashDir(), fmt.Sprintf("%s-goroutine-%s.txt", label, time.Now().Format("20060102-150405")))
	f, err := os.Create(path) //nolint:gosec // path is constructed from trusted EnvOr values
	if err != nil {
		slog.ErrorContext(ctx, "crash dump: create goroutine file failed", slog.Any(logKeyError, err))
		return ""
	}
	defer func() { _ = f.Close() }()
	if p := pprof.Lookup("goroutine"); p != nil {
		_ = p.WriteTo(f, 1)
	}
	return path
}

// startMemoryWatchdog launches a background goroutine that samples memory every 60s.
// On threshold breach, it auto-captures heap profiles to the crash directory.
func startMemoryWatchdog(ctx context.Context) {
	const (
		defaultWarnMB = 512
		defaultCritMB = 2048
	)
	warnMB := defaultWarnMB
	critMB := defaultCritMB
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

	slog.InfoContext(ctx, "memory watchdog started", slog.Int(logKeyWarnMB, warnMB), slog.Int(logKeyCritMB, critMB))

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
				heapMB := int(m.HeapAlloc / (1024 * 1024)) //nolint:gosec // values are MB-scale, no overflow risk
				sysMB := int(m.HeapSys / (1024 * 1024))    //nolint:gosec // values are MB-scale, no overflow risk
				goroutines := runtime.NumGoroutine()

				slog.DebugContext(ctx, "watchdog sample",
					slog.Int(logKeyHeapAllocMB, heapMB),
					slog.Int(logKeyHeapSysMB, sysMB),
					slog.Int(logKeyGoroutines, goroutines),
				)

				if heapMB >= critMB && !critFired {
					critFired = true
					slog.ErrorContext(ctx, "watchdog CRITICAL: heap exceeds critical threshold",
						slog.Int(logKeyHeapMB, heapMB), slog.Int(logKeyThresholdMB, critMB))
					heapPath := dumpHeapProfile(ctx, "critical")
					goroutinePath := dumpGoroutineProfile(ctx, "critical")
					slog.ErrorContext(ctx, "crash dumps captured",
						slog.String(logKeyHeap, heapPath), slog.String(logKeyGoroutine, goroutinePath))
				} else if heapMB >= warnMB && !warnFired {
					warnFired = true
					slog.WarnContext(ctx, "watchdog WARNING: heap exceeds warn threshold",
						slog.Int(logKeyHeapMB, heapMB), slog.Int(logKeyThresholdMB, warnMB))
					heapPath := dumpHeapProfile(ctx, "warning")
					slog.WarnContext(ctx, "heap dump captured", slog.String(logKeyPath, heapPath))
				}

				if heapMB < warnMB {
					warnFired = false
					critFired = false
				}
			}
		}
	}()
}
