package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScopeForDir(t *testing.T) {
	home, _ := os.UserHomeDir()

	cfg := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"scribe": {Path: "/home/user/Workspace/scribe"},
			"pi":     {Path: "/home/user/Workspace/pi-mono"},
			"foni":   {Path: "/home/user/Projects/foni"},
		},
	}

	cases := []struct {
		dir  string
		want string
	}{
		{"/home/user/Workspace/scribe", "scribe"},
		{"/home/user/Workspace/scribe/cmd/scribe", "scribe"},
		{"/home/user/Workspace/pi-mono/packages/agent", "pi"},
		{"/home/user/Projects/foni", "foni"},
		{"/home/user/Projects/foni/pkg/something", "foni"},
		{"/home/user/other", ""},
		{"/tmp", ""},
	}

	for _, tc := range cases {
		got := cfg.ScopeForDir(tc.dir)
		if got != tc.want {
			t.Errorf("ScopeForDir(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}

	// tilde expansion
	cfgTilde := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"home": {Path: "~/Projects/home"},
		},
	}
	wantDir := filepath.Join(home, "Projects", "home", "sub")
	if got := cfgTilde.ScopeForDir(wantDir); got != "home" {
		t.Errorf("ScopeForDir with tilde path: got %q, want %q", got, "home")
	}
}

func TestScopeForDir_LongestPrefixWins(t *testing.T) {
	// Nested paths are rejected by validation, but longest-prefix logic
	// should still return the most specific match if they somehow exist.
	cfg := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"workspace": {Path: "/home/user/Workspace"},
			"scribe":    {Path: "/home/user/Workspace/scribe"},
		},
	}
	got := cfg.ScopeForDir("/home/user/Workspace/scribe/pkg")
	if got != "scribe" {
		t.Errorf("expected longest-prefix winner %q, got %q", "scribe", got)
	}
}

func TestScopeForDir_NoPathsConfigured(t *testing.T) {
	cfg := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"noop": {Key: "NOP"},
		},
	}
	if got := cfg.ScopeForDir("/anywhere"); got != "" {
		t.Errorf("expected empty string when no paths configured, got %q", got)
	}
}

func TestScopeForDir_Empty(t *testing.T) {
	cfg := &Config{}
	if got := cfg.ScopeForDir("/anything"); got != "" {
		t.Errorf("expected empty string for empty config, got %q", got)
	}
}

func TestValidateScopePaths_Overlap(t *testing.T) {
	cfg := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"parent": {Path: "/home/user/Workspace"},
			"child":  {Path: "/home/user/Workspace/scribe"},
		},
	}
	if err := cfg.validateScopePaths(); err == nil {
		t.Error("expected error for nested paths, got nil")
	}
}

func TestValidateScopePaths_NoOverlap(t *testing.T) {
	cfg := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"a": {Path: "/home/user/Workspace/scribe"},
			"b": {Path: "/home/user/Workspace/pi-mono"},
			"c": {Path: "/home/user/Projects/foni"},
		},
	}
	if err := cfg.validateScopePaths(); err != nil {
		t.Errorf("unexpected error for non-overlapping paths: %v", err)
	}
}

func TestValidateScopePaths_MissingPathsIgnored(t *testing.T) {
	cfg := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"noop": {Key: "NOP"},
		},
	}
	if err := cfg.validateScopePaths(); err != nil {
		t.Errorf("unexpected error when paths are empty: %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"~", home},
		{"~/foo/bar", filepath.Join(home, "foo", "bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, tc := range cases {
		got := expandHome(tc.in)
		if got != tc.want {
			t.Errorf("expandHome(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWorkspaceScopesFor(t *testing.T) {
	cfg := &Config{
		ScopeConfigs: map[string]ScopeConfig{
			"scribe": {Path: "/Workspace/scribe"},
			"foni":   {Path: "/Workspace/foni"},
		},
	}

	cases := []struct {
		workspace string
		want      []string
	}{
		{"scribe", []string{"scribe"}},              // named scope — single element
		{"foni,scribe", []string{"foni", "scribe"}}, // comma-separated literal list
		{"unknown", []string{"unknown"}},            // unknown — treated as literal
		{"a,b,c", []string{"a", "b", "c"}},          // multi-element literal
	}
	for _, tc := range cases {
		got := cfg.WorkspaceScopesFor(tc.workspace)
		if len(got) != len(tc.want) {
			t.Errorf("WorkspaceScopesFor(%q) = %v, want %v", tc.workspace, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("WorkspaceScopesFor(%q)[%d] = %q, want %q", tc.workspace, i, got[i], tc.want[i])
			}
		}
	}
}
