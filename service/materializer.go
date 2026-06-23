package service

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/dpopsuev/scribe/config"
)

const (
	logKeyMaterialize = "materialize"
	logKeyScope       = "scope"
	logKeyInterval    = "interval"
	logKeyChanged     = "changed"
	logKeySkipped     = "skipped"
	logKeySweepDur    = "sweep_dur"
)

// Materializer runs periodic materialization of configured scope repos
// into the artifact graph. It sweeps scope configs for paths that contain
// git repos and runs RepoIngest for each.
type Materializer struct {
	svc      *Service
	sink     *ScribeSink
	scopes   map[string]config.ScopeConfig
	interval time.Duration
	stop     chan struct{}
}

// NewMaterializer creates and starts a background materializer.
// Interval of 0 runs a single sweep on start with no ticker.
func NewMaterializer(svc *Service, scopes map[string]config.ScopeConfig, interval time.Duration) *Materializer {
	m := &Materializer{
		svc:      svc,
		sink:     NewScribeSink(svc.Proto.Store()),
		scopes:   scopes,
		interval: interval,
		stop:     make(chan struct{}),
	}
	go m.run()
	return m
}

// Stop halts the background materializer.
func (m *Materializer) Stop() {
	close(m.stop)
}

// Sink returns the ScribeSink for push-based materialization by external tools.
func (m *Materializer) Sink() *ScribeSink {
	return m.sink
}

func (m *Materializer) run() {
	m.sweep()

	if m.interval <= 0 {
		return
	}

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

func (m *Materializer) sweep() {
	ctx := context.Background()
	start := time.Now()

	repos := m.gitRepos()
	if len(repos) == 0 {
		return
	}

	changed, skipped := 0, 0
	for scope, repoPath := range repos {
		result, err := m.svc.RepoIngest(ctx, repoPath)
		if err != nil {
			slog.WarnContext(ctx, "materializer: ingest failed",
				slog.String(logKeyScope, scope),
				slog.Any("error", err)) //nolint:sloglint // one-off error key
			continue
		}
		if result.Artifacts > 0 {
			changed++
			slog.InfoContext(ctx, "materializer: ingested",
				slog.String(logKeyScope, scope),
				slog.Int("artifacts", result.Artifacts), //nolint:sloglint // domain-specific key
				slog.Int("edges", result.Edges))         //nolint:sloglint // domain-specific key
		} else {
			skipped++
		}
	}

	dur := time.Since(start)
	if changed > 0 || slog.Default().Enabled(ctx, slog.LevelDebug) {
		slog.InfoContext(ctx, "materializer: sweep complete",
			slog.Int(logKeyChanged, changed),
			slog.Int(logKeySkipped, skipped),
			slog.String(logKeySweepDur, dur.Round(time.Millisecond).String()))
	}
}

// gitRepos returns scope→repoPath for scopes whose path is a git repo.
func (m *Materializer) gitRepos() map[string]string {
	repos := make(map[string]string)
	for name, sc := range m.scopes {
		if sc.Path == "" {
			continue
		}
		path := expandScopePath(sc.Path)
		gitDir := filepath.Join(path, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			repos[name] = path
		}
	}
	return repos
}

func expandScopePath(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if len(path) > 1 && path[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
