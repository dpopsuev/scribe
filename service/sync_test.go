package service_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
)

func writeMD(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncDir_ImportsArtifacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)
	dir := t.TempDir()

	writeMD(t, dir, "fix-auth.md", `---
id: SCR-TSK-001
kind: task
title: Fix auth bug
scope: scribe
status: draft
priority: high
labels: [go, security]
---

## context

JWT expiry not checked.

## acceptance

Given expired token, returns 401.
`)

	n, err := svc.SyncDir(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 artifact, got %d", n)
	}

	art, err := svc.Proto.GetArtifact(ctx, "SCR-TSK-001")
	if err != nil {
		t.Fatal(err)
	}
	if art.Label(parchment.LabelPrefixKind) != "task" {
		t.Errorf("kind = %q, want task", art.Label(parchment.LabelPrefixKind))
	}
	if art.Label(parchment.LabelPrefixPriority) != "high" {
		t.Errorf("priority = %q, want high", art.Label(parchment.LabelPrefixPriority))
	}
	if !slices.Contains(art.Labels, "go") || !slices.Contains(art.Labels, "security") {
		t.Errorf("labels = %v, want to contain go and security", art.Labels)
	}
	var ctxText, acceptText string
	for _, sec := range art.Sections {
		switch sec.Name {
		case "context":
			ctxText = sec.Text
		case "acceptance":
			acceptText = sec.Text
		}
	}
	if ctxText == "" {
		t.Error("expected context section, got empty")
	}
	if acceptText == "" {
		t.Error("expected acceptance section, got empty")
	}
}

func TestSyncDir_PrunesDeletedFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)
	dir := t.TempDir()

	writeMD(t, dir, "a.md", "---\nid: SYN-A\nkind: note\ntitle: A\n---\n")
	writeMD(t, dir, "b.md", "---\nid: SYN-B\nkind: note\ntitle: B\n---\n")

	if _, err := svc.SyncDir(ctx, dir); err != nil {
		t.Fatal(err)
	}

	// Delete b.md, sync again — SYN-B should be pruned
	if err := os.Remove(filepath.Join(dir, "b.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SyncDir(ctx, dir); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Proto.GetArtifact(ctx, "SYN-A"); err != nil {
		t.Error("SYN-A should still exist")
	}
	if _, err := svc.Proto.GetArtifact(ctx, "SYN-B"); err == nil {
		t.Error("SYN-B should have been pruned")
	}
}

func TestExportScope_WritesFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t, "scribe")

	_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:note", "architecture"},
		Title:  "Design notes",
		Sections: []parchment.Section{{Name: "body", Text: "Key insight here."}},
	})
	if err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	n, err := svc.ExportScope(ctx, "scribe", outDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 file, got %d", n)
	}

	entries, _ := os.ReadDir(outDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 .md file, got %d", len(entries))
	}
	content, _ := os.ReadFile(filepath.Join(outDir, entries[0].Name()))
	s := string(content)
	if !strings.Contains(s, "kind: note") {
		t.Error("missing kind in frontmatter")
	}
	if !strings.Contains(s, "## body") {
		t.Error("missing body section heading")
	}
	if !strings.Contains(s, "Key insight here.") {
		t.Error("missing section text")
	}
}

func TestRoundTrip_ExportThenSync(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t, "test")

	// Create two artifacts
	for _, title := range []string{"Alpha note", "Beta note"} {
		_, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Labels: []string{"kind:note"},
			Title:  title,
			Sections: []parchment.Section{{Name: "body", Text: title + " body."}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	outDir := t.TempDir()
	n, err := svc.ExportScope(ctx, "test", outDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("export: expected 2, got %d", n)
	}

	// Sync the exported files into a fresh store
	svc2 := newTestService(t, "test")
	n2, err := svc2.SyncDir(ctx, outDir)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 2 {
		t.Fatalf("sync: expected 2, got %d", n2)
	}

	// Both artifacts should be retrievable by ID
	arts, _ := svc2.Proto.Store().List(ctx, parchment.Filter{Labels: []string{"kind:note"}})
	if len(arts) != 2 {
		t.Errorf("expected 2 notes after round-trip, got %d", len(arts))
	}
}

func TestSyncDir_DerivedIDWhenNoFrontmatter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc := newTestService(t)
	dir := t.TempDir()

	writeMD(t, dir, "my-rule.md", "# My Rule\n\nSome content.\n")

	n, err := svc.SyncDir(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}

	// List via store directly (SyncDir sets scope=global, outside homeScopes)
	arts, _ := svc.Proto.Store().List(ctx, parchment.Filter{Labels: []string{"kind:note"}})
	if len(arts) == 0 {
		t.Fatal("no artifacts after sync")
	}
	if arts[0].ID == "" {
		t.Error("derived ID should not be empty")
	}
}
