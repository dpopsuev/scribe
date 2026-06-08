package service_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/config"
	"github.com/dpopsuev/scribe/service"
)

// stubOllama returns an httptest.Server that responds to POST /api/embeddings
// with a fixed 3-dimension embedding vector.
func stubOllama(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
}

func TestOpen_WithEmbedConfig_StartsEmbedder(t *testing.T) {
	// Given: embed.url points at a stub Ollama server
	// When: service.Open is called and an artifact is created
	// Then: the artifact gains the "encoded" label within the sweep interval
	t.Parallel()

	ollama := stubOllama(t)
	defer ollama.Close()

	cfg := &config.Config{}
	cfg.DB.SQLite.Path = filepath.Join(t.TempDir(), "test.sqlite")
	cfg.Embed.URL = ollama.URL
	cfg.Embed.Model = "nomic-embed-text"
	cfg.Embed.SweepIntervalSec = 1

	svc, cleanup, err := service.Open(cfg, []string{"test"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:note"}, Title: "embedding integration test", Scope: "test"})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	// Wait up to 3 sweep cycles for the "encoded" label to appear.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
		if updated != nil && slices.Contains(updated.Labels, parchment.LabelEncoded("nomic-embed-text")) {
			return // pass
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("artifact %s did not gain 'encoded' label within deadline", art.ID)
}

func TestOpen_WithEmbedConfig_CleanupStopsEmbedder(t *testing.T) {
	// Given: service.Open with embed config
	// When: cleanup() is called
	// Then: no panic — the embedder goroutine exits cleanly
	t.Parallel()

	ollama := stubOllama(t)
	defer ollama.Close()

	cfg := &config.Config{}
	cfg.DB.SQLite.Path = filepath.Join(t.TempDir(), "test.sqlite")
	cfg.Embed.URL = ollama.URL
	cfg.Embed.SweepIntervalSec = 60

	_, cleanup, err := service.Open(cfg, []string{"test"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cleanup() // must not panic or block
}

func TestOpen_WithoutEmbedConfig_EmbedderDisabled(t *testing.T) {
	// Given: no embed.url in config
	// When: service.Open is called and semantic search is attempted
	// Then: error is returned — embedder is not running
	t.Parallel()

	cfg := &config.Config{}
	cfg.DB.SQLite.Path = filepath.Join(t.TempDir(), "test.sqlite")
	// cfg.Embed.URL intentionally empty

	svc, cleanup, err := service.Open(cfg, []string{"test"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	_, err = svc.Proto.SearchSemantic(ctx, "anything", parchment.ListInput{})
	if err == nil {
		t.Fatal("expected error from SearchSemantic when embedder is not configured")
	}
}
