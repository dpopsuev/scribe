package mcp_test

// ingest_session_test.go — knowledge(action=ingest_session) tests.
//
// SCR-NED-10: vault should grow from agent work, not from deliberate capture.
// Import Pi / Claude Code JSONL sessions → extract decisions, patterns,
// concepts and external references as knowledge artifacts automatically.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

// buildPiSession writes a minimal Pi JSONL session and returns its path.
func buildPiSession(t *testing.T, dir string) string {
	t.Helper()
	entries := []map[string]any{
		{"type": "session", "version": 3, "id": "test-session-001", "timestamp": "2026-05-27T10:00:00.000Z", "cwd": "/workspace/myproject"},
		{"type": "message", "id": "aaa1", "parentId": nil, "timestamp": "2026-05-27T10:01:00.000Z",
			"message": map[string]any{"role": "user", "content": "How should we handle retries?"}},
		{"type": "message", "id": "aaa2", "parentId": "aaa1", "timestamp": "2026-05-27T10:01:05.000Z",
			"message": map[string]any{
				"role": "assistant",
				"content": []map[string]any{
					{"type": "text", "text": "Use exponential backoff with jitter. Cap at 5 retries."},
					{"type": "toolCall", "id": "tc1", "name": "bash", "arguments": map[string]any{"command": "go test ./..."}},
				},
			}},
		{"type": "message", "id": "aaa3", "parentId": "aaa2", "timestamp": "2026-05-27T10:01:10.000Z",
			"message": map[string]any{"role": "user", "content": "File this decision to Scribe"}},
		{"type": "compaction", "id": "aaa4", "parentId": "aaa3", "timestamp": "2026-05-27T10:05:00.000Z",
			"summary": "Discussed retry strategy. Decided on exponential backoff with jitter, cap 5 retries. Implemented in pkg/retry."},
	}

	path := filepath.Join(dir, "2026-05-27T10-00-00_test-session-001.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// buildClaudeSession writes a minimal Claude Code JSONL session and returns its path.
func buildClaudeSession(t *testing.T, dir string) string {
	t.Helper()
	entries := []map[string]any{
		{"type": "permission-mode", "permissionMode": "default", "sessionId": "abc123"},
		{"type": "user", "parentUuid": nil,
			"message": map[string]any{"role": "user", "content": "Should we use gRPC or REST?"}},
		{"type": "assistant", "message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "Use REST for external APIs, gRPC for internal service-to-service."},
				{"type": "tool_use", "id": "tu1", "name": "mcp__scribe__artifact",
					"input": map[string]any{"action": "create", "kind": "decision", "title": "REST external gRPC internal"}},
			},
		}},
		{"type": "user", "message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "tool_result", "tool_use_id": "tu1", "content": "created DEC-2026-001 [draft]"},
			},
		}},
	}

	path := filepath.Join(dir, "abc123.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func newIngestServer(t *testing.T) (proto *parchment.Protocol, call func(map[string]any) string) {
	t.Helper()
	s := openStore(t)
	proto = parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	call = func(args map[string]any) string {
		return callTool(t, cs, "admin", args)
	}
	return
}

// TestIngestSession_PiFormat verifies a Pi JSONL session is ingested and
// creates a source artifact with session metadata as provenance.
func TestIngestSession_PiFormat(t *testing.T) {
	proto, call := newIngestServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := buildPiSession(t, dir)

	out := call(map[string]any{
		"action": "ingest_session",
		"path":   path,
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(strings.ToLower(out), "unknown") {
		t.Fatalf("ingest_session failed: %s", out)
	}

	// A source artifact for the session must have been created.
	sources, err := proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("expected at least one source artifact for the session")
	}

	// Source must have provenance section with the file path.
	found := false
	for _, s := range sources {
		for _, sec := range s.Sections {
			if sec.Name == "provenance" && strings.Contains(sec.Text, path) {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("source artifact must have provenance section containing session path %q", path)
	}
}

// TestIngestSession_ClaudeFormat verifies a Claude Code JSONL session is ingested.
func TestIngestSession_ClaudeFormat(t *testing.T) {
	proto, call := newIngestServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := buildClaudeSession(t, dir)

	out := call(map[string]any{
		"action": "ingest_session",
		"path":   path,
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown action") {
		t.Fatalf("ingest_session action not implemented: %s", out)
	}

	sources, _ := proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: "test"})
	if len(sources) == 0 {
		t.Fatal("expected source artifact for Claude session")
	}
}

// TestIngestSession_ExtractsCompactionSummary verifies that compaction
// summaries become context artifacts (they contain synthesized knowledge).
func TestIngestSession_ExtractsCompactionSummary(t *testing.T) {
	proto, call := newIngestServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := buildPiSession(t, dir) // Pi session has a compaction entry

	call(map[string]any{
		"action": "ingest_session",
		"path":   path,
		"scope":  "test",
	})

	// The compaction summary should become a context or note artifact.
	all, _ := proto.ListArtifacts(ctx, parchment.ListInput{Scope: "test"})
	hasCompactionNote := false
	for _, a := range all {
		if a.Kind == parchment.KindContext || a.Kind == parchment.KindNote {
			for _, sec := range a.Sections {
				if strings.Contains(sec.Text, "retry") || strings.Contains(sec.Text, "backoff") {
					hasCompactionNote = true
				}
			}
		}
	}
	if !hasCompactionNote {
		t.Error("compaction summary should be extracted as a context/note artifact")
	}
}

// TestIngestSession_Idempotent verifies that re-ingesting the same session
// does not create duplicate artifacts.
func TestIngestSession_Idempotent(t *testing.T) {
	proto, call := newIngestServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	path := buildPiSession(t, dir)

	call(map[string]any{"action": "ingest_session", "path": path, "scope": "test"})
	call(map[string]any{"action": "ingest_session", "path": path, "scope": "test"})

	sources, _ := proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: "test"})
	// Should have exactly one source for this session, not two.
	sessionSources := 0
	for _, s := range sources {
		for _, sec := range s.Sections {
			if sec.Name == "provenance" && strings.Contains(sec.Text, path) {
				sessionSources++
			}
		}
	}
	if sessionSources > 1 {
		t.Errorf("re-ingesting same session should be idempotent, got %d source artifacts", sessionSources)
	}
}

// TestIngestSession_Directory verifies that a directory path ingests all
// JSONL files within it.
func TestIngestSession_Directory(t *testing.T) {
	proto, call := newIngestServer(t)
	ctx := context.Background()
	dir := t.TempDir()
	buildPiSession(t, dir)
	buildClaudeSession(t, dir)

	out := call(map[string]any{
		"action": "ingest_session",
		"path":   dir,
		"scope":  "test",
	})

	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("directory ingest failed: %s", out)
	}

	sources, _ := proto.ListArtifacts(ctx, parchment.ListInput{Kind: parchment.KindSource, Scope: "test"})
	if len(sources) < 2 {
		t.Errorf("directory ingest should create one source per session file, got %d", len(sources))
	}
}
