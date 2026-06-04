// Package cmds contains scribe CLI command groups.
// Each file in this package owns one logical group of commands and exposes
// a Register(root *cobra.Command) function that adds its commands to the root.
//
// Shared infrastructure (config loading, store construction, logging) lives
// in this file. Commands call the helpers directly without going through main.
package cmds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/service"
)

const formatJSON = "json"

// ConfigPath and DBPath are set by main via persistent root flags.
// All commands read through MustConfig() which consults these vars.
var (
	ConfigPath string
	DBPath     string
	Version    = "dev"
)

// MustConfig loads the resolved Config, applying DBPath override if set.
// Exits with a message on error — CLI commands cannot proceed without config.
func MustConfig() *config.Config {
	cfg, err := config.Resolve(ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load config: %v\n", err)
		os.Exit(1)
	}
	if DBPath != "" {
		cfg.DB.SQLite.Path = DBPath
	}
	return cfg
}

// MustService is the single construction path for most CLI commands.
// Uses service.Open so homeScopes and schema loading are identical to the MCP server.
// Applies the same CWD-based scope detection as the serve command.
func MustService() (svc *service.Service, cleanup func()) {
	cfg := MustConfig()
	// Mirror serve.go: CWD detection → configured scopes.
	var homeScopes []string
	if cwd, err := os.Getwd(); err == nil {
		if sc := cfg.ScopeForDir(cwd); sc != "" {
			homeScopes = []string{sc}
		}
	}
	s, cl, err := service.Open(cfg, homeScopes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return s, cl
}

// RunOp executes a registered Op by name, marshaling input to JSON first.
// Returns the text output or an error. Commands migrated to the registry
// call this instead of calling svc.Proto directly.
func RunOp(name string, input any) error {
	op := service.Find(name)
	if op == nil {
		return fmt.Errorf("op %q not found in registry", name) //nolint:err113 // internal routing error
	}
	svc, cleanup := MustService()
	defer cleanup()
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	out, err := op.Run(context.Background(), svc, raw)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// MustStore opens a raw SQLiteStore — used by commands that need direct store
// access (migrate-ids, etc.).
func MustStore() *parchment.SQLiteStore {
	cfg := MustConfig()
	s, err := parchment.OpenSQLiteConfig(cfg.SQLiteConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	return s
}

// InitLogger configures the default slog handler from SCRIBE_LOG_LEVEL.
func InitLogger() {
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

// EnvOr returns the value of the environment variable key, or fallback if unset.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// SortArts sorts a slice of artifacts by the given field name.
func SortArts(arts []*parchment.Artifact, field string) {
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

// SessionTimeout returns the MCP session timeout from SCRIBE_SESSION_TIMEOUT
// env var, defaulting to 8 hours.
func SessionTimeout() time.Duration {
	if v := os.Getenv("SCRIBE_SESSION_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("invalid SCRIBE_SESSION_TIMEOUT, using default") //nolint:sloglint // no context available here; gosec: env var value is not injected
	}
	return 8 * time.Hour
}
