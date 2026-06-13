package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// GitDetector walks up the directory tree from cwd, finds the nearest
// .git/config, and produces a git: label from the origin remote URL.
type GitDetector struct{}

func (GitDetector) Detect(cwd string) []string {
	url := findGitOrigin(cwd)
	if url == "" {
		return nil
	}
	return []string{"git:" + NormalizeGitURL(url)}
}

// findGitOrigin walks up from cwd looking for .git/config.
func findGitOrigin(cwd string) string {
	dir := cwd
	for {
		data, err := os.ReadFile(filepath.Join(dir, ".git", "config")) //nolint:gosec // path constructed from dir walk, not user input
		if err == nil {
			if url := parseOriginURL(data); url != "" {
				return url
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

// parseOriginURL extracts the url= value from [remote "origin"] in a .git/config.
func parseOriginURL(data []byte) string {
	inOrigin := false
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == `[remote "origin"]` {
			inOrigin = true
			continue
		}
		if inOrigin {
			if strings.HasPrefix(line, "[") {
				break // next section — origin had no url
			}
			if strings.HasPrefix(line, "url") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					return strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return ""
}

// NormalizeGitURL converts any git remote URL form to host/owner/repo:
//
//	git@github.com:dpopsuev/locus.git → github.com/dpopsuev/locus
//	https://github.com/dpopsuev/locus.git → github.com/dpopsuev/locus
//	ssh://git@github.com/dpopsuev/locus → github.com/dpopsuev/locus
func NormalizeGitURL(url string) string {
	url = strings.TrimSuffix(url, ".git")
	// SCP-style: git@github.com:owner/repo
	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
		return url
	}
	// https:// or ssh://
	for _, prefix := range []string{"https://", "http://", "ssh://git@", "ssh://"} {
		if strings.HasPrefix(url, prefix) {
			return strings.TrimPrefix(url, prefix)
		}
	}
	return url
}
