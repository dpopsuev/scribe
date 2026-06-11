package integration_test

import (
	"testing"

	parchment "github.com/dpopsuev/parchment"

	"github.com/dpopsuev/scribe/testkit"
)

// TestAgentSession_E2E simulates a three-turn agent conversation using a
// scripted (faux) LLM, ingests each turn into Scribe, and asserts the
// resulting graph is correctly structured and queryable.
func TestAgentSession_E2E(t *testing.T) { //nolint:gocyclo // E2E: sequential assertions on one scenario
	srv, db := testkit.NewServer(t)
	defer srv.Close()

	sessionID := "sess-abc123"
	source := "faux-agent"

	loop := &testkit.AgentLoop{
		Session: testkit.AgentSession{ID: sessionID, Source: source, CWD: "/workspace/scribe", Model: "faux-1.0"},
		LLM: &testkit.ScriptedLLM{Script: []testkit.ScriptedTurn{
			{
				User:      "what is in graph.js?",
				Assistant: "Let me read that file.",
				Tools:     []testkit.AgentToolCall{{Name: "file_read", Input: "graph.js", Output: "export function initGraph() { ... }"}},
			},
			{
				User:      "(tool result received)",
				Assistant: "graph.js contains the initGraph function which sets up the 3D force graph.",
			},
			{
				User:      "create a task to refactor initGraph",
				Assistant: "Task created.",
				Tools:     []testkit.AgentToolCall{{Name: "scribe_create_artifact", Input: `{"kind":"task","title":"Refactor initGraph"}`, Output: "SCR-TSK-999"}},
			},
		}},
		Client: testkit.NewIngestClient(srv.URL, source),
	}

	if err := loop.Run(t.Context()); err != nil {
		t.Fatalf("agent run: %v", err)
	}

	// a) Three turn nodes.
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

	// c) Turn 0 has a traces_to edge toward locus:symbol:graph.js.
	edges, err := db.Neighbors(t.Context(), turn0.ID, "traces_to", parchment.Outgoing)
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(edges) == 0 {
		t.Error("want traces_to edge from turn 0, got none")
	} else if edges[0].To != "locus:symbol:graph.js" {
		t.Errorf("traces_to target: got %s, want locus:symbol:graph.js", edges[0].To)
	}

	// d) Turn 2 has a produces edge toward the created task.
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
		t.Errorf("produces edge: got %v, want target SCR-TSK-999", prodEdges)
	}

	// e) Session label query returns all 3 turns.
	if got := testkit.CountByLabels(t, db, "source:"+source, "session:"+sessionID); got != 3 {
		t.Errorf("session label query: got %d, want 3", got)
	}

	// f) Idempotency — second run, same data, zero errors, same count.
	loop.LLM.Reset()
	if err := loop.Run(t.Context()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := testkit.CountByLabels(t, db, "source:"+source, "session:"+sessionID); got != 3 {
		t.Errorf("after second run: %d nodes, want 3 (idempotent)", got)
	}
}
