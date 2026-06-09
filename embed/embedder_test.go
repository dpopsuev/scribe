package embed_test

import (
	"context"
	"slices"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/embed"
)

// stubEmbedFunc returns a fixed non-zero vector — sufficient for testing the
// label lifecycle without a real Ollama instance.
func stubEmbedFunc(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2, 0.3}, nil
}

func newTestProto(t *testing.T) *parchment.Protocol {
	t.Helper()
	store := parchment.NewMemoryStore()
	return parchment.New(store, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{
		EmbedFunc:  stubEmbedFunc,
		EmbedModel: "test-model",
	})
}

func newEmbedder(proto *parchment.Protocol) *embed.Embedder {
	return embed.New(context.Background(), proto, "test-model", time.Hour, 4, stubEmbedFunc)
}

func isEncoded(art *parchment.Artifact) bool {
	return slices.Contains(art.Labels, parchment.LabelEncoded("test-model"))
}

// TestEmbedder_EncodedLabelAdded verifies that processOne adds the "encoded" label.
func TestEmbedder_EncodedLabelAdded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	proto := newTestProto(t)

	art, err := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:note"}, Title: "graph physics notes"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	e := newEmbedder(proto)
	e.ProcessOne(ctx, art.ID)

	updated, _ := proto.GetArtifact(ctx, art.ID)
	if !isEncoded(updated) {
		t.Error("expected 'encoded' label after embedding")
	}
}

// TestEmbedder_EncodedLabelStrippedOnContentChange verifies that updating the
// title removes the "encoded" label so the artifact is re-queued.
func TestEmbedder_EncodedLabelStrippedOnContentChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	proto := newTestProto(t)

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:note"}, Title: "original title"})

	e := newEmbedder(proto)
	e.ProcessOne(ctx, art.ID)

	updated, _ := proto.GetArtifact(ctx, art.ID)
	if !isEncoded(updated) {
		t.Fatal("expected 'encoded' label after first embed")
	}

	// Change the title — should strip "encoded".
	_, err := proto.SetField(ctx, []string{art.ID}, "title", "changed title")
	if err != nil {
		t.Fatalf("set title: %v", err)
	}

	after, _ := proto.GetArtifact(ctx, art.ID)
	if isEncoded(after) {
		t.Error("'encoded' label should be removed after content change")
	}
}

// TestEmbedder_StatusChangeDoesNotStripEncoded verifies that changing status
// alone does not invalidate the embedding.
func TestEmbedder_StatusChangeDoesNotStripEncoded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	proto := newTestProto(t)

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:note"}, Title: "stable title"})

	e := newEmbedder(proto)
	e.ProcessOne(ctx, art.ID)

	updated, _ := proto.GetArtifact(ctx, art.ID)
	if !isEncoded(updated) {
		t.Fatal("expected 'encoded' label after embed")
	}

	// Status change must not strip "encoded".
	_, _ = proto.SetField(ctx, []string{art.ID}, "status", "active", parchment.SetFieldOptions{Force: true})

	after, _ := proto.GetArtifact(ctx, art.ID)
	if !isEncoded(after) {
		t.Error("'encoded' label should survive a status-only change")
	}
}

// TestEmbedder_SweepQueuesUnencodedArtifacts verifies that Sweep_ enqueues
// artifacts that lack the "encoded" label.
func TestEmbedder_SweepQueuesUnencodedArtifacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	proto := newTestProto(t)

	// Create two artifacts — neither has "encoded".
	a1, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:note"}, Title: "note one"})
	a2, _ := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{"kind:note"}, Title: "note two"})

	e := newEmbedder(proto)
	e.Sweep(ctx)

	// Process what was queued.
	e.ProcessOne(ctx, a1.ID)
	e.ProcessOne(ctx, a2.ID)

	for _, id := range []string{a1.ID, a2.ID} {
		art, _ := proto.GetArtifact(ctx, id)
		if !isEncoded(art) {
			t.Errorf("%s should be encoded after sweep + processOne", id)
		}
	}
}
