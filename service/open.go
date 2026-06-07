package service

import (
	"context"
	"fmt"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/embed"
)

// Open is the single construction path for a Service instance.
// Both the CLI (cmd/scribe) and the MCP server (mcp.NewServer) call Open
// so they share identical Protocol configuration, homeScopes, and schema loading.
//
// homeScopes overrides the scopes derived from cfg.ResolvedScopes().
// Pass nil or omit to use the config-derived scopes (correct for CLI commands).
// Pass an explicit slice for the serve command which derives scopes from CWD and flags.
func Open(cfg *config.Config, homeScopes ...[]string) (*Service, func(), error) {
	s, err := parchment.OpenSQLiteConfig(cfg.SQLiteConfig())
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	scopes := cfg.ResolvedScopes()
	if len(homeScopes) > 0 && len(homeScopes[0]) > 0 {
		scopes = homeScopes[0]
	}
	idc := cfg.ProtocolIDConfig()

	// Wire embedding if configured.
	var embedder *embed.Embedder
	if cfg.Embed.Enabled() {
		model := cfg.Embed.Model
		if model == "" {
			model = "nomic-embed-text"
		}
		delay := time.Duration(cfg.Embed.EmbedDelay()) * time.Millisecond
		sweep := time.Duration(cfg.Embed.SweepInterval()) * time.Second
		embedder = embed.New(nil, model, cfg.Embed.URL, delay, sweep) // proto set below
		idc.EmbedFunc = embedder.EmbedFunc()
		idc.EmbedModel = model
	}

	proto := parchment.New(s, nil, scopes, nil, idc)
	svc := New(proto, nil, scopes)

	cleanup := func() { _ = s.Close() }
	if embedder != nil {
		embedder.SetProto(proto) //nolint:errcheck // fluent return ignored here; proto is set
		embedder.Start(context.Background())
		cleanup = func() {
			embedder.Stop()
			_ = s.Close()
		}
	}

	return svc, cleanup, nil
}
