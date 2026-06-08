package integration_test

// Embedding fitness test suite.
//
// Measures how well the configured embedding model captures semantic similarity
// across the domains Scribe covers: work tracking, code intelligence, agent
// context, network topology, and document knowledge.
//
// Metrics reported per challenge:
//   Recall@1  — correct answer is the top result
//   Recall@3  — correct answer is in the top 3
//   MRR       — mean reciprocal rank (1/position of first correct answer)
//
// A model is considered fit if Recall@1 ≥ 0.70 and MRR ≥ 0.80 across all
// challenges. These thresholds are conservative — a good embedding model
// should score much higher on this deterministic corpus.
//
// The suite uses SemanticEmbeddingFunc with a shared vocabulary so results are
// deterministic and independent of any external service. To test a real model,
// run with SCRIBE_EMBED_URL set and the integration tag:
//   SCRIBE_EMBED_URL=http://localhost:11434 go test ./tests/integration/... -v

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/embed"
)

// ── vocabulary ────────────────────────────────────────────────────────────────

var fitnessVocab = []string{
	// Work tracking
	"task", "spec", "bug", "goal", "campaign", "milestone", "deadline", "priority",
	"implement", "refactor", "test", "deploy", "review", "blocked", "active", "draft",
	// Code / architecture
	"parchment", "protocol", "store", "filter", "artifact", "label", "trait", "edge",
	"schema", "sqlite", "migrate", "index", "cache", "query", "graph", "cycle",
	// Agent / context
	"session", "turn", "context", "recall", "embed", "vector", "semantic", "window",
	"compaction", "summary", "agent", "llm", "prompt", "token", "history",
	// Network
	"router", "firewall", "bandwidth", "latency", "subnet", "gateway", "redundant",
	// Knowledge
	"concept", "note", "source", "citation", "elaborates", "synthesizes", "knowledge",
}

// ── challenge definition ──────────────────────────────────────────────────────

type fitnessChallenge struct {
	name        string
	domain      string
	query       string
	corpus      []fitnessDoc
	flagIndices []int // indices of correct answers (usually 1, sometimes 2+)
}

type fitnessDoc struct {
	title string
	body  string
}

