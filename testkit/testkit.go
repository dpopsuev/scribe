// Package testkit provides test fixtures, generators, and faux agents for
// integration and E2E tests. Nothing in this package is compiled into
// production binaries.
//
//nolint:gosec // weak rand throughout — all testkit code produces synthetic data, never security-sensitive values
package testkit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"

	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/web"
)

// ── NDJSON generator ──────────────────────────────────────────────────────

// ShapeFunc produces one NodeRecord for index i.
type ShapeFunc func(i int, source, scanSHA string) ingest.NodeRecord

// Generator streams synthetic NDJSON for load and integration tests.
type Generator struct {
	Source       string
	NodeCount    int
	EdgesPerNode int
	ScanSHA      string
	Shape        ShapeFunc
}

// Generate writes NDJSON records to w and returns node/edge counts.
func (g *Generator) Generate(w io.Writer) (nodes, edges int, err error) { //nolint:nonamedreturns // named returns used for deferred error accumulation
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	sha := g.ScanSHA
	if sha == "" {
		sha = fmt.Sprintf("%x", rand.Uint64())
	}

	ids := make([]string, g.NodeCount)
	for i := range g.NodeCount {
		rec := g.Shape(i, g.Source, sha)
		rec.Type = "node"
		ids[i] = rec.ID
		if err = enc.Encode(rec); err != nil {
			return
		}
		nodes++
	}

	for i, from := range ids {
		for range g.EdgesPerNode {
			j := rand.IntN(len(ids))
			if j == i {
				continue
			}
			if err = enc.Encode(ingest.EdgeRecord{
				Type:     "edge",
				From:     from,
				To:       ids[j],
				Relation: "relates_to",
			}); err != nil {
				return
			}
			edges++
		}
	}

	err = enc.Encode(ingest.MetaRecord{
		Type:       "meta",
		Source:     g.Source,
		ScanSHA:    sha,
		ScannedAt:  time.Now().UTC().Format(time.RFC3339),
		TotalNodes: nodes,
		TotalEdges: edges,
	})
	return
}

// ParseGenerated reads NDJSON from a Generator into typed slices for Client.Stream.
func ParseGenerated(t *testing.T, gen *Generator) ([]ingest.NodeRecord, []ingest.EdgeRecord) {
	t.Helper()
	var buf bytes.Buffer
	if _, _, err := gen.Generate(&buf); err != nil {
		t.Fatalf("generate: %v", err)
	}
	var nodes []ingest.NodeRecord
	var edges []ingest.EdgeRecord
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(sc.Bytes(), &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case "node":
			var n ingest.NodeRecord
			if err := json.Unmarshal(sc.Bytes(), &n); err == nil {
				nodes = append(nodes, n)
			}
		case "edge":
			var e ingest.EdgeRecord
			if err := json.Unmarshal(sc.Bytes(), &e); err == nil {
				edges = append(edges, e)
			}
		}
	}
	return nodes, edges
}

// ── Built-in shape functions ──────────────────────────────────────────────

// LocusComponentShape produces a node shaped like a Locus ArchService.
var LocusComponentShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:component:pkg/comp%d", source, i),
		Kind:   "note",
		Title:  fmt.Sprintf("pkg/comp%d", i),
		Status: "active",
		Labels: []string{"source:" + source, "kind:component", "lang:go"},
		Extra: map[string]any{
			"fan_in":     rand.IntN(20),
			"fan_out":    rand.IntN(30),
			"churn":      rand.IntN(50),
			"loc":        rand.IntN(2000) + 100,
			"scan_sha":   sha,
			"scanned_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
}

// JiraIssueShape produces a node shaped like a Jira issue.
var JiraIssueShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	priorities := []string{"high", "medium", "low"}
	types := []string{"bug", "story", "task"}
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:PROJ-%d", source, i+1),
		Kind:   "note",
		Title:  fmt.Sprintf("Issue %d: synthetic Jira ticket", i+1),
		Status: "active",
		Labels: []string{
			"source:" + source, "kind:issue",
			"priority:" + priorities[rand.IntN(len(priorities))],
			"type:" + types[rand.IntN(len(types))],
		},
		Extra: map[string]any{"issue_key": fmt.Sprintf("PROJ-%d", i+1), "scan_sha": sha},
	}
}

