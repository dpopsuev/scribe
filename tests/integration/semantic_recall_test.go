package integration_test

// CTF-style semantic recall test.
//
// The flag is the artifact whose content best matches the query. The test
// seeds a corpus of artifacts with deliberately similar titles so FTS alone
// is insufficient — the query has no lexical overlap with the target artifact.
// Only a correctly embedded and ranked retrieval pipeline returns the flag first.
//
// Vocabulary (deterministic SemanticEmbeddingFunc):
//   "clock", "sync", "holdover", "ptp", "boundary", "grandmaster"
//   "authentication", "jwt", "token", "login", "session"
//   "graph", "physics", "force", "node", "simulation"
//   "embed", "vector", "similarity", "recall", "semantic"
//
// Query: "synchronization precision boundary clock"
// Flag:  the PTP artifact (the one about clock synchronization)
// Noise: auth, graph, embed artifacts — similar word counts but different semantics

import (
	"context"
	"encoding/json"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

var semanticVocab = []string{
	// PTP / timing domain
	"clock", "sync", "holdover", "ptp", "boundary", "grandmaster",
	// Auth domain
	"authentication", "jwt", "token", "login", "session",
	// Graph physics domain
	"graph", "physics", "force", "node", "simulation",
	// Embedding domain
	"embed", "vector", "similarity", "recall", "semantic",
}

type corpusEntry struct {
	title string
	body  string
	flag  bool // true = the expected top result for the query
}

var corpus = []corpusEntry{
	{
		title: "PTP clock synchronization and holdover under boundary clock conditions",
		body:  "A boundary clock maintains sync to a grandmaster and relays PTP to downstream nodes. During holdover the clock coasts on its local oscillator.",
		flag:  true,
	},
	{
		title: "JWT authentication flow and session token validation",
		body:  "The authentication service validates login requests and issues jwt tokens. Sessions expire after 24h.",
	},
	{
		title: "Force-directed graph physics and node simulation",
		body:  "A force simulation positions nodes by iterating repulsion and gravity forces until the graph reaches equilibrium.",
	},
	{
		title: "Vector embedding similarity and semantic recall pipeline",
		body:  "Embeddings map text to vectors. Cosine similarity ranks recall results by semantic proximity.",
	},
	{
		title: "General system notes",
		body:  "Miscellaneous notes about the project. No specific domain.",
	},
}

func TestSemanticRecall_FlagRankedFirst(t *testing.T) {
	// Given: a corpus of artifacts pre-embedded with SemanticEmbeddingFunc
	// When:  SearchSemantic is called with a query that has no lexical overlap with the flag title
	// Then:  the PTP artifact (flag) is ranked first
	t.Parallel()

	embedFn := parchment.SemanticEmbeddingFunc(semanticVocab)
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{
		EmbedFunc:  embedFn,
		EmbedModel: "test",
	})
	svc := service.New(proto, nil, []string{"test"})
	ctx := context.Background()

	var flagID string
	for _, entry := range corpus {
		art, err := proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind:  "note",
			Title: entry.title,
			Scope: "test",
			Sections: []parchment.Section{
				{Name: "body", Text: entry.body},
			},
		})
		if err != nil {
			t.Fatalf("create %q: %v", entry.title, err)
		}
		// Pre-embed using the protocol's EmbedFunc directly.
		text := entry.title + "\n" + entry.body
		vec, err := embedFn(ctx, text)
		if err != nil {
			t.Fatalf("embed %q: %v", entry.title, err)
		}
		hash := parchment.ContentHash(art)
		if err := proto.StoreEmbedding(ctx, art.ID, "test", hash, vec); err != nil {
			t.Fatalf("store embedding %q: %v", entry.title, err)
		}
		if entry.flag {
			flagID = art.ID
		}
	}

	if flagID == "" {
		t.Fatal("no flag artifact in corpus")
	}

	// The query has no words that appear in the flag title — FTS would miss it.
	// SemanticEmbeddingFunc encodes "synchronization precision boundary clock"
	// as hitting "sync", "boundary", "clock" — overlapping maximally with the PTP artifact.
	// "precision" is not in the vocab, so it contributes nothing — only the timing words fire.
	query := "synchronization precision boundary clock"

	op := service.Find("list")
	raw, _ := json.Marshal(map[string]any{
		"mode":  "semantic",
		"query": query,
		"scope": "test",
	})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatalf("semantic search: %v", err)
	}

	// The flag artifact ID must appear in the output.
	if out == "" {
		t.Fatal("empty result from semantic search")
	}

	// Parse the ranked results directly to verify the flag is first.
	arts, err := proto.SearchSemantic(ctx, query, parchment.ListInput{Scope: "test"})
	if err != nil {
		t.Fatalf("SearchSemantic: %v", err)
	}
	if len(arts) == 0 {
		t.Fatal("SearchSemantic returned no results")
	}
	if arts[0].ID != flagID {
		t.Errorf("flag not ranked first: got %s (%q), want %s", arts[0].ID, arts[0].Title, flagID)
		for i, a := range arts {
			t.Logf("  rank %d: %s — %s", i+1, a.ID, a.Title)
		}
	}
}
