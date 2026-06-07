package web_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"testing"

	parchment "github.com/dpopsuev/parchment"

	"github.com/dpopsuev/scribe/internal/ingest"
	"github.com/dpopsuev/scribe/web"
)

// ── Faux agent primitives ─────────────────────────────────────────────────

// ScriptedTurn is one exchange in a scripted conversation.
type ScriptedTurn struct {
	User      string
	Assistant string
	Tools     []ingest.AgentToolCall
}

// ScriptedLLM drives a fixed conversation script.
// It is the test-double LLM: no network, no API key, fully deterministic.
type ScriptedLLM struct {
	script []ScriptedTurn
	pos    int
}

func (s *ScriptedLLM) Next() (ScriptedTurn, bool) {
	if s.pos >= len(s.script) {
		return ScriptedTurn{}, false
	}
	t := s.script[s.pos]
	s.pos++
	return t, true
}

// AgentLoop runs a ScriptedLLM and writes each turn to Scribe via ingest.Client.
// This is the minimal "agent" — no real LLM, no real tools, just the data flow.
type AgentLoop struct {
	Session ingest.AgentSession
	LLM     *ScriptedLLM
	Client  *ingest.Client
}

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

// ── Test ──────────────────────────────────────────────────────────────────

func TestAgentSession_E2E(t *testing.T) { //nolint:gocyclo // E2E test: sequential assertions on one scenario; splitting would obscure the narrative
	// Real Scribe server, isolated SQLite DB.
	db, err := parchment.OpenSQLite(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	proto := parchment.New(db, nil, nil, nil, parchment.ProtocolConfig{})
	srv := httptest.NewServer(web.NewServer(proto, "test", ""))
	defer srv.Close()

	sessionID := "sess-abc123"
	source := "faux-agent"

	// Scripted conversation: 3 turns.
	//   Turn 0: user asks about a file → agent reads it
	//   Turn 1: agent replies with content
	//   Turn 2: user asks to create a task → agent creates one
	script := []ScriptedTurn{
		{
			User:      "what is in graph.js?",
			Assistant: "Let me read that file.",
			Tools: []ingest.AgentToolCall{
				{Name: "file_read", Input: "graph.js", Output: "export function initGraph() { ... }"},
			},
		},
		{
			User:      "(tool result received)",
			Assistant: "graph.js contains the initGraph function which sets up the 3D force graph.",
		},
		{
			User:      "create a task to refactor initGraph",
			Assistant: "Task created.",
			Tools: []ingest.AgentToolCall{
				{Name: "scribe_create_artifact", Input: `{"kind":"task","title":"Refactor initGraph"}`, Output: "SCR-TSK-999"},
			},
		},
	}

	loop := &AgentLoop{
		Session: ingest.AgentSession{
			ID:     sessionID,
			Source: source,
			CWD:    "/workspace/scribe",
			Model:  "faux-1.0",
		},
		LLM:    &ScriptedLLM{script: script},
		Client: &ingest.Client{BaseURL: srv.URL, Source: source},
	}

	// ── First run ────────────────────────────────────────────────────────
	if err := loop.Run(t.Context()); err != nil {
		t.Fatalf("agent run: %v", err)
	}

	// a) 3 turn nodes in parchment.
	arts, err := db.List(t.Context(), parchment.Filter{
		Labels: []string{"source:" + source, "session:" + sessionID, "kind:turn"},
	})
	if err != nil {
		t.Fatalf("list turns: %v", err)
	}
	if got := len(arts); got != 3 {
		t.Errorf("want 3 turn nodes, got %d", got)
	}

	// b) Turn 0 extra.tools_used contains file_read.
	var turn0 *parchment.Artifact
	for _, a := range arts {
		if a.Extra["turn_index"] == float64(0) {
			turn0 = a
			break
		}
	}
	if turn0 == nil {
		t.Fatal("turn 0 not found")
	}
	tools, _ := turn0.Extra["tools_used"].([]any)
	if len(tools) == 0 || tools[0] != "file_read" {
		t.Errorf("turn 0 tools_used: got %v, want [file_read]", tools)
	}

	// c) Turn 0 has traces_to edge toward locus:symbol:graph.js.
	edges, err := db.Neighbors(t.Context(), turn0.ID, "traces_to", parchment.Outgoing)
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(edges) == 0 {
		t.Error("want traces_to edge from turn 0 to locus:symbol:graph.js, got none")
	} else if edges[0].To != "locus:symbol:graph.js" {
		t.Errorf("traces_to target: got %s, want locus:symbol:graph.js", edges[0].To)
	}

	// d) Turn 2 has produces edge toward the created task.
	var turn2 *parchment.Artifact
	for _, a := range arts {
		if a.Extra["turn_index"] == float64(2) {
			turn2 = a
			break
		}
	}
	if turn2 == nil {
		t.Fatal("turn 2 not found")
	}
	prodEdges, err := db.Neighbors(t.Context(), turn2.ID, "produces", parchment.Outgoing)
	if err != nil {
		t.Fatalf("produces neighbors: %v", err)
	}
	if len(prodEdges) == 0 || prodEdges[0].To != "SCR-TSK-999" {
		t.Errorf("produces edge: got %v, want SCR-TSK-999", prodEdges)
	}

	// e) Label filter returns all 3 turns by session.
	all, err := db.List(t.Context(), parchment.Filter{
		Labels: []string{"source:" + source, "session:" + sessionID},
	})
	if err != nil {
		t.Fatalf("label list: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("label query: got %d, want 3", len(all))
	}

	// f) Idempotency — second run produces zero errors and same count.
	loop.LLM = &ScriptedLLM{script: script} // reset the script
	if err := loop.Run(t.Context()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	arts2, _ := db.List(t.Context(), parchment.Filter{
		Labels: []string{"source:" + source, "session:" + sessionID, "kind:turn"},
	})
	if len(arts2) != 3 {
		t.Errorf("after second run: got %d nodes, want 3 (idempotent)", len(arts2))
	}
}
