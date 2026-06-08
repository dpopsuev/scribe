package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newBriefSetup(t *testing.T) (store parchment.Store, call func(args map[string]any) string) {
	t.Helper()
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	return s, func(args map[string]any) string { return callTool(t, cs, "admin", args) }
}

func seedBriefNoise(t *testing.T, s parchment.Store) {
	t.Helper()
	ctx := context.Background()
	old := time.Now().Add(-30 * 24 * time.Hour)

	// Docs and refs — trigger Domain Context
	for i := 0; i < 5; i++ {
		_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:ref", "status:draft"}, ID: "REF-" + string(rune('A'+i)), Scope: "test", Title: "Reference " + string(rune('A'+i)),
			UpdatedAt: old})
		_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:doc", "status:draft"}, ID: "DOC-" + string(rune('A'+i)), Scope: "other-scope", Title: "Doc " + string(rune('A'+i)),
			UpdatedAt: old})
	}
	// Tasks without sections — trigger should-section warnings
	for i := 0; i < 3; i++ {
		_ = s.Put(ctx, &parchment.Artifact{Labels: []string{"kind:task", "status:draft"}, ID: "TSK-" + string(rune('A'+i)), Scope: "test", Title: "Task " + string(rune('A'+i)),
			UpdatedAt: old})
	}
}

// TestBrief_NoDomainContextSection verifies the Domain Context section is gone.
func TestBrief_NoDomainContextSection(t *testing.T) {
	s, call := newBriefSetup(t)
	seedBriefNoise(t, s)
	out := call(map[string]any{"action": "brief"})
	if strings.Contains(out, "Domain Context:") {
		t.Errorf("brief must not contain Domain Context section, got:\n%s", out)
	}
}

// TestBrief_NoShouldSectionWarnings verifies per-kind section gap counts
// are not in brief — they belong in dashboard.
func TestBrief_NoShouldSectionWarnings(t *testing.T) {
	s, call := newBriefSetup(t)
	seedBriefNoise(t, s)
	out := call(map[string]any{"action": "brief"})
	if strings.Contains(out, "missing recommended sections") {
		t.Errorf("brief must not contain should-section warnings, got:\n%s", out)
	}
}

// TestBrief_StaleDraftsIsOneLiner verifies stale drafts is a count, not a list.
func TestBrief_StaleDraftsIsOneLiner(t *testing.T) {
	s, call := newBriefSetup(t)
	seedBriefNoise(t, s)
	out := call(map[string]any{"action": "brief"})
	if strings.Contains(out, "showing 10") || strings.Contains(out, "Stale Drafts (") {
		t.Errorf("brief must not show itemized stale draft list, got:\n%s", out)
	}
}

// TestBrief_SingleStaleLine verifies stale artifacts appear as exactly one
// warning line, not two overlapping counts.
func TestBrief_SingleStaleLine(t *testing.T) {
	s, call := newBriefSetup(t)
	seedBriefNoise(t, s)
	out := call(map[string]any{"action": "brief"})
	count := strings.Count(out, "stale")
	if count > 1 {
		t.Errorf("brief must have at most one 'stale' mention, got %d:\n%s", count, out)
	}
}
