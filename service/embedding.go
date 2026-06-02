package service

// embedding_ollama.go — Ollama EmbeddingFunc adapter.
//
// Ollama runs locally — no API key, no data leaves the machine.
// Pull the model once: ollama pull nomic-embed-text
//
// Integration tests use t.Skipf if Ollama is not running:
//   fn, err := NewOllamaEmbeddingFunc("nomic-embed-text", "")
//   if err != nil { t.Skipf("Ollama not available: %v", err) }

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const defaultOllamaBase = "http://localhost:11434"

// NewOllamaEmbeddingFunc returns an EmbeddingFunc that calls the Ollama
// embeddings API. baseURL defaults to http://localhost:11434.
//
// Probe: the function makes one test call on creation to verify Ollama is
// reachable. Returns an error immediately if not — callers should t.Skipf.
func NewOllamaEmbeddingFunc(model, baseURL string) (parchment.EmbeddingFunc, error) {
	if baseURL == "" {
		baseURL = defaultOllamaBase
	}
	fn := ollamaEmbeddingFunc(model, baseURL)

	// Probe: verify Ollama is reachable before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := fn(ctx, "probe"); err != nil {
		return nil, fmt.Errorf("ollama not available at %s: %w", baseURL, err)
	}
	return fn, nil
}

// ollamaEmbeddingFunc builds the actual closure without the probe.
// Exported via NewOllamaEmbeddingFunc to enforce the availability check.
func ollamaEmbeddingFunc(model, baseURL string) parchment.EmbeddingFunc {
	client := &http.Client{Timeout: 30 * time.Second}
	endpoint := baseURL + "/api/embeddings"

	return func(ctx context.Context, text string) ([]float32, error) {
		body, err := json.Marshal(map[string]string{
			"model":  model,
			"prompt": text,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body)) //nolint:gosec // endpoint is operator-supplied via SCRIBE_EMBED_URL
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req) //nolint:gosec // operator-controlled endpoint
		if err != nil {
			return nil, fmt.Errorf("ollama call: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable here

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("ollama returned %d", resp.StatusCode) //nolint:err113 // HTTP status code is sufficient context
		}

		var result struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if len(result.Embedding) == 0 {
			return nil, fmt.Errorf("ollama returned empty embedding for model %q", model) //nolint:err113 // model name in message is the context
		}
		return result.Embedding, nil
	}
}
