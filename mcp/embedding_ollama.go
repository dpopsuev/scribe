package mcp

import (
	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

// NewOllamaEmbeddingFunc delegates to service.NewOllamaEmbeddingFunc.
// Kept for backward compat with existing integration tests.
func NewOllamaEmbeddingFunc(model, baseURL string) (parchment.EmbeddingFunc, error) {
	return service.NewOllamaEmbeddingFunc(model, baseURL)
}
