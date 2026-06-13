package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dpopsuev/scribe/workspace"
)

func TestDirDetector(t *testing.T) {
	cwd := t.TempDir()
	labels := workspace.DirDetector{}.Detect(workspace.WorkspaceInputs{CWD: cwd})
	if len(labels) != 1 {
		t.Fatalf("want 1 label, got %d: %v", len(labels), labels)
	}
	if !strings.HasPrefix(labels[0], "dir:") {
		t.Errorf("label should start with dir:, got %q", labels[0])
	}
}

func TestDirDetector_EmptyCWD(t *testing.T) {
	labels := workspace.DirDetector{}.Detect(workspace.WorkspaceInputs{})
	if len(labels) != 0 {
		t.Errorf("empty CWD should produce no labels, got %v", labels)
	}
}

func TestGitDetector_UsesProvidedRemote(t *testing.T) {
	// HTTP transport: client provides git_remote directly — no filesystem walk.
	labels := workspace.GitDetector{}.Detect(workspace.WorkspaceInputs{
		CWD:       t.TempDir(),
		GitRemote: "git@github.com:dpopsuev/locus.git",
	})
	if len(labels) != 1 || labels[0] != "git:github.com/dpopsuev/locus" {
		t.Errorf("want git:github.com/dpopsuev/locus, got %v", labels)
	}
}

func TestGitDetector_WalksFilesystem(t *testing.T) {
	// stdio transport: no git_remote provided — detector reads .git/config.
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(gitDir, "config"), []byte(`[remote "origin"]
	url = git@github.com:dpopsuev/locus.git
`), 0o644) //nolint:errcheck // test setup

	labels := workspace.GitDetector{}.Detect(workspace.WorkspaceInputs{CWD: dir})
	if len(labels) != 1 || labels[0] != "git:github.com/dpopsuev/locus" {
		t.Errorf("want git:github.com/dpopsuev/locus, got %v", labels)
	}
}

func TestGitDetector_WalksUp(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(gitDir, "config"), []byte(`[remote "origin"]
	url = https://github.com/dpopsuev/scribe.git
`), 0o644) //nolint:errcheck // test setup

	sub := filepath.Join(root, "cmd", "scribe")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	labels := workspace.GitDetector{}.Detect(workspace.WorkspaceInputs{CWD: sub})
	if len(labels) != 1 || labels[0] != "git:github.com/dpopsuev/scribe" {
		t.Errorf("want git:github.com/dpopsuev/scribe, got %v", labels)
	}
}

func TestGitDetector_NoGit(t *testing.T) {
	labels := workspace.GitDetector{}.Detect(workspace.WorkspaceInputs{CWD: t.TempDir()})
	if len(labels) != 0 {
		t.Errorf("want no labels, got %v", labels)
	}
}

func TestDetect_Compose(t *testing.T) {
	dir := t.TempDir()
	labels := workspace.Detect(workspace.WorkspaceInputs{CWD: dir}, workspace.DefaultDetectors())
	hasDir := false
	for _, l := range labels {
		if strings.HasPrefix(l, "dir:") {
			hasDir = true
		}
	}
	if !hasDir {
		t.Errorf("expected dir: label in %v", labels)
	}
}

func TestGoModuleDetector_FindsModule(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/dpopsuev/locus\n\ngo 1.22\n"), 0o644) //nolint:errcheck // test setup

	labels := workspace.GoModuleDetector{}.Detect(workspace.WorkspaceInputs{CWD: dir})
	if len(labels) != 1 || labels[0] != "module:github.com/dpopsuev/locus" {
		t.Errorf("want module:github.com/dpopsuev/locus, got %v", labels)
	}
}

func TestGoModuleDetector_WalksUp(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/dpopsuev/scribe\n"), 0o644) //nolint:errcheck // test setup
	sub := filepath.Join(root, "cmd", "scribe")
	os.MkdirAll(sub, 0o755) //nolint:errcheck // test setup

	labels := workspace.GoModuleDetector{}.Detect(workspace.WorkspaceInputs{CWD: sub})
	if len(labels) != 1 || labels[0] != "module:github.com/dpopsuev/scribe" {
		t.Errorf("want module:github.com/dpopsuev/scribe, got %v", labels)
	}
}

func TestGoModuleDetector_NoGoMod(t *testing.T) {
	labels := workspace.GoModuleDetector{}.Detect(workspace.WorkspaceInputs{CWD: t.TempDir()})
	if len(labels) != 0 {
		t.Errorf("want no labels, got %v", labels)
	}
}

func TestTimeDetector_ProducesQuarterAndWeek(t *testing.T) {
	fixed := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC) // Q2, W24
	labels := workspace.TimeDetector{}.Detect(workspace.WorkspaceInputs{Now: fixed})
	if len(labels) != 2 {
		t.Fatalf("want 2 labels, got %d: %v", len(labels), labels)
	}
	if labels[0] != "time:2026.q2" {
		t.Errorf("want time:2026.q2, got %q", labels[0])
	}
	if labels[1] != "time:2026.w24" {
		t.Errorf("want time:2026.w24, got %q", labels[1])
	}
}

func TestTimeDetector_Q4(t *testing.T) {
	fixed := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	labels := workspace.TimeDetector{}.Detect(workspace.WorkspaceInputs{Now: fixed})
	if labels[0] != "time:2026.q4" {
		t.Errorf("want time:2026.q4, got %q", labels[0])
	}
}

func TestNormalizeGitURL(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"git@github.com:dpopsuev/locus.git", "github.com/dpopsuev/locus"},
		{"https://github.com/dpopsuev/locus.git", "github.com/dpopsuev/locus"},
		{"https://github.com/dpopsuev/locus", "github.com/dpopsuev/locus"},
		{"ssh://git@github.com/dpopsuev/locus.git", "github.com/dpopsuev/locus"},
	}
	for _, tc := range cases {
		got := workspace.NormalizeGitURL(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeGitURL(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
