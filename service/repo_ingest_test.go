package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
)

func TestLoadPlaybook_Default(t *testing.T) {
	pb, err := service.LoadPlaybook("/tmp/nonexistent-repo-" + t.Name())
	if err != nil {
		t.Fatal(err)
	}
	if pb.Scope == "" {
		t.Fatal("want non-empty scope")
	}
	if len(pb.Sources) != 1 {
		t.Fatalf("want 1 default source, got %d", len(pb.Sources))
	}
}

func TestLoadPlaybook_Custom(t *testing.T) {
	dir := t.TempDir()
	playbook := `
version: 1
scope: test-project
sources:
  specs:
    path: requirements
    kind: intent.spec
ticket_patterns:
  - regex: '(TEST-\d+)'
    backend: linear
`
	if err := os.WriteFile(filepath.Join(dir, ".scribe.yaml"), []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	pb, err := service.LoadPlaybook(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pb.Scope != "test-project" {
		t.Fatalf("want scope 'test-project', got %q", pb.Scope)
	}
	if len(pb.Sources) != 1 {
		t.Fatalf("want 1 source, got %d", len(pb.Sources))
	}
}

func TestRepoIngest_MarkdownFiles(t *testing.T) {
	dir := t.TempDir()

	specDir := filepath.Join(dir, "specs")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	specContent := "---\ntitle: Auth Flow\nkind: intent.spec\n---\n\n## context\n\nOAuth2 implementation.\n"
	if err := os.WriteFile(filepath.Join(specDir, "auth-flow.md"), []byte(specContent), 0o644); err != nil {
		t.Fatal(err)
	}

	docsDir := filepath.Join(dir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	docContent := "---\ntitle: Architecture\n---\n\n## overview\n\nSystem design.\n"
	if err := os.WriteFile(filepath.Join(docsDir, "architecture.md"), []byte(docContent), 0o644); err != nil {
		t.Fatal(err)
	}

	playbook := "version: 1\nscope: test\nsources:\n  specs:\n    path: specs\n    kind: intent.spec\n  docs:\n    path: docs\n    kind: knowledge.note\n"
	if err := os.WriteFile(filepath.Join(dir, ".scribe.yaml"), []byte(playbook), 0o644); err != nil {
		t.Fatal(err)
	}

	store := parchment.NewMemoryStore()
	proto := parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	svc := service.New(proto, nil, []string{"test"})

	result, err := svc.RepoIngest(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 2 {
		t.Fatalf("want 2 files, got %d", result.Files)
	}
	if result.Artifacts != 2 {
		t.Fatalf("want 2 artifacts, got %d", result.Artifacts)
	}
	if result.Scope != "test" {
		t.Fatalf("want scope 'test', got %q", result.Scope)
	}
}
