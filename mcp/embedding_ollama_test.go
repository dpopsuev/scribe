package mcp_test

// embedding_ollama_test.go — Ollama integration tests.
//
// These tests require a running Ollama instance with nomic-embed-text pulled.
// They are SKIPPED automatically when Ollama is unavailable — safe to run in CI.
//
//   ollama pull nomic-embed-text
//   go test -run TestOllama ./mcp/
//
// Test scope: NewOllamaEmbeddingFunc IS what we're testing here (integration).
// The contract suite (embeddingFuncContract) verifies invariants once real
// vectors are available.

import (
	"context"
	"math"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

// embeddingFuncContractPublic runs invariant checks on any EmbeddingFunc.
func embeddingFuncContractPublic(t *testing.T, fn parchment.EmbeddingFunc) {
	t.Helper()
	ctx := context.Background()

	t.Run("non_empty", func(t *testing.T) {
		v, err := fn(ctx, "hello world")
		if err != nil {
			t.Fatalf("EmbeddingFunc: %v", err)
		}
		if len(v) == 0 {
			t.Error("must return non-empty vector")
		}
	})
	t.Run("consistent_dims", func(t *testing.T) {
		v1, _ := fn(ctx, "first text")
		v2, _ := fn(ctx, "second text with more words")
		if len(v1) != len(v2) {
			t.Errorf("inconsistent dims: %d != %d", len(v1), len(v2))
		}
	})
	t.Run("approximately_normalized", func(t *testing.T) {
		v, _ := fn(ctx, "normalize test")
		var sum float64
		for _, x := range v {
			sum += float64(x) * float64(x)
		}
		mag := math.Sqrt(sum)
		if math.Abs(mag-1.0) > 0.1 {
			t.Errorf("magnitude=%.4f, want ~1.0", mag)
		}
	})
}

func TestOllama_Contract(t *testing.T) {
	fn, err := scribemcp.NewOllamaEmbeddingFunc("nomic-embed-text", "")
	if err != nil {
		t.Skipf("Ollama not available: %v", err)
	}
	embeddingFuncContractPublic(t, fn)
}

func TestOllama_SemanticallySimilarTextsCloser(t *testing.T) {
	fn, err := scribemcp.NewOllamaEmbeddingFunc("nomic-embed-text", "")
	if err != nil {
		t.Skipf("Ollama not available: %v", err)
	}

	ctx := testCtx(t)

	vConformance1, _ := fn(ctx, "template conformance check fires on promote not create")
	vConformance2, _ := fn(ctx, "template validation deferred until artifact status changes")
	vUnrelated, _ := fn(ctx, "ptp clock synchronization holdover test methodology")

	simRelated := parchment.CosineSimilarity(vConformance1, vConformance2)
	simUnrelated := parchment.CosineSimilarity(vConformance1, vUnrelated)

	if simRelated <= simUnrelated {
		t.Errorf("related texts (%.3f) must be closer than unrelated (%.3f)",
			simRelated, simUnrelated)
	}
	t.Logf("related=%.3f unrelated=%.3f gap=%.3f", simRelated, simUnrelated, simRelated-simUnrelated)
}

func TestOllama_CTF_BeatsFTS(t *testing.T) {
	// This is the System B CTF run — same 10 challenges, real embeddings.
	// Compare captures and mean TTF against System A (7/10, TTF=1.9).
	t.Skip("CTF with real embeddings — run manually: go test -run TestOllama_CTF_BeatsFTS -v ./mcp/")
}
