package service

import (
	"fmt"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
)

// Open is the single construction path for a Service instance.
// embedFunc and embedModel are optional — pass nil/empty for CLI commands
// that do not use semantic search. The serve command constructs these via
// embed.OllamaFunc and passes them in; the embedder sweep is managed there.
func Open(cfg *config.Config, embedFunc parchment.EmbeddingFunc, embedModel string, homeScopes ...[]string) (*Service, func(), error) {
	backend, err := parchment.NewSQLiteBackend(cfg.SQLiteConfig())
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	scopes := cfg.ResolvedScopes()
	if len(homeScopes) > 0 && len(homeScopes[0]) > 0 {
		scopes = homeScopes[0]
	}
	idc := cfg.ProtocolIDConfig()
	idc.EmbedFunc = embedFunc
	idc.EmbedModel = embedModel

	proto := parchment.New(backend.Store(), nil, scopes, nil, idc)
	svc := New(proto, backend.Snapshotter(), scopes)
	return svc, func() { _ = backend.Close() }, nil
}
