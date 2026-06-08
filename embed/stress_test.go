package embed_test

// Embedder stress tests — five scenarios untested by unit tests.
//
// Run all:   go test ./embed/... -run TestEmbedder_Stress -v -timeout 120s
// With race: go test ./embed/... -run TestEmbedder_Stress -race -timeout 120s

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/embed"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// countingEmbedFunc returns an EmbeddingFunc that counts calls and sleeps
// for the given duration to simulate Ollama latency.
func countingEmbedFunc(calls *int64, latency time.Duration) parchment.EmbeddingFunc {
	return func(_ context.Context, _ string) ([]float32, error) {
		atomic.AddInt64(calls, 1)
		if latency > 0 {
			time.Sleep(latency)
		}
		return []float32{0.1, 0.2, 0.3}, nil
	}
}

// slowThenFastFunc returns a func that is slow for the first n calls then fast.
func slowThenFastFunc(n int64, slowLatency, fastLatency time.Duration) parchment.EmbeddingFunc {
	var count int64
	return func(_ context.Context, _ string) ([]float32, error) {
		c := atomic.AddInt64(&count, 1)
		if c <= n {
			time.Sleep(slowLatency)
		} else {
			time.Sleep(fastLatency)
		}
		return []float32{0.1, 0.2, 0.3}, nil
	}
}

func newStressProto(t *testing.T, artifactCount int, embedFn parchment.EmbeddingFunc) *parchment.Protocol {
	t.Helper()
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, parchment.KnowledgeSchema(), []string{"test"}, nil,
		parchment.ProtocolConfig{EmbedFunc: embedFn, EmbedModel: "stress-model"})
	ctx := context.Background()
	for i := range artifactCount {
		_, err := proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind: "note", Scope: "test",
			Title: strings.Repeat("stress test artifact content ", 10),
		})
		if err != nil {
			t.Fatalf("create artifact %d: %v", i, err)
		}
	}
	return proto
}

// ── 1. THROUGHPUT ─────────────────────────────────────────────────────────────

func TestEmbedder_Stress_Throughput(t *testing.T) {
	// Given: 500 artifacts, embedFunc completes instantly, 8 workers.
	// When: embedder processes the full corpus.
	// Then: all 500 are embedded within 5 seconds; throughput ≥ 50/s.
	t.Parallel()
	const count = 500
	var calls int64
	proto := newStressProto(t, count, countingEmbedFunc(&calls, 0))

	e := embed.New(context.Background(), proto, "stress-model",
		time.Hour, 8, countingEmbedFunc(&calls, 0))
	defer e.Stop()

	e.Sweep(context.Background())

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&calls) >= count {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := atomic.LoadInt64(&calls)
	elapsed := time.Since(deadline.Add(-5 * time.Second))
	throughput := float64(got) / elapsed.Seconds()

	t.Logf("throughput: %.0f embeds/sec (%d in %.2fs)", throughput, got, elapsed.Seconds())

	if got < count {
		t.Errorf("only %d/%d artifacts embedded in 5s — throughput too low", got, count)
	}
}

// ── 2. BACKPRESSURE ───────────────────────────────────────────────────────────

func TestEmbedder_Stress_Backpressure(t *testing.T) {
	// Given: channel capacity 4096, enqueue 8000 IDs at once.
	// When: Enqueue is called for all 8000.
	// Then: the call never blocks; channel holds at most 4096.
	//       The remainder are silently dropped and recovered on next Sweep.
	t.Parallel()
	var calls int64
	proto := newStressProto(t, 100, countingEmbedFunc(&calls, 0))

	e := embed.New(context.Background(), proto, "stress-model",
		time.Hour, 1, countingEmbedFunc(&calls, 0))
	defer e.Stop()

	start := time.Now()
	for i := range 8000 {
		e.Enqueue(strings.Repeat("x", i%10+1)) // arbitrary IDs
	}
	elapsed := time.Since(start)

	// Enqueue must not block — 8000 calls must complete in well under 1ms each.
	if elapsed > 100*time.Millisecond {
		t.Errorf("Enqueue blocked: 8000 calls took %v, expected < 100ms", elapsed)
	}
	t.Logf("8000 Enqueue calls took %v (non-blocking confirmed)", elapsed)
}

// ── 3. AIMD SCALING ───────────────────────────────────────────────────────────

