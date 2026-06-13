package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// GitDetector produces a git: label from the repository's origin remote URL.
// If inputs.GitRemote is already set (HTTP transport — client provided it),
// it is used directly. Otherwise the detector walks up from inputs.CWD to
// find .git/config (stdio transport — server has filesystem access).
type GitDetector struct{}

func (GitDetector) Detect(inputs WorkspaceInputs) []string {
	remote := inputs.GitRemote
	if remote == "" && inputs.CWD != "" {
		remote = findGitOrigin(inputs.CWD)
	}
	if remote == "" {
		return nil
	}
	return []string{"git:" + NormalizeGitURL(remote)}
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
				break
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
//	git@github.com:dpopsuev/locus.git  →  github.com/dpopsuev/locus
//	https://github.com/dpopsuev/locus  →  github.com/dpopsuev/locus
//	ssh://git@github.com/dpopsuev/locus →  github.com/dpopsuev/locus
func NormalizeGitURL(url string) string {
	url = strings.TrimSuffix(url, ".git")
	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
		return url
	}
	for _, prefix := range []string{"https://", "http://", "ssh://git@", "ssh://"} {
		if strings.HasPrefix(url, prefix) {
			return strings.TrimPrefix(url, prefix)
		}
	}
	return url
}
