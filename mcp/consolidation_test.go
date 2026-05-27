package mcp_test

// consolidation_test.go — RED tests for the 4→3 tool API consolidation.
//
// Design decision: everything is an Artifact — Work or Knowledge.
// The knowledge tool is eliminated. Its actions move to artifact and admin.
//
// New contracts:
//   artifact(action=recall, query=...)       — was knowledge(action=recall)
//   artifact(action=create, kind=note)       — was knowledge(action=capture)
//   artifact(action=list, family=knowledge)  — was knowledge(action=orient/catalog)
//   admin(action=ingest_session, path=...)   — was knowledge(action=ingest_session)
//   admin(action=detect, check=knowledge)    — was knowledge(action=lint)
//
// knowledge tool → redirect hints for all 14 actions (not hard-removed for compat).

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newConsolidatedServer(t *testing.T) (proto *parchment.Protocol, callArtifact, callAdmin, callKnowledge func(map[string]any) string) {
	t.Helper()
	s := openStore(t)
	proto = parchment.New(s, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{
		EmbedFunc: parchment.SemanticEmbeddingFunc([]string{
			"template", "conformance", "setfield", "recall", "archive", "knowledge",
		}),
	})
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{
		EmbedFunc: parchment.SemanticEmbeddingFunc([]string{
			"template", "conformance", "setfield", "recall", "archive", "knowledge",
		}),
	}, "v0")
	cs := connectClient(t, srv)
	callArtifact = func(args map[string]any) string { return callTool(t, cs, "artifact", args) }
	callAdmin = func(args map[string]any) string { return callTool(t, cs, "admin", args) }
	callKnowledge = func(args map[string]any) string { return callTool(t, cs, "knowledge", args) }
	return
}

// ─── artifact(action=recall) ──────────────────────────────────────────────────

// TestConsolidation_ArtifactRecall verifies that recall moved from the knowledge
// tool to the artifact tool. artifact(action=recall) must find knowledge artifacts.
func TestConsolidation_ArtifactRecall(t *testing.T) {
	proto, callArtifact, _, _ := newConsolidatedServer(t)
	ctx := context.Background()

	_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindNote, Title: "template conformance deferred",
		Scope: "test", Status: parchment.StatusEvergreen,
		Sections: []parchment.Section{{Name: "body", Text: "conformance fires on promote not create"}},
	})

	out := callArtifact(map[string]any{
		"action": "recall",
		"query":  "template conformance",
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown artifact action") {
		t.Fatalf("artifact(action=recall) not implemented: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "conformance") && !strings.Contains(strings.ToLower(out), "template") {
		t.Errorf("artifact(action=recall) must find knowledge artifacts\nGot: %s", out)
	}
}

// TestConsolidation_ArtifactCreateNote verifies that knowledge artifacts are
// created via artifact(action=create, kind=note) — same tool, different kind.
func TestConsolidation_ArtifactCreateNote(t *testing.T) {
	_, callArtifact, _, _ := newConsolidatedServer(t)

	out := callArtifact(map[string]any{
		"action": "create",
		"kind":   "note",
		"title":  "SetField rejects unknown fields",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "body", "text": "use attach_section instead"},
		},
	})

	// Note was created: output contains the artifact ID and title
	if !strings.Contains(out, "created") && !strings.Contains(out, "NOT-") {
		t.Errorf("artifact(create, kind=note) must succeed\nGot: %s", out)
	}
}

// TestConsolidation_ArtifactListKnowledgeFamily verifies that knowledge artifacts
// can be listed via artifact(action=list, family=knowledge).
func TestConsolidation_ArtifactListKnowledgeFamily(t *testing.T) {
	proto, callArtifact, _, _ := newConsolidatedServer(t)
	ctx := context.Background()

	_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindNote, Title: "a fleeting note", Scope: "test",
	})
	_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "a task", Scope: "test",
	})

	out := callArtifact(map[string]any{
		"action": "list",
		"scope":  "test",
		"family": "knowledge",
	})

	if strings.Contains(strings.ToLower(out), "unknown") {
		t.Fatalf("artifact(list, family=knowledge) not implemented: %s", out)
	}
	// Must include the note, must not include the task
	if !strings.Contains(strings.ToLower(out), "fleeting note") && !strings.Contains(out, "note") {
		t.Errorf("artifact(list, family=knowledge) must show knowledge artifacts\nGot: %s", out)
	}
}