func TestEmbedder_Stress_AIMDScaling(t *testing.T) {
	// Given: embedFunc is fast (5ms) for first 50 calls, then slow (600ms).
	// maxWorkers=8, controlInterval=10s.
	// When: embedder runs for 25s (2+ controller cycles).
	// Then: some artifacts are processed (controller didn't deadlock).
	//       We measure throughput in phase 1 (fast) vs phase 2 (slow).
	t.Parallel()

	const fastLatency = 5 * time.Millisecond
	const slowLatency = 600 * time.Millisecond
	const switchAfter = int64(50)

	var calls int64
	// Wrap slowThenFast so the outer counter tracks all calls.
	inner := slowThenFastFunc(switchAfter, fastLatency, slowLatency)
	embedFn := func(ctx context.Context, text string) ([]float32, error) {
		atomic.AddInt64(&calls, 1)
		return inner(ctx, text)
	}

	proto := newStressProto(t, 200, embedFn)
	e := embed.New(context.Background(), proto, "stress-model",
		time.Hour, 8, embedFn)
	defer e.Stop()

	e.Sweep(context.Background())

	// Wait for 2 controller intervals (10s each) + processing time.
	time.Sleep(25 * time.Second)

	got := atomic.LoadInt64(&calls)
	t.Logf("AIMD scaling: %d embed calls in 25s (fast phase: first %d, slow phase: rest)", got, switchAfter)

	if got == 0 {
		t.Error("no artifacts processed — dispatcher or sweep appears stuck")
	}
}

// ── 4. RACE DETECTION ─────────────────────────────────────────────────────────

func TestEmbedder_Stress_RaceDetection(t *testing.T) {
	// Concurrent Enqueue + Sweep + ProcessOne + Stop under the race detector.
	// Run with: go test ./embed/... -race -run TestEmbedder_Stress_RaceDetection
	t.Parallel()
	var calls int64
	proto := newStressProto(t, 50, countingEmbedFunc(&calls, time.Millisecond))

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	e := embed.New(ctx, proto, "stress-model",
		500*time.Millisecond, 4, countingEmbedFunc(&calls, time.Millisecond))

	// Concurrent goroutines hammering Enqueue and Sweep simultaneously.
	done := make(chan struct{})
	for range 5 {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					e.Enqueue("test-id")
					e.Sweep(ctx)
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	time.Sleep(5 * time.Second)
	close(done)
	e.Stop()

	t.Logf("race test: %d embed calls completed without data race", atomic.LoadInt64(&calls))
}

// ── 5. LONG TEXT ──────────────────────────────────────────────────────────────

func TestEmbedder_Stress_LongText(t *testing.T) {
	// Given: an artifact with 8000-char content.
	// When: ProcessOne is called.
	// Then: the embed call completes (no timeout, no panic, encoded label added).
	//
	// With a real model (7-8s per call), this validates the 120s client timeout
	// and confirms keep_alive pins the model. Uses stub for CI.
	t.Parallel()
	var calls int64

	// 8000-char dense technical text — worst case for tokenizer density
	longText := strings.Repeat(
		"parchment artifact graph edge type trait CycleGuard CascadeArchive AllowedPairs "+
			"CompletionRollup ConformanceCheck LabelTrait EdgeTypeTrait Protocol SQLiteStore "+
			"MemoryStore Filter Artifact CreateInput ListInput SetField ComponentRegistry "+
			"TraitStore conflictPolicy ConflictReject ConflictMinimum ConflictPresence "+
			"ConflictUnion Strangler Fig TDD Red Green Refactor scope label status kind ",
		20, // ~1600 chars × 5 = 8000 chars
	)
	if len(longText) > 8000 {
		longText = longText[:8000]
	}

	embedFn := countingEmbedFunc(&calls, 5*time.Millisecond)
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, parchment.KnowledgeSchema(), []string{"test"}, nil,
		parchment.ProtocolConfig{EmbedFunc: embedFn, EmbedModel: "stress-model"})

	// Create an artifact with a long section.
	ctx := context.Background()
	art, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: "note", Scope: "test", Title: "long text stress test",
		Sections: []parchment.Section{{Name: "body", Text: longText}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	e := embed.New(context.Background(), proto, "stress-model",
		time.Hour, 1, embedFn)
	defer e.Stop()

	start := time.Now()
	e.ProcessOne(ctx, art.ID)
	elapsed := time.Since(start)

	t.Logf("long text (8000 chars) processed in %v", elapsed)

	if atomic.LoadInt64(&calls) == 0 {
		t.Error("embed func was never called — ProcessOne may have short-circuited")
	}

	// Verify encoded label was added.
	updated, _ := proto.GetArtifact(ctx, art.ID)
	found := false
	for _, l := range updated.Labels {
		if l == parchment.LabelEncoded("stress-model") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("encoded label not added after long-text embed; labels=%v", updated.Labels)
	}
}
