package service_test

import (
	"context"
	"os"
	"path/filepath"
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
	if art.Kind != "task" {
		t.Errorf("kind = %q, want task", art.Kind)
	}
	if art.Priority != "high" {
		t.Errorf("priority = %q, want high", art.Priority)
	}
	if len(art.Labels) == 0 || art.Labels[0] != "go" {
		t.Errorf("labels = %v, want [go security]", art.Labels)
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
	arts, _ := svc.Proto.Store().List(ctx, parchment.Filter{Kind: "note"})
	if len(arts) == 0 {
		t.Fatal("no artifacts after sync")
	}
	if arts[0].ID == "" {
		t.Error("derived ID should not be empty")
	}
}