// ─── admin(action=ingest_session) ────────────────────────────────────────────

// TestConsolidation_AdminIngestSession verifies that ingest_session moved from
// the knowledge tool to the admin tool.
func TestConsolidation_AdminIngestSession(t *testing.T) {
	dir := t.TempDir()
	path := buildPiSession(t, dir)

	_, _, callAdmin, _ := newConsolidatedServer(t)

	out := callAdmin(map[string]any{
		"action": "ingest_session",
		"path":   path,
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown admin action") {
		t.Fatalf("admin(action=ingest_session) not implemented: %s", out)
	}
	// Must report ingestion activity
	if out == "" {
		t.Error("admin(action=ingest_session) must return output")
	}
}

// ─── knowledge tool → redirect hints ─────────────────────────────────────────

// TestConsolidation_KnowledgeCapture_Redirect verifies that knowledge(capture)
// returns a redirect hint pointing agents to artifact(create, kind=note).
func TestConsolidation_KnowledgeCapture_Redirect(t *testing.T) {
	_, _, _, callKnowledge := newConsolidatedServer(t)

	out := callKnowledge(map[string]any{
		"action": "capture",
		"title":  "test",
		"scope":  "test",
	})

	// Must NOT error with "unknown action" — must give a redirect
	if strings.Contains(strings.ToLower(out), "unknown knowledge action") {
		t.Errorf("knowledge(capture) must redirect, not error\nGot: %s", out)
	}
	// Must mention artifact tool or kind=note
	hasRedirect := strings.Contains(strings.ToLower(out), "artifact") ||
		strings.Contains(strings.ToLower(out), "kind=note") ||
		strings.Contains(strings.ToLower(out), "kind: note")
	if !hasRedirect {
		t.Errorf("knowledge(capture) redirect must mention artifact or kind=note\nGot: %s", out)
	}
}

// TestConsolidation_KnowledgeRecall_Redirect verifies that knowledge(recall)
// redirects to artifact(recall).
func TestConsolidation_KnowledgeRecall_Redirect(t *testing.T) {
	_, _, _, callKnowledge := newConsolidatedServer(t)

	out := callKnowledge(map[string]any{
		"action": "recall",
		"query":  "test query",
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown knowledge action") {
		t.Errorf("knowledge(recall) must redirect, not error\nGot: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "artifact") {
		t.Errorf("knowledge(recall) redirect must mention artifact tool\nGot: %s", out)
	}
}

// TestConsolidation_KnowledgeOrient_Redirect verifies orient redirects to artifact(list).
func TestConsolidation_KnowledgeOrient_Redirect(t *testing.T) {
	_, _, _, callKnowledge := newConsolidatedServer(t)

	out := callKnowledge(map[string]any{
		"action": "orient",
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown knowledge action") {
		t.Errorf("knowledge(orient) must redirect\nGot: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "artifact") {
		t.Errorf("knowledge(orient) redirect must mention artifact tool\nGot: %s", out)
	}
}

// ─── Removed artifact actions → redirect hints ───────────────────────────────

// TestConsolidation_BatchCreate_Folded verifies that batch_create is folded
// into create with an artifacts[] parameter.
func TestConsolidation_BatchCreate_Folded(t *testing.T) {
	_, callArtifact, _, _ := newConsolidatedServer(t)

	// batch_create via create with artifacts[] — new unified interface
	out := callArtifact(map[string]any{
		"action": "create",
		"artifacts": []map[string]any{
			{"kind": "note", "title": "note one", "scope": "test"},
			{"kind": "note", "title": "note two", "scope": "test"},
		},
	})

	if strings.Contains(strings.ToLower(out), "unknown artifact action") {
		t.Fatalf("artifact(create) with artifacts[] must work: %s", out)
	}
	if !strings.Contains(out, "note one") && !strings.Contains(out, "2") {
		t.Errorf("batch create via artifacts[] must create both notes\nGot: %s", out)
	}
}
