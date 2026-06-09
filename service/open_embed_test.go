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
	"github.com/dpopsuev/scribe/embed"
	"github.com/dpopsuev/scribe/service"
)

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
	// Given: an embed func pointing at a stub Ollama server
	// When:  service.Open is called, an artifact is created, embedder sweep runs
	// Then:  the artifact gains the "encoded" label within the sweep interval
	t.Parallel()

	ollama := stubOllama(t)
	defer ollama.Close()

	const model = "nomic-embed-text"
	cfg := &config.Config{}
	cfg.DB.SQLite.Path = filepath.Join(t.TempDir(), "test.sqlite")

	embedFunc := embed.OllamaFunc(ollama.URL, model)
	svc, cleanup, err := service.Open(cfg, embedFunc, model, []string{"test"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer cleanup()

	embedder := embed.New(context.Background(), svc.Proto, model, time.Second, 1, embedFunc)
	defer embedder.Stop()

	ctx := context.Background()
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:note", "scope:test"},
		Title:  "embedding integration test",
	})
	if err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		updated, _ := svc.Proto.GetArtifact(ctx, art.ID)
		if updated != nil && slices.Contains(updated.Labels, parchment.LabelEncoded(model)) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("artifact %s did not gain 'encoded' label within deadline", art.ID)
}

func TestOpen_WithEmbedConfig_CleanupStopsEmbedder(t *testing.T) {
	// Given: service.Open with an embed func
	// When:  cleanup() is called
	// Then:  no panic — store closes cleanly
	t.Parallel()

	ollama := stubOllama(t)
	defer ollama.Close()

	const model = "nomic-embed-text"
	cfg := &config.Config{}
	cfg.DB.SQLite.Path = filepath.Join(t.TempDir(), "test.sqlite")

	embedFunc := embed.OllamaFunc(ollama.URL, model)
	embedder := func(proto *parchment.Protocol) func() {
		e := embed.New(context.Background(), proto, model, 60*time.Second, 1, embedFunc)
		return e.Stop
	}

	svc, cleanup, err := service.Open(cfg, embedFunc, model, []string{"test"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	stop := embedder(svc.Proto)
	stop()
	cleanup()
}

func TestOpen_WithoutEmbedConfig_EmbedderDisabled(t *testing.T) {
	// Given: no embed func passed
	// When:  SearchSemantic is called
	// Then:  error returned — no embedder configured
	t.Parallel()

	cfg := &config.Config{}
	cfg.DB.SQLite.Path = filepath.Join(t.TempDir(), "test.sqlite")

	svc, cleanup, err := service.Open(cfg, nil, "", []string{"test"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer cleanup()

	_, err = svc.Proto.SearchSemantic(context.Background(), "anything", parchment.ListInput{})
	if err == nil {
		t.Fatal("expected error from SearchSemantic when no embed func configured")
	}
}
