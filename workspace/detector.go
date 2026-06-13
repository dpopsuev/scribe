// Package workspace provides detectors that read the local environment and
// produce context labels for a Scribe session (git:, dir:, etc.).
// Each Detector is independent and opt-in. Detectors are composed by Detect,
// which runs all of them and merges their labels.
package workspace

// WorkspaceInputs carries the raw facts a client knows about its environment.
// For the stdio transport, the server populates this from its own filesystem.
// For the HTTP transport, the client sends cwd and optionally git_remote in
// the MCP initialize _meta field; the server reads them here.
type WorkspaceInputs struct {
	CWD       string // working directory path
	GitRemote string // git remote URL (optional — populated by GitDetector if empty)
}

// Detector reads WorkspaceInputs and produces context labels.
type Detector interface {
	Detect(inputs WorkspaceInputs) []string
}

// Detect runs all detectors against inputs and returns the merged label set.
func Detect(inputs WorkspaceInputs, detectors []Detector) []string {
	labels := make([]string, 0, len(detectors)*2)
	for _, d := range detectors {
		labels = append(labels, d.Detect(inputs)...)
	}
	return labels
}

// DefaultDetectors returns the standard set used at session start.
func DefaultDetectors() []Detector {
	return []Detector{
		DirDetector{},
		GitDetector{},
	}
}
