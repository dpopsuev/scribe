package embed_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dpopsuev/scribe/embed"
)

func TestOllamaFunc_HappyPath(t *testing.T) {
	// Given: Ollama returns a valid embedding vector
	// When: OllamaEmbedFunc is called
	// Then: the vector is returned with no error
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" || r.Method != http.MethodPost {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	fn := embed.OllamaFunc(srv.URL, "nomic-embed-text")
	vec, err := fn(context.Background(), "test text")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(vec))
	}
}

func TestOllamaFunc_Non200(t *testing.T) {
	// Given: Ollama returns HTTP 500
	// When: OllamaEmbedFunc is called
	// Then: error contains the status code
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	fn := embed.OllamaFunc(srv.URL, "nomic-embed-text")
	_, err := fn(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status code in error, got: %v", err)
	}
}

func TestOllamaFunc_EmptyEmbedding(t *testing.T) {
	// Given: Ollama returns an empty embedding array
	// When: OllamaEmbedFunc is called
	// Then: error is returned (not a silent zero vector)
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"embedding":[]}`))
	}))
	defer srv.Close()

	fn := embed.OllamaFunc(srv.URL, "nomic-embed-text")
	_, err := fn(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for empty embedding")
	}
}

func TestOllamaFunc_InvalidJSON(t *testing.T) {
	// Given: Ollama returns malformed JSON
	// When: OllamaEmbedFunc is called
	// Then: decode error is returned
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	fn := embed.OllamaFunc(srv.URL, "nomic-embed-text")
	_, err := fn(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestOllamaFunc_ContextCanceled(t *testing.T) {
	// Given: context is already canceled before the call
	// When: OllamaEmbedFunc is called
	// Then: error is returned immediately without network activity
	t.Parallel()
	fn := embed.OllamaFunc("http://127.0.0.1:1", "nomic-embed-text")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled
	_, err := fn(ctx, "test text")
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}
