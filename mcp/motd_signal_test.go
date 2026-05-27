package mcp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newMotdSetup(t *testing.T) (store parchment.Store, call func(args map[string]any) string) {
	t.Helper()
	s := openStore(t)
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "test")
	cs := connectClient(t, srv)
	return s, func(args map[string]any) string { return callTool(t, cs, "admin", args) }
}

func seedMotdNoise(t *testing.T, s parchment.Store) {
	t.Helper()
	ctx := context.Background()
	old := time.Now().Add(-30 * 24 * time.Hour)

	// Docs and refs — trigger Domain Context
	for i := 0; i < 5; i++ {
		_ = s.Put(ctx, &parchment.Artifact{
			ID: "REF-" + string(rune('A'+i)), Kind: "ref", Scope: "test",
			Status: "draft", Title: "Reference " + string(rune('A'+i)),
			UpdatedAt: old,
		})
		_ = s.Put(ctx, &parchment.Artifact{
			ID: "DOC-" + string(rune('A'+i)), Kind: "doc", Scope: "other-scope",
			Status: "draft", Title: "Doc " + string(rune('A'+i)),
			UpdatedAt: old,
		})
	}
	// Tasks without sections — trigger should-section warnings
	for i := 0; i < 3; i++ {
		_ = s.Put(ctx, &parchment.Artifact{
			ID: "TSK-" + string(rune('A'+i)), Kind: "task", Scope: "test",
			Status: "draft", Title: "Task " + string(rune('A'+i)),
			UpdatedAt: old,
		})
	}
}

// TestMotd_NoDomainContextSection verifies the Domain Context section is gone.
func TestMotd_NoDomainContextSection(t *testing.T) {
	s, call := newMotdSetup(t)
	seedMotdNoise(t, s)
	out := call(map[string]any{"action": "motd"})
	if strings.Contains(out, "Domain Context:") {
		t.Errorf("motd must not contain Domain Context section, got:\n%s", out)
	}
}

// TestMotd_NoShouldSectionWarnings verifies per-kind section gap counts
// are not in motd — they belong in dashboard.
func TestMotd_NoShouldSectionWarnings(t *testing.T) {
	s, call := newMotdSetup(t)
	seedMotdNoise(t, s)
	out := call(map[string]any{"action": "motd"})
	if strings.Contains(out, "missing recommended sections") {
		t.Errorf("motd must not contain should-section warnings, got:\n%s", out)
	}
}

// TestMotd_StaleDraftsIsOneLiner verifies stale drafts is a count, not a list.
func TestMotd_StaleDraftsIsOneLiner(t *testing.T) {
	s, call := newMotdSetup(t)
	seedMotdNoise(t, s)
	out := call(map[string]any{"action": "motd"})
	if strings.Contains(out, "showing 10") || strings.Contains(out, "Stale Drafts (") {
		t.Errorf("motd must not show itemized stale draft list, got:\n%s", out)
	}
}
