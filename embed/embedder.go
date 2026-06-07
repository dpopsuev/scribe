// Package embed provides a background embedding worker that keeps artifact
// vectors fresh in parchment without blocking the write path.
//
// Design:
//   - Writes enqueue artifact IDs on a buffered channel (non-blocking drop if full).
//   - A pool of worker goroutines drains the channel concurrently, each calling
//     the embed endpoint, storing the vector, and adding the "encoded" label.
//   - A controller goroutine adjusts concurrency every 10s based on an EWMA of
//     Ollama call latency: fast response → more workers; slow → fewer. This is
//     additive-increase / multiplicative-decrease (AIMD) — the same algorithm
//     TCP uses for congestion control.
//   - A periodic sweep finds artifacts missing the "encoded" label and re-queues
//     them, recovering any IDs dropped when the channel was full.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const (
	channelCap      = 4096 // larger buffer for backfill
	ewmaAlpha       = 0.15 // EWMA smoothing factor; lower = slower adaptation
	latencyLowMs    = 150  // below this: Ollama is fast, increase workers
	latencyHighMs   = 500  // above this: Ollama is struggling, decrease workers
	controlInterval = 10 * time.Second
)

var errEmptyEmbedding = fmt.Errorf("ollama returned empty embedding") //nolint:err113 // package-level sentinel

const logKeyWorkers = "workers"
const logKeyLatencyMs = "latency_ewma_ms"

// Embedder runs the background embedding loop with an adaptive worker pool.
type Embedder struct {
	proto      *parchment.Protocol
	model      string
	sweepDur   time.Duration
	maxWorkers int
	queue      chan string
	stop       chan struct{}
	embedFunc  parchment.EmbeddingFunc

	// concurrency control
	sem        chan struct{} // semaphore: capacity = current worker limit
	semMu      sync.Mutex    // guards sem replacement during resize
	ewmaMs     float64       // exponential weighted moving average latency in ms
	ewmaMu     sync.Mutex
	workersCur int32 // atomic: number of workers currently holding a sem token
}

// OllamaEmbedFunc returns a parchment.EmbeddingFunc that calls the Ollama
// /api/embeddings endpoint. Constructed before the Protocol — no circular dep.
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
			return nil, fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, raw) //nolint:err113 // status+body runtime values
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

// New constructs an Embedder and starts the background goroutines immediately.
// maxWorkers caps the adaptive pool; embedFunc overrides the embed call (tests).
func New(ctx context.Context, proto *parchment.Protocol, model string, sweepInterval time.Duration, maxWorkers int, embedFunc parchment.EmbeddingFunc) *Embedder {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	e := &Embedder{
		proto:      proto,
		model:      model,
		sweepDur:   sweepInterval,
		maxWorkers: maxWorkers,
		queue:      make(chan string, channelCap),
		stop:       make(chan struct{}),
		embedFunc:  embedFunc,
		sem:        make(chan struct{}, 1), // start conservative: 1 worker
		ewmaMs:     200,                    // assume 200ms until we have data
	}
	go e.dispatcher(ctx)
	go e.controller(ctx)
	go e.sweeper(ctx)
	return e
}

// Enqueue adds an artifact ID to the embedding queue. Non-blocking.
func (e *Embedder) Enqueue(id string) {
	select {
	case e.queue <- id:
	default:
	}
}

// Stop signals all background goroutines to exit.
func (e *Embedder) Stop() { close(e.stop) }

// dispatcher drains the queue, acquiring a semaphore slot per item and
// launching a worker goroutine for each.
func (e *Embedder) dispatcher(ctx context.Context) {
	for {
		select {
		case <-e.stop:
			return
		case id := <-e.queue:
			e.semMu.Lock()
			sem := e.sem
			e.semMu.Unlock()

			select {
			case sem <- struct{}{}:
				atomic.AddInt32(&e.workersCur, 1)
				go func(id string) {
					defer func() {
						e.semMu.Lock()
						s := e.sem
						e.semMu.Unlock()
						<-s
						atomic.AddInt32(&e.workersCur, -1)
					}()
					start := time.Now()
					e.ProcessOne(ctx, id)
					e.recordLatency(time.Since(start))
				}(id)
			case <-e.stop:
				return
			}
		}
	}
}

// controller adjusts semaphore capacity every controlInterval based on EWMA latency.
// AIMD: +1 worker when fast, -1 when slow; bounds [1, maxWorkers].
func (e *Embedder) controller(ctx context.Context) {
	ticker := time.NewTicker(controlInterval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-ticker.C:
			e.ewmaMu.Lock()
			latency := e.ewmaMs
			e.ewmaMu.Unlock()

			e.semMu.Lock()
			cur := cap(e.sem)
			var next int
			switch {
			case latency < latencyLowMs && cur < e.maxWorkers:
				next = cur + 1
			case latency > latencyHighMs && cur > 1:
				next = cur - 1
			default:
				next = cur
			}
			if next != cur {
				newSem := make(chan struct{}, next)
				// Drain existing tokens into new semaphore up to new cap.
				for len(e.sem) > 0 && len(newSem) < next {
					<-e.sem
					newSem <- struct{}{}
				}
				e.sem = newSem
				slog.InfoContext(ctx, "embed: concurrency adjusted",
					slog.Int(logKeyWorkers, next),
					slog.Float64(logKeyLatencyMs, math.Round(latency)),
				)
			}
			e.semMu.Unlock()
		}
	}
}

// sweeper fires a Sweep immediately at startup and then periodically.
func (e *Embedder) sweeper(ctx context.Context) {
	e.Sweep(ctx)
	ticker := time.NewTicker(e.sweepDur)
	defer ticker.Stop()
	for {
		select {
		case <-e.stop:
			return
		case <-ticker.C:
			e.Sweep(ctx)
		}
	}
}

func (e *Embedder) recordLatency(d time.Duration) {
	ms := float64(d.Milliseconds())
	e.ewmaMu.Lock()
	e.ewmaMs = ewmaAlpha*ms + (1-ewmaAlpha)*e.ewmaMs
	e.ewmaMu.Unlock()
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
	_, _ = e.proto.SetField(ctx, []string{id}, "labels", parchment.LabelEncoded)
}

// Sweep finds artifacts without the "encoded" label and queues them.
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
		slog.InfoContext(ctx, "embed: sweep queued artifacts",
			slog.Int(parchment.LogKeyCount, len(arts)),
			slog.Int(logKeyWorkers, cap(e.sem)),
		)
	}
}

// maxEmbedChars is a conservative character limit for nomic-embed-text (2048 tokens ≈ 8192 chars).
// We use 6000 to stay safely under the limit across all tokenizers.
const maxEmbedChars = 6000

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
		if b.Len() >= maxEmbedChars {
			break
		}
	}
	text := b.String()
	if len(text) > maxEmbedChars {
		text = text[:maxEmbedChars]
	}
	return text
}
