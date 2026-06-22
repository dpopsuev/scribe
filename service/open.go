package service

import (
	"errors"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
)

// ErrUnsupportedBackend is returned when the configured backend type is not recognized.
var ErrUnsupportedBackend = errors.New("unsupported backend")

func openBackend(cfg *config.Config) (parchment.Backend, error) {
	switch cfg.DB.Backend {
	case "", "sqlite":
		return parchment.NewSQLiteBackend(cfg.SQLiteConfig())
	case "libsql":
		return parchment.NewLibSQLBackend(cfg.DB.LibSQL)
	case "turso":
		return parchment.NewTursoBackend(cfg.DB.Turso)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedBackend, cfg.DB.Backend)
	}
}

// Open is the single construction path for a Service instance.
// embedFunc and embedModel are optional — pass nil/empty for CLI commands
// that do not use semantic search. The serve command constructs these via
// embed.OllamaFunc and passes them in; the embedder sweep is managed there.
func Open(cfg *config.Config, embedFunc parchment.EmbeddingFunc, embedModel string, homeScopes ...[]string) (*Service, func(), error) {
	backend, err := openBackend(cfg)
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
	svc.EmbedModel = embedModel
	return svc, func() { _ = backend.Close() }, nil
}
