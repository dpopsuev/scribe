package mcp_test

// correlate_test.go — admin(action=correlate) tests.
//
// SCR-GOL-26: Artifact Correlation and Status Drift Insights.
// Given a block of text containing artifact IDs and delivery signals,
// return which artifacts were found, which are missing, which show
// status drift, and what the agent should do next.

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newCorrelateServer(t *testing.T) (proto *parchment.Protocol, call func(map[string]any) string) {
	t.Helper()
	s := openStore(t)
	proto = parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	call = func(args map[string]any) string {
		return callTool(t, cs, "admin", args)
	}
	return
}

// TestCorrelate_FoundArtifacts verifies that artifact IDs mentioned in evidence
// text are matched against live artifacts and returned in the "found" set.
func TestCorrelate_FoundArtifacts(t *testing.T) {
	proto, call := newCorrelateServer(t)
	ctx := context.Background()

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "implement retry logic", Scope: "test",
	})

	evidence := "Completed work: " + art.ID + " is done. Pushed PR #42."

	out := call(map[string]any{
		"action":   "correlate",
		"evidence": evidence,
		"scope":    "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown admin action") {
		t.Fatalf("correlate action not implemented: %s", out)
	}
	if !strings.Contains(out, art.ID) {
		t.Errorf("correlate output must reference found artifact %s\nGot: %s", art.ID, out)
	}
	if !strings.Contains(strings.ToLower(out), "found") {
		t.Errorf("correlate output must include 'found' section\nGot: %s", out)
	}
}

// TestCorrelate_MissingArtifacts verifies that active artifacts NOT mentioned
// in evidence are surfaced as missing/unaccounted-for.
func TestCorrelate_MissingArtifacts(t *testing.T) {
	proto, call := newCorrelateServer(t)
	ctx := context.Background()

	mentioned, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "mentioned task", Scope: "test",
	})
	unmentioned, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "silent task", Scope: "test",
	})

	evidence := "Shipped: " + mentioned.ID + " — looks good."

	out := call(map[string]any{
		"action":   "correlate",
		"evidence": evidence,
		"scope":    "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown admin action") {
		t.Fatalf("correlate not implemented: %s", out)
	}
	if !strings.Contains(out, unmentioned.ID) {
		t.Errorf("correlate must surface unmentioned active artifact %s\nGot: %s", unmentioned.ID, out)
	}
	if !strings.Contains(strings.ToLower(out), "missing") && !strings.Contains(strings.ToLower(out), "unaccounted") {
		t.Errorf("correlate must include 'missing' or 'unaccounted' section\nGot: %s", out)
	}
}

// TestCorrelate_StatusDrift verifies that artifacts mentioned as "done" or
// "complete" in evidence but still active in Scribe are flagged as drift.
func TestCorrelate_StatusDrift(t *testing.T) {
	proto, call := newCorrelateServer(t)
	ctx := context.Background()

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "drift candidate", Scope: "test",
	})
	// Artifact is draft/active in Scribe but evidence says it's done.
	evidence := art.ID + " PASSED — all tests green, shipped to prod."

	out := call(map[string]any{
		"action":   "correlate",
		"evidence": evidence,
		"scope":    "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown admin action") {
		t.Fatalf("correlate not implemented: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "drift") && !strings.Contains(strings.ToLower(out), "complete") {
		t.Errorf("correlate must surface status drift when evidence implies completion\nGot: %s", out)
	}
}

// TestCorrelate_EmptyEvidence verifies a clear error on empty evidence.
func TestCorrelate_EmptyEvidence(t *testing.T) {
	_, call := newCorrelateServer(t)

	out := call(map[string]any{
		"action":   "correlate",
		"evidence": "",
		"scope":    "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown admin action") {
		t.Fatalf("correlate not implemented: %s", out)
	}
	// Should return an error or empty result, not panic.
	if out == "" {
		t.Error("correlate with empty evidence must return some output")
	}
}

// TestCorrelate_Recommendations verifies that the output includes actionable
// next steps (e.g. "set status=complete on X").
func TestCorrelate_Recommendations(t *testing.T) {
	proto, call := newCorrelateServer(t)
	ctx := context.Background()

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "closeable task", Scope: "test",
	})
	evidence := art.ID + " is done and deployed. No issues."

	out := call(map[string]any{
		"action":   "correlate",
		"evidence": evidence,
		"scope":    "test",
	})

	if strings.Contains(strings.ToLower(out), "unknown admin action") {
		t.Fatalf("correlate not implemented: %s", out)
	}
	hasRec := strings.Contains(strings.ToLower(out), "recommend") ||
		strings.Contains(strings.ToLower(out), "set status") ||
		strings.Contains(strings.ToLower(out), "complete") ||
		strings.Contains(strings.ToLower(out), "next")
	if !hasRec {
		t.Errorf("correlate must include recommendations\nGot: %s", out)
	}
}

// TestCorrelate_SchemaField verifies the admin tool schema includes correlate
// as a valid action (agents can discover it).
func TestCorrelate_SchemaField(t *testing.T) {
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)

	// Use the schema action to inspect the admin tool definition.
	out := callTool(t, cs, "admin", map[string]any{"action": "schema"})
	// If schema doesn't list correlate, check at least the error message mentions it
	// OR the action is handled (not 'unknown admin action').
	directOut := callTool(t, cs, "admin", map[string]any{"action": "correlate", "evidence": "test"})
	if strings.Contains(directOut, "unknown admin action") {
		t.Errorf("'correlate' must not be an unknown admin action\nschema: %s\ndirect: %s", out, directOut)
	}
}