// GitHubPRShape produces a node shaped like a GitHub pull request.
var GitHubPRShape ShapeFunc = func(i int, source, sha string) ingest.NodeRecord {
	states := []string{"open", "merged", "closed"}
	return ingest.NodeRecord{
		ID:     fmt.Sprintf("%s:octocat/hello-world#%d", source, i+1),
		Kind:   "note",
		Title:  fmt.Sprintf("PR #%d: synthetic pull request", i+1),
		Status: "active",
		Labels: []string{"source:" + source, "kind:pr", "state:" + states[rand.IntN(len(states))]},
		Extra:  map[string]any{"number": i + 1, "scan_sha": sha},
	}
}

// ── Faux agent ────────────────────────────────────────────────────────────

// ScriptedTurn is one exchange in a scripted conversation.
type ScriptedTurn struct {
	User      string
	Assistant string
	Tools     []ingest.AgentToolCall
}

// ScriptedLLM drives a fixed conversation — no network, no API key.
type ScriptedLLM struct {
	Script []ScriptedTurn
	pos    int
}

// Next returns the next turn and whether there was one.
func (s *ScriptedLLM) Next() (ScriptedTurn, bool) {
	if s.pos >= len(s.Script) {
		return ScriptedTurn{}, false
	}
	t := s.Script[s.pos]
	s.pos++
	return t, true
}

// Reset replays the script from the beginning.
func (s *ScriptedLLM) Reset() { s.pos = 0 }

// AgentLoop runs a ScriptedLLM and writes each turn to Scribe.
type AgentLoop struct {
	Session ingest.AgentSession
	LLM     *ScriptedLLM
	Client  *ingest.Client
}

// Run drives the script to completion, streaming each turn to Scribe.
func (a *AgentLoop) Run(ctx context.Context) error {
	for i := 0; ; i++ {
		turn, ok := a.LLM.Next()
		if !ok {
			break
		}
		agentTurn := ingest.AgentTurn{
			ID:            fmt.Sprintf("%s:%s:%d", a.Session.Source, a.Session.ID, i),
			SessionID:     a.Session.ID,
			Index:         i,
			UserText:      turn.User,
			AssistantText: turn.Assistant,
			ToolCalls:     turn.Tools,
			Extra:         map[string]any{"model": a.Session.Model},
		}
		node := ingest.TurnToNodeRecord(agentTurn)
		edges := ingest.TurnToEdges(agentTurn)
		if _, err := a.Client.Stream(ctx, []ingest.NodeRecord{node}, edges); err != nil {
			return fmt.Errorf("turn %d: %w", i, err)
		}
	}
	return nil
}

// ── Test server helper ────────────────────────────────────────────────────

// NewServer starts a real Scribe HTTP server backed by an isolated SQLite DB.
// The server and DB are closed automatically when t ends.
func NewServer(t *testing.T) (*httptest.Server, parchment.Store) {
	t.Helper()
	db, err := parchment.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	proto := parchment.New(db, nil, nil, nil, parchment.ProtocolConfig{})
	return httptest.NewServer(web.NewServer(proto, "test", "")), db
}

// CountByPrefix returns the number of artifacts whose ID starts with prefix.
func CountByPrefix(t *testing.T, db parchment.Store, prefix string) int {
	t.Helper()
	arts, err := db.List(context.Background(), parchment.Filter{IDPrefix: prefix})
	if err != nil {
		t.Fatalf("list %q: %v", prefix, err)
	}
	return len(arts)
}

// CountByLabels returns the number of artifacts matching all given labels.
func CountByLabels(t *testing.T, db parchment.Store, labels ...string) int {
	t.Helper()
	arts, err := db.List(context.Background(), parchment.Filter{Labels: labels})
	if err != nil {
		t.Fatalf("list labels %v: %v", labels, err)
	}
	return len(arts)
}
