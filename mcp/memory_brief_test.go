package mcp_test

// memory_brief_test.go — brief memory section + orient recent sessions RED tests.
//
// brief must surface top-3 relevant knowledge artifacts for the current scope
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

// TestBrief_IncludesMemorySection verifies that brief returns a Memory section
// when knowledge artifacts exist for the scope.
func TestBrief_IncludesMemorySection(t *testing.T) {
	proto, callAdmin, _ := newMemoryServer(t)
	ctx := context.Background()

	// Seed an evergreen note — should surface in brief memory
	_, err := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixStatus + parchment.StatusEvergreen},
		Title: "SetField rejects unknown fields",

		Sections: []parchment.Section{
			{Name: "body", Text: "Use attach_section for named content instead."},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	out := callAdmin(map[string]any{
		"action": "brief",
		"scope":  "test",
	})

	// brief must include a memory or knowledge section
	hasMemory := strings.Contains(strings.ToLower(out), "memory") ||
		strings.Contains(strings.ToLower(out), "knowledge") ||
		strings.Contains(strings.ToLower(out), "setfield") ||
		strings.Contains(strings.ToLower(out), "unknown field")

	if !hasMemory {
		t.Errorf("brief must include relevant knowledge artifacts when they exist\nGot: %s", out)
	}
}

// TestBrief_NoMemorySection_WhenVaultEmpty verifies brief doesn't add a noisy
// empty Memory section when there are no knowledge artifacts.
func TestBrief_NoMemorySection_WhenVaultEmpty(t *testing.T) {
	_, callAdmin, _ := newMemoryServer(t)

	out := callAdmin(map[string]any{
		"action": "brief",
		"scope":  "test",
	})

	// No knowledge artifacts → no memory section needed
	// Should not show "(none)" or empty bullets
	if strings.Contains(out, "Memory:\n  (none)") ||
		strings.Contains(out, "Memory:\n\n") {
		t.Errorf("brief must not show an empty memory section\nGot: %s", out)
	}
}

// TestBrief_MemoryLimitedToThree verifies brief surfaces at most 3 memory items
// to keep the context tight.
func TestBrief_MemoryLimitedToThree(t *testing.T) {
	proto, callAdmin, _ := newMemoryServer(t)
	ctx := context.Background()

	for i := range 6 {
		_, _ = proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindNote, parchment.LabelPrefixStatus + parchment.StatusEvergreen},
			Title: "memory note",

			Sections: []parchment.Section{
				{Name: "body", Text: "some important observation about the system"},
			},
			Goal: "note" + string(rune('A'+i)),
		})
	}

	out := callAdmin(map[string]any{
		"action": "brief",
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
		t.Errorf("brief memory section must show at most 3 items, got %d\nGot: %s", memoryItems, out)
	}
}

// TestOrient_IncludesRecentSessions verifies that orient includes a Recent
// Sessions section populated from source artifacts created by ingest_session.
func TestOrient_IncludesRecentSessions(t *testing.T) {
	proto, _, callKnowledge := newMemoryServer(t)
	ctx := context.Background()

	// Simulate what ingest_session creates: a source artifact with provenance section
	_, err := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindSource},
		Title: "Session: 2026-05-26T10-00-00_abc123.jsonl (parchment)",

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

// TestBrief_SurfacesScope verifies that brief discloses the active scope filter
// so agents know what they can see without making an additional tool call.
func TestBrief_SurfacesScope(t *testing.T) {
	_, callAdmin, _ := newMemoryServer(t) // homeScopes = ["test"]

	out := callAdmin(map[string]any{"action": "brief"})

	if !strings.Contains(out, "Scope: test") {
		t.Errorf("brief must include active scope; got:\n%s", out)
	}
}

// TestBrief_OrientPointer verifies that a full (non-delta) brief appends a
// Tier 1→2 navigation hint pointing agents to artifact(action=orient).
func TestBrief_OrientPointer(t *testing.T) {
	_, callAdmin, _ := newMemoryServer(t)

	full := callAdmin(map[string]any{"action": "brief"})
	if !strings.Contains(full, "artifact(action=orient)") {
		t.Errorf("full brief must include orient navigation hint; got:\n%s", full)
	}

	// Delta call (since= set) must NOT include the pointer — it's redundant noise.
	delta := callAdmin(map[string]any{"action": "brief", "since": "2020-01-01T00:00:00Z"})
	if strings.Contains(delta, "artifact(action=orient)") {
		t.Errorf("delta brief must not include orient pointer; got:\n%s", delta)
	}
}
