// Package embed provides a background embedding worker that keeps artifact
// vectors fresh in parchment without blocking the write path.
//
// Design:
//   - Writes enqueue artifact IDs on a buffered channel (non-blocking drop if full).
//   - A single goroutine drains the channel, calls the embed endpoint, stores the
//     vector, and adds the "encoded" label. A polite sleep between calls keeps CPU load low.
//   - A periodic sweep finds artifacts missing the "encoded" label and re-queues them,
//     recovering any IDs that were dropped when the channel was full.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const channelCap = 256

var errEmptyEmbedding = fmt.Errorf("ollama returned empty embedding") //nolint:err113 // package-level sentinel

// Embedder runs the background embedding loop.
type Embedder struct {
	proto     *parchment.Protocol
	model     string
	delay     time.Duration
	sweepDur  time.Duration
	queue     chan string
	stop      chan struct{}
	embedFunc parchment.EmbeddingFunc
}

// OllamaEmbedFunc returns a parchment.EmbeddingFunc that calls the Ollama
// /api/embeddings endpoint. It has no dependency on the Embedder or Protocol
// and can be constructed before the Protocol is built.
func OllamaEmbedFunc(ollamaURL, model string) parchment.EmbeddingFunc {
	client := &http.Client{Timeout: 30 * time.Second}
	base := strings.TrimRight(ollamaURL, "/")
	return func(ctx context.Context, text string) ([]float32, error) {
		body, _ := json.Marshal(map[string]string{"model": model, "prompt": text})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close() //nolint:errcheck // deferred close on read-only response body
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			return nil, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, raw) //nolint:err113 // status+body are runtime values
		}
		var result struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode: %w", err)
		}
		if len(result.Embedding) == 0 {
			return nil, errEmptyEmbedding
		}
		return result.Embedding, nil
	}
}

// New constructs an Embedder and starts the background goroutine immediately.
// proto must be non-nil. embedFunc overrides the embed call (nil = use the
// Protocol's own EmbedFunc, which was wired from OllamaEmbedFunc at construction).
func New(ctx context.Context, proto *parchment.Protocol, model string, delay, sweepInterval time.Duration, embedFunc parchment.EmbeddingFunc) *Embedder {
	e := &Embedder{
		proto:     proto,
		model:     model,
		delay:     delay,
		sweepDur:  sweepInterval,
		queue:     make(chan string, channelCap),
		stop:      make(chan struct{}),
		embedFunc: embedFunc,
	}
	go e.run(ctx)
	return e
}

// Enqueue adds an artifact ID to the embedding queue. Non-blocking: if the
// channel is full the ID is silently dropped; the sweep goroutine recovers it.
func (e *Embedder) Enqueue(id string) {
	select {
	case e.queue <- id:
	default:
	}
}

// Stop signals the background goroutine to exit.
func (e *Embedder) Stop() { close(e.stop) }

func (e *Embedder) run(ctx context.Context) {
	ticker := time.NewTicker(e.sweepDur)
	defer ticker.Stop()

	for {
		select {
		case <-e.stop:
			return
		case id := <-e.queue:
			e.ProcessOne(ctx, id)
			time.Sleep(e.delay)
		case <-ticker.C:
			e.Sweep(ctx)
		}
	}
}

// ProcessOne embeds a single artifact by ID. Exported for testing.
func (e *Embedder) ProcessOne(ctx context.Context, id string) {
	art, err := e.proto.GetArtifact(ctx, id)
	if err != nil || art == nil {
		return
	}

	text := embeddingText(art)
	vec, err := e.embedFunc(ctx, text)
	if err != nil {
		slog.WarnContext(ctx, "embed: call failed",
			slog.String(parchment.LogKeyID, id), slog.Any(parchment.LogKeyError, err))
		return
	}

	hash := parchment.ContentHash(art)
	if err := e.proto.StoreEmbedding(ctx, id, e.model, hash, vec); err != nil {
		slog.WarnContext(ctx, "embed: store failed",
			slog.String(parchment.LogKeyID, id), slog.Any(parchment.LogKeyError, err))
		return
	}

	// Add "encoded" label so the sweep ignores this artifact until content changes.
	_, _ = e.proto.SetField(ctx, []string{id}, "labels", parchment.LabelEncoded)
}

// Sweep finds artifacts without the "encoded" label and queues them. Exported for testing.
func (e *Embedder) Sweep(ctx context.Context) {
	arts, err := e.proto.ListArtifacts(ctx, parchment.ListInput{
		ExcludeLabels: []string{parchment.LabelEncoded},
		ExcludeKind:   "edge_type_definition",
	})
	if err != nil {
		slog.WarnContext(ctx, "embed: sweep list failed", slog.Any(parchment.LogKeyError, err))
		return
	}
	for _, art := range arts {
		e.Enqueue(art.ID)
	}
	if len(arts) > 0 {
		slog.InfoContext(ctx, "embed: sweep queued artifacts", slog.Int(parchment.LogKeyCount, len(arts)))
	}
}

// embeddingText constructs the text to embed from an artifact's content fields.
func embeddingText(art *parchment.Artifact) string {
	var b strings.Builder
	b.WriteString(art.Title)
	if art.Goal != "" {
		b.WriteString("\n")
		b.WriteString(art.Goal)
	}
	for _, s := range art.Sections {
		b.WriteString("\n")
		b.WriteString(s.Text)
	}
	return b.String()
}