var fitnessChallenges = []fitnessChallenge{
	{
		name:   "cycle-detection-in-dag",
		domain: "code",
		query:  "prevent circular dependency loops in graph",
		corpus: []fitnessDoc{
			{title: "CycleGuardTrait rejects edges that would close a loop",
				body: "CycleGuard walks outgoing edges from the target; if the source is reachable, the edge would form a cycle and is rejected. Applies to depends_on and imports relations."},
			{title: "JWT token validation and session expiry",
				body: "The authentication service validates login requests and checks token expiry. Sessions are invalidated after 24 hours."},
			{title: "Database connection pool management",
				body: "The pool maintains a configurable number of idle connections. Connections are reused to amortize the cost of handshake."},
			{title: "UI button click handler for modal dismiss",
				body: "The dismiss button attaches an event listener that sets the modal display to none and clears the backdrop overlay."},
		},
		flagIndices: []int{0},
	},
	{
		name:   "semantic-search-context-recall",
		domain: "agent",
		query:  "retrieve relevant past conversation turns for context window",
		corpus: []fitnessDoc{
			{title: "Embedder background goroutine queues artifacts for vector storage",
				body: "The embedder drains a buffered channel, calls the Ollama embedding endpoint, stores the vector alongside a content hash, and adds the encoded:model label. The sweep re-queues artifacts missing the label."},
			{title: "Semantic recall: list mode=hybrid returns FTS and vector results merged",
				body: "list(mode=hybrid, query=..., session=abc, depth=2) searches both FTS5 and cosine similarity, deduplicates by ID, orders by score descending. The session shorthand prepends session:abc to the labels filter."},
			{title: "Sprint planning meeting notes for Q3",
				body: "The team agreed to focus on reducing cycle time. Three features are scheduled: dark mode toggle, CSV export, and notification preferences."},
			{title: "DNS round-robin load balancing configuration",
				body: "Multiple A records are returned for the same hostname. The client selects one at random. TTL controls cache duration."},
		},
		flagIndices: []int{1},
	},
	{
		name:   "network-boundary-isolation",
		domain: "network",
		query:  "all external traffic must pass through a single controlled entry point",
		corpus: []fitnessDoc{
			{title: "BoundaryTrait enforces wall semantics — no bypass allowed",
				body: "When BoundaryTrait is set on a node within a namespace, all edges from nodes outside the namespace must go through the boundary node. Direct edges to internal nodes are rejected at LinkArtifacts time."},
			{title: "Task lifecycle: draft to active requires context section",
				body: "A task cannot transition from draft to active unless it has a non-empty context section. The ConformanceTrait enforces this on the satisfies relation."},
			{title: "Firewall DMZ: all inbound traffic routes through the perimeter",
				body: "The DMZ router inspects all packets. Internal services are not directly reachable from external networks. The gateway has redundant paths for failover."},
			{title: "CSS custom property inheritance in component trees",
				body: "Custom properties cascade from parent to child elements. A child can override a property locally without affecting siblings."},
		},
		flagIndices: []int{0, 2}, // both BoundaryTrait and DMZ firewall descriptions are correct
	},
	{
		name:   "work-tracking-completion-rollup",
		domain: "work",
		query:  "automatically complete parent when all children finish",
		corpus: []fitnessDoc{
			{title: "RollupTrait: all targets complete triggers source auto-transition",
				body: "When every outgoing edge target of a relation with RollupTrait reaches a terminal status, the source artifact is automatically transitioned to complete via setStatus."},
			{title: "Kubernetes horizontal pod autoscaler configuration",
				body: "The HPA scales the deployment based on CPU utilization. When average usage exceeds 80%, new pods are created. Scale-down happens after a cooldown period."},
			{title: "CompletionRollup: completing all tasks auto-completes the goal",
				body: "The completionRollup method finds incoming parent_of edges with CompletionRollup=true, checks whether all outgoing edges of that relation are terminal, and fires autoCompleteParent when all children are done."},
			{title: "OAuth2 PKCE flow for single-page applications",
				body: "The client generates a code verifier and challenge. The authorisation server returns a code. The client exchanges the code for a token using the verifier."},
		},
		flagIndices: []int{0, 2},
	},
	{
		name:   "document-knowledge-contradiction",
		domain: "knowledge",
		query:  "two notes that express conflicting claims about the same concept",
		corpus: []fitnessDoc{
			{title: "ContradictsTrait: symmetric relation marks genuine disagreement",
				body: "When note A contradicts note B, the relation is symmetric — B also contradicts A. The knowledge lint surfaces these tensions for resolution. Neither note is authoritative until the contradiction is resolved."},
			{title: "Task dependency graph: depends_on creates execution ordering",
				body: "Tasks with depends_on edges cannot be activated until all their dependencies are complete. TopoSort produces the correct execution order."},
			{title: "Note: force graph uses N-body repulsion between nodes",
				body: "The simulation applies repulsive forces between all pairs of nodes at O(n^2) complexity. Long-range gravity attracts nodes toward the center."},
			{title: "Note: force graph uses Barnes-Hut approximation for performance",
				body: "The simulation groups distant nodes into super-nodes, reducing complexity to O(n log n). This contradicts the N-body approach — only one should be used."},
		},
		flagIndices: []int{0},
	},
	{
		name:   "agent-session-ordering",
		domain: "agent",
		query:  "turns in a conversation must be retrievable in chronological order",
		corpus: []fitnessDoc{
			{title: "OrdinalTrait on turn: label encodes zero-padded sequence position",
				body: "The label turn:00042 allows lexicographic sort of session turns. The embedder writes this label at ingest time from extra.turn_index. list(labels_prefix=[session:abc], sort=turn:) returns turns in conversation order."},
			{title: "SQLite WAL mode enables concurrent readers with one writer",
				body: "WAL mode appends changes to a write-ahead log. Readers see a consistent snapshot. The checkpoint process merges the log back into the main database."},
			{title: "Campaign parent_of goal parent_of task hierarchy",
				body: "A campaign contains goals; goals contain tasks. The AllowedPairs constraint enforces correct nesting. Archiving the campaign cascades to goals and tasks via CascadeTrait."},
			{title: "Ollama keep_alive pins the embedding model in memory",
				body: "Passing keep_alive:-1 prevents Ollama from unloading the model between calls. Without this, competing models cause cold-start timeouts exceeding 30 seconds."},
		},
		flagIndices: []int{0},
	},
}

// ── metrics ───────────────────────────────────────────────────────────────────

type challengeResult struct {
	name      string
	domain    string
	recall1   float64 // 1.0 if flag is rank 1
	recall3   float64 // 1.0 if flag is in top 3
	mrr       float64 // 1/rank of first flag
	topRanked string  // title of rank-1 result
	flagRanks []int   // rank positions of all flag documents
}

func runChallenge(t *testing.T, embedFn parchment.EmbeddingFunc, c fitnessChallenge) challengeResult {
	t.Helper()
	ctx := context.Background()

	queryVec, err := embedFn(ctx, c.query)
	if err != nil {
		t.Fatalf("challenge %s: embed query: %v", c.name, err)
	}

	type scored struct {
		idx   int
		title string
		score float32
	}
	results := make([]scored, len(c.corpus))
	for i, doc := range c.corpus {
		text := doc.title + "\n" + doc.body
		vec, err := embedFn(ctx, text)
		if err != nil {
			t.Fatalf("challenge %s doc %d: embed: %v", c.name, i, err)
		}
		results[i] = scored{idx: i, title: doc.title, score: parchment.CosineSimilarity(queryVec, vec)}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })

	flagSet := make(map[int]bool, len(c.flagIndices))
	for _, fi := range c.flagIndices {
		flagSet[fi] = true
	}

	var flagRanks []int
	for rank, r := range results {
		if flagSet[r.idx] {
			flagRanks = append(flagRanks, rank+1) // 1-indexed
		}
	}

	recall1 := 0.0
	recall3 := 0.0
	mrr := 0.0
	if len(flagRanks) > 0 {
		if flagRanks[0] == 1 {
			recall1 = 1.0
		}
		for _, r := range flagRanks {
			if r <= 3 {
				recall3 = 1.0
				break
			}
		}
		mrr = 1.0 / float64(flagRanks[0])
	}

	return challengeResult{
		name:      c.name,
		domain:    c.domain,
		recall1:   recall1,
		recall3:   recall3,
		mrr:       mrr,
		topRanked: results[0].title,
		flagRanks: flagRanks,
	}
}

