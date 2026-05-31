package mcp_test

// memory_motd_test.go — motd memory section + orient recent sessions RED tests.
//
// motd must surface top-3 relevant knowledge artifacts for the current scope
// so agents get memory context without calling a separate tool.
//
// orient must include a "Recent Sessions" section from source artifacts
// created by ingest_session, showing what was worked on last.

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newMemoryServer(t *testing.T) (proto *parchment.Protocol, callAdmin, callKnowledge func(map[string]any) string) {
	t.Helper()
	s := openStore(t)
	proto = parchment.New(s, parchment.KnowledgeSchema(), []string{"test"}, nil, parchment.ProtocolConfig{})
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	callAdmin = func(args map[string]any) string {
		return callTool(t, cs, "admin", args)
	}
	callKnowledge = func(args map[string]any) string {
		return callTool(t, cs, "artifact", args)
	}
	return
}

// TestMotd_IncludesMemorySection verifies that motd returns a Memory section
// when knowledge artifacts exist for the scope.
func TestMotd_IncludesMemorySection(t *testing.T) {
	proto, callAdmin, _ := newMemoryServer(t)
	ctx := context.Background()

	// Seed an evergreen note — should surface in motd memory
	_, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:   parchment.KindNote,
		Title:  "SetField rejects unknown fields",
		Scope:  "test",
		Status: parchment.StatusEvergreen,
		Sections: []parchment.Section{
			{Name: "body", Text: "Use attach_section for named content instead."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	out := callAdmin(map[string]any{
		"action": "motd",
		"scope":  "test",
	})

	// motd must include a memory or knowledge section
	hasMemory := strings.Contains(strings.ToLower(out), "memory") ||
		strings.Contains(strings.ToLower(out), "knowledge") ||
		strings.Contains(strings.ToLower(out), "setfield") ||
		strings.Contains(strings.ToLower(out), "unknown field")

	if !hasMemory {
		t.Errorf("motd must include relevant knowledge artifacts when they exist\nGot: %s", out)
	}
}

// TestMotd_NoMemorySection_WhenVaultEmpty verifies motd doesn't add a noisy
// empty Memory section when there are no knowledge artifacts.
func TestMotd_NoMemorySection_WhenVaultEmpty(t *testing.T) {
	_, callAdmin, _ := newMemoryServer(t)

	out := callAdmin(map[string]any{
		"action": "motd",
		"scope":  "test",
	})

	// No knowledge artifacts → no memory section needed
	// Should not show "(none)" or empty bullets
	if strings.Contains(out, "Memory:\n  (none)") ||
		strings.Contains(out, "Memory:\n\n") {
		t.Errorf("motd must not show an empty memory section\nGot: %s", out)
	}
}

// TestMotd_MemoryLimitedToThree verifies motd surfaces at most 3 memory items
// to keep the context tight.
func TestMotd_MemoryLimitedToThree(t *testing.T) {
	proto, callAdmin, _ := newMemoryServer(t)
	ctx := context.Background()

	for i := range 6 {
		_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{
			Kind:   parchment.KindNote,
			Title:  "memory note",
			Scope:  "test",
			Status: parchment.StatusEvergreen,
			Sections: []parchment.Section{
				{Name: "body", Text: "some important observation about the system"},
			},
			Goal: "note" + string(rune('A'+i)),
		})
	}

	out := callAdmin(map[string]any{
		"action": "motd",
		"scope":  "test",
	})

	// Count memory items — find the Memory section and count bullet lines
	lines := strings.Split(out, "\n")
	inMemory := false
	memoryItems := 0
	for _, l := range lines {
		if strings.Contains(strings.ToLower(l), "memory") && strings.Contains(l, ":") {
			inMemory = true
			continue
		}
		if inMemory && strings.HasPrefix(strings.TrimSpace(l), "[") {
			memoryItems++
		}
		if inMemory && l == "" && memoryItems > 0 {
			break
		}
	}

	if memoryItems > 3 {
		t.Errorf("motd memory section must show at most 3 items, got %d\nGot: %s", memoryItems, out)
	}
}

// TestOrient_IncludesRecentSessions verifies that orient includes a Recent
// Sessions section populated from source artifacts created by ingest_session.
func TestOrient_IncludesRecentSessions(t *testing.T) {
	proto, _, callKnowledge := newMemoryServer(t)
	ctx := context.Background()

	// Simulate what ingest_session creates: a source artifact with provenance section
	_, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind:  parchment.KindSource,
		Title: "Session: 2026-05-26T10-00-00_abc123.jsonl (parchment)",
		Scope: "test",
		Sections: []parchment.Section{
			{Name: "provenance", Text: "/home/dpopsuev/.config/pi/agent/sessions/--parchment--/2026-05-26T10-00-00_abc123.jsonl"},
			{Name: "summary", Text: "Format: pi\nCWD: /home/dpopsuev/Workspace/parchment\nCompaction summaries: 2\nScribe tool calls observed: 47"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	out := callKnowledge(map[string]any{
		"action": "orient",
		"scope":  "test",
	})

	hasSessionsSection := strings.Contains(strings.ToLower(out), "session") ||
		strings.Contains(out, "provenance") ||
		strings.Contains(out, "abc123")

	if !hasSessionsSection {
		t.Errorf("orient must include Recent Sessions section when sessions have been ingested\nGot: %s", out)
	}
}

// TestOrient_NoSessionsSection_WhenNoneIngested verifies orient doesn't show
// an empty sessions section when no sessions have been ingested.
func TestOrient_NoSessionsSection_WhenNoneIngested(t *testing.T) {
	_, _, callKnowledge := newMemoryServer(t)

	out := callKnowledge(map[string]any{
		"action": "orient",
		"scope":  "test",
	})

	// No sessions ingested → no confusing empty section
	if strings.Contains(out, "Recent Sessions:\n  (none)") {
		t.Errorf("orient must not show empty Recent Sessions section\nGot: %s", out)
	}
}

// TestMotd_SurfacesScope verifies that motd discloses the active scope filter
// so agents know what they can see without making an additional tool call.
func TestMotd_SurfacesScope(t *testing.T) {
	_, callAdmin, _ := newMemoryServer(t) // homeScopes = ["test"]

	out := callAdmin(map[string]any{"action": "motd"})

	if !strings.Contains(out, "Scope: test") {
		t.Errorf("motd must include active scope; got:\n%s", out)
	}
}

// TestMotd_OrientPointer verifies that a full (non-delta) motd appends a
// Tier 1→2 navigation hint pointing agents to artifact(action=orient).
func TestMotd_OrientPointer(t *testing.T) {
	_, callAdmin, _ := newMemoryServer(t)

	full := callAdmin(map[string]any{"action": "motd"})
	if !strings.Contains(full, "artifact(action=orient)") {
		t.Errorf("full motd must include orient navigation hint; got:\n%s", full)
	}

	// Delta call (since= set) must NOT include the pointer — it's redundant noise.
	delta := callAdmin(map[string]any{"action": "motd", "since": "2020-01-01T00:00:00Z"})
	if strings.Contains(delta, "artifact(action=orient)") {
		t.Errorf("delta motd must not include orient pointer; got:\n%s", delta)
	}
}
