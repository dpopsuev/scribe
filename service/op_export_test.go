package service_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestOpExport_SingleIDMarkdown(t *testing.T) {
	svc := newTestService(t, "exp")
	ctx := context.Background()

	spec, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels: []string{"kind:intent.spec", "scope:exp", "work.draft"},
		Title:  "Auth Spec",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:effort.task", "scope:exp", "work.active", "priority:high"},
		Title:    "Implement auth",
		Sections: []parchment.Section{{Name: "context", Text: "wire login"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Proto.LinkArtifacts(ctx, task.ID, "implements", []string{spec.ID}, 0); err != nil {
		t.Fatal(err)
	}

	op := service.Find("export")
	raw, _ := json.Marshal(map[string]any{"id": task.ID, "format": "markdown"})
	out, err := op.Run(ctx, svc, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "---\n") || !strings.Contains(out, "kind: effort.task") {
		t.Fatalf("missing frontmatter: %s", out)
	}
	if !strings.Contains(out, "[[implements::Auth Spec]]") {
		t.Fatalf("missing typed wikilink: %s", out)
	}
}

func TestExportScope_SkipUnchangedAndConflict(t *testing.T) {
	svc := newTestService(t, "inc")
	ctx := context.Background()

	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Labels:   []string{"kind:knowledge.note", "scope:inc", "note.fleeting"},
		Title:    "Inc Note",
		Sections: []parchment.Section{{Name: "body", Text: "v1"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	outDir := t.TempDir()
	n, err := svc.ExportScope(ctx, "inc", outDir, false)
	if err != nil || n != 1 {
		t.Fatalf("first export n=%d err=%v", n, err)
	}

	n2, err := svc.ExportScope(ctx, "inc", outDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("expected skip, wrote %d", n2)
	}

	entries, _ := os.ReadDir(outDir)
	path := filepath.Join(outDir, entries[0].Name())
	if err := os.WriteFile(path, []byte("---\nid: hacked\n---\n\n## body\n\nhuman edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	n3, err := svc.ExportScope(ctx, "inc", outDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if n3 != 1 {
		t.Fatalf("expected conflict write count 1, got %d", n3)
	}
	if _, err := os.Stat(path + ".conflict.md"); err != nil {
		t.Fatalf("expected conflict sidecar: %v", err)
	}
	_ = art
}
