package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// GoModuleDetector reads the nearest go.mod and produces a module: label.
// Example: module:github.com/dpopsuev/parchment
// Only active when a go.mod is found — other language projects are unaffected.
type GoModuleDetector struct{}

func (GoModuleDetector) Detect(inputs WorkspaceInputs) []string {
	if inputs.CWD == "" {
		return nil
	}
	mod := findGoModule(inputs.CWD)
	if mod == "" {
		return nil
	}
	return []string{"module:" + mod}
}

// findGoModule walks up from cwd looking for a go.mod file,
// returns the module path declared on the "module" line.
func findGoModule(cwd string) string {
	dir := cwd
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // path from dir walk
		if err == nil {
			if mod := parseGoMod(data); mod != "" {
				return mod
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// parseGoMod extracts the module path from go.mod content.
func parseGoMod(data []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}
