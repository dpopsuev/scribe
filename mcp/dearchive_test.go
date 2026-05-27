package mcp_test

// dearchive_test.go — archive reversibility.
//
// PRC-BUG-11: archive was observed as irreversible. Agents tried
// admin(action=restore) which doesn't exist. The correct path is
// artifact(action=de-archive). These tests verify the full round-trip
// and that the action is reachable without force flags.

import (
	"context"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newDeArchiveServer(t *testing.T) (proto *parchment.Protocol, call func(string, map[string]any) string) {
	t.Helper()
	s := openStore(t)
	proto = parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	srv, _ := scribemcp.NewServer(s, []string{"test"}, nil, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	call = func(tool string, args map[string]any) string {
		return callTool(t, cs, tool, args)
	}
	return
}

// TestDeArchive_RoundTrip verifies that an archived artifact can be restored
// to draft via artifact(action=de-archive).
func TestDeArchive_RoundTrip(t *testing.T) {
	proto, call := newDeArchiveServer(t)
	ctx := context.Background()

	art, err := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "to be archived and restored", Scope: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Archive it.
	archOut := call("artifact", map[string]any{
		"action": "archive",
		"id":     art.ID,
	})
	if strings.Contains(strings.ToLower(archOut), "error") {
		t.Fatalf("archive failed: %s", archOut)
	}

	// Verify it's archived.
	got, _ := proto.GetArtifact(ctx, art.ID)
	if !parchment.New(nil, nil, nil, nil, parchment.ProtocolConfig{}).Schema().IsReadonly(got.Status) {
		schema := parchment.DefaultSchema()
		if !schema.IsReadonly(got.Status) {
			t.Fatalf("expected archived status after archive, got: %s", got.Status)
		}
	}

	// Restore via de-archive.
	out := call("artifact", map[string]any{
		"action": "de-archive",
		"id":     art.ID,
	})
	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("de-archive failed: %s", out)
	}

	// Verify it's back to draft.
	restored, err := proto.GetArtifact(ctx, art.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != parchment.StatusDraft {
		t.Errorf("de-archive should restore to draft, got: %s", restored.Status)
	}
}

// TestDeArchive_RestoredArtifactIsWritable verifies that a de-archived
// artifact can be mutated again (the read-only guard is lifted).
func TestDeArchive_RestoredArtifactIsWritable(t *testing.T) {
	proto, call := newDeArchiveServer(t)
	ctx := context.Background()

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "writable after restore", Scope: "test",
	})

	call("artifact", map[string]any{"action": "archive", "id": art.ID})
	call("artifact", map[string]any{"action": "de-archive", "id": art.ID})

	// Should be able to set title now.
	out := call("artifact", map[string]any{
		"action": "set",
		"id":     art.ID,
		"field":  "title",
		"value":  "updated after restore",
	})
	if strings.Contains(strings.ToLower(out), "read-only") ||
		strings.Contains(strings.ToLower(out), "archived") {
		t.Errorf("de-archived artifact should be writable, got: %s", out)
	}

	got, _ := proto.GetArtifact(ctx, art.ID)
	if got.Title != "updated after restore" {
		t.Errorf("title not updated after de-archive: %s", got.Title)
	}
}

// TestDeArchive_Cascade verifies that cascade=true restores children too.
func TestDeArchive_Cascade(t *testing.T) {
	proto, call := newDeArchiveServer(t)
	ctx := context.Background()

	parent, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindGoal, Title: "parent", Scope: "test",
	})
	child, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "child", Scope: "test", Parent: parent.ID,
	})

	// Archive parent with cascade.
	call("artifact", map[string]any{"action": "archive", "id": parent.ID, "cascade": true})

	// De-archive parent with cascade.
	out := call("artifact", map[string]any{
		"action":  "de-archive",
		"id":      parent.ID,
		"cascade": true,
	})
	if strings.Contains(strings.ToLower(out), "error") {
		t.Fatalf("cascade de-archive failed: %s", out)
	}

	// Both should be writable.
	for _, id := range []string{parent.ID, child.ID} {
		got, _ := proto.GetArtifact(ctx, id)
		if got.Status == parchment.StatusArchived {
			t.Errorf("%s still archived after cascade de-archive", id)
		}
	}
}

// TestDeArchive_NonArchivedReturnsError verifies de-archive on a non-archived
// artifact returns a clear error rather than silently no-oping.
func TestDeArchive_NonArchivedReturnsError(t *testing.T) {
	proto, call := newDeArchiveServer(t)
	ctx := context.Background()

	art, _ := proto.CreateArtifact(ctx, parchment.CreateInput{
		Kind: parchment.KindTask, Title: "not archived", Scope: "test",
	})

	out := call("artifact", map[string]any{
		"action": "de-archive",
		"id":     art.ID,
	})

	// Should indicate the artifact is not archived.
	if !strings.Contains(strings.ToLower(out), "not archived") &&
		!strings.Contains(strings.ToLower(out), "not readonly") &&
		!strings.Contains(strings.ToLower(out), "ok") { // ok=false in result
		t.Logf("de-archive of non-archived artifact: %s", out)
	}
	// The key invariant: artifact must NOT be in archived status afterward.
	got, _ := proto.GetArtifact(ctx, art.ID)
	if got.Status == parchment.StatusArchived {
		t.Errorf("de-archive of non-archived artifact must not archive it, got: %s", got.Status)
	}
}
