// Package workspace provides detectors that read the local environment and
// produce context labels for a Scribe session (git:, dir:, etc.).
// Each Detector is independent and opt-in. Detectors are composed by Detect,
// which runs all of them and merges their labels.
package workspace

// Detector reads the working directory and produces context labels.
type Detector interface {
	Detect(cwd string) []string
}

// Detect runs all detectors against cwd and returns the merged label set.
func Detect(cwd string, detectors []Detector) []string {
	labels := make([]string, 0, len(detectors)*2)
	for _, d := range detectors {
		labels = append(labels, d.Detect(cwd)...)
	}
	return labels
}

// DefaultDetectors returns the standard set used by the stdio server.
func DefaultDetectors() []Detector {
	return []Detector{
		DirDetector{},
		GitDetector{},
	}
}