// ── test entry points ─────────────────────────────────────────────────────────

func TestEmbedding_FitnessSuite_SemanticEmbedFunc(t *testing.T) {
	// Deterministic baseline using SemanticEmbeddingFunc.
	// Establishes the floor — a vocabulary-overlap model.
	// A real embedding model must score higher on cross-domain challenges.
	t.Parallel()
	embedFn := parchment.SemanticEmbeddingFunc(fitnessVocab)
	// SemanticEmbeddingFunc is a vocabulary-overlap baseline — not a real model.
	// Thresholds are low; the point is that R@3=1.0 (always in top 3).
	runFitnessSuite(t, embedFn, "SemanticEmbeddingFunc", 0.20, 0.40)
}

func TestEmbedding_FitnessSuite_OllamaModel(t *testing.T) {
	// Real model fitness test. Requires SCRIBE_EMBED_URL to be set.
	// Run: SCRIBE_EMBED_URL=http://localhost:11434 go test ./tests/integration/... -v -run FitnessSuite_Ollama
	embedURL := os.Getenv("SCRIBE_EMBED_URL")
	if embedURL == "" {
		t.Skip("SCRIBE_EMBED_URL not set — skipping real model fitness test")
	}
	model := os.Getenv("SCRIBE_EMBED_MODEL")
	if model == "" {
		model = "qwen3-embedding:0.6b"
	}
	t.Logf("testing model: %s at %s", model, embedURL)

	embedFn := embed.OllamaFunc(embedURL, model)
	// Real model thresholds: Recall@1 ≥ 0.70, MRR ≥ 0.80
	runFitnessSuite(t, embedFn, model, 0.70, 0.80)
}

func runFitnessSuite(t *testing.T, embedFn parchment.EmbeddingFunc, modelName string, minRecall1, minMRR float64) {
	t.Helper()
	results := make([]challengeResult, 0, len(fitnessChallenges))
	for _, c := range fitnessChallenges {
		results = append(results, runChallenge(t, embedFn, c))
	}

	// Aggregate metrics
	var sumRecall1, sumRecall3, sumMRR float64
	for _, r := range results {
		sumRecall1 += r.recall1
		sumRecall3 += r.recall3
		sumMRR += r.mrr
	}
	n := float64(len(results))
	avgRecall1 := sumRecall1 / n
	avgRecall3 := sumRecall3 / n
	avgMRR := sumMRR / n

	// Per-challenge report
	var report strings.Builder
	fmt.Fprintf(&report, "\n── Embedding Fitness Report: %s ──\n", modelName)
	fmt.Fprintf(&report, "%-35s  %-10s  R@1  R@3   MRR   Top result\n", "Challenge", "Domain")
	fmt.Fprintf(&report, "%s\n", strings.Repeat("─", 100))
	for _, r := range results {
		flagStr := fmt.Sprintf("rank%v", r.flagRanks)
		top := r.topRanked
		if len(top) > 40 {
			top = top[:37] + "..."
		}
		fmt.Fprintf(&report, "%-35s  %-10s  %.2f  %.2f  %.2f  %s %s\n",
			r.name, r.domain, r.recall1, r.recall3, r.mrr, top, flagStr)
	}
	fmt.Fprintf(&report, "%s\n", strings.Repeat("─", 100))
	fmt.Fprintf(&report, "%-35s  %-10s  %.2f  %.2f  %.2f\n",
		"AGGREGATE", "", avgRecall1, avgRecall3, avgMRR)
	t.Log(report.String())

	// Threshold assertions
	if avgRecall1 < minRecall1 {
		t.Errorf("Recall@1 %.2f < threshold %.2f — model not fit for semantic recall", avgRecall1, minRecall1)
	}
	if avgMRR < minMRR {
		t.Errorf("MRR %.2f < threshold %.2f — model ranks correct answers too low", avgMRR, minMRR)
	}
	// Per-challenge: warn (not fail) if a single challenge is missed
	for _, r := range results {
		if r.recall3 == 0 {
			t.Errorf("challenge %q: correct answer not in top 3 — check vocabulary or model", r.name)
		}
	}
}

var _ = math.Round // keep math import for potential future use in score display
