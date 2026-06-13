package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// DirDetector produces a dir: label from the working directory,
// expressed relative to $HOME for readability.
type DirDetector struct{}

func (DirDetector) Detect(inputs WorkspaceInputs) []string {
	if inputs.CWD == "" {
		return nil
	}
	rel := inputs.CWD
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if r, err := filepath.Rel(home, inputs.CWD); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	return []string{"dir:" + filepath.ToSlash(rel)}
}
