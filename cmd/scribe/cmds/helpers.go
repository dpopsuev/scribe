// Package cmds contains scribe CLI command groups.
// Each file in this package owns one logical group of commands and exposes
// a Register(root *cobra.Command) function that adds its commands to the root.
//
// Shared infrastructure (config loading, store construction, logging) lives
// in this file. Commands call the helpers directly without going through main.
package cmds

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/service"
)

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

// MustProto opens a raw Protocol — used by commands that need direct access
// below the service layer (lint, migrate, etc.).
func MustProto() (proto *parchment.Protocol, cleanup func()) {
	cfg := MustConfig()
	s, err := parchment.OpenSQLiteConfig(cfg.SQLiteConfig())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open store: %v\n", err)
		os.Exit(1)
	}
	return parchment.New(s, nil, nil, nil, cfg.ProtocolIDConfig()), func() { _ = s.Close() }
}

// MustService is the single construction path for most CLI commands.
// Uses service.Open so homeScopes and schema loading are identical to the MCP server.
func MustService() (svc *service.Service, cleanup func()) {
	cfg := MustConfig()
	s, cl, err := service.Open(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return s, cl
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
