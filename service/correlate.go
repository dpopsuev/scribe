package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

var artifactIDRe = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,6}-[A-Z]{2,5}-\d+|[A-Z]{1,6}-\d+)\b`)

var completionSignals = []string{
	"passed", "done", "deployed", "shipped", "complete", "completed",
	"fixed", "merged", "closed", "resolved", "released", "delivered",
}

// CorrelateResult holds the output of a correlate operation.
type CorrelateResult struct {
	Found   []*parchment.Artifact
	Missing []*parchment.Artifact
	Drift   []*parchment.Artifact
	Scope   string
}

// Correlate parses evidence text for artifact IDs and compares against live artifacts.
func (s *Service) Correlate(ctx context.Context, evidence, scope string) (*CorrelateResult, error) {
	if evidence == "" {
		return nil, fmt.Errorf("evidence is required") //nolint:err113 // agent-facing, inline message is the contract
	}
	if scope == "" && len(s.HomeScopes) > 0 {
		scope = s.HomeScopes[0]
	}

	evidenceLower := strings.ToLower(evidence)
	mentionedIDs := ExtractIDs(evidence)

	var correlateLabels []string
	if scope != "" {
		correlateLabels = []string{parchment.LabelPrefixScope + scope}
	}
	all, err := s.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: correlateLabels})
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	schema := s.Proto.Schema()

	byID := make(map[string]*parchment.Artifact, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}

	var found []*parchment.Artifact
	for id := range mentionedIDs {
		if a, ok := byID[id]; ok {
			found = append(found, a)
		}
	}

	var missing []*parchment.Artifact
	for _, a := range all {
		if schema.IsTerminal(a.Label(parchment.LabelPrefixStatus)) {
			continue
		}
		if !mentionedIDs[a.ID] {
			missing = append(missing, a)
		}
	}

	var drift []*parchment.Artifact
	for _, a := range found {
		if schema.IsTerminal(a.Label(parchment.LabelPrefixStatus)) {
			continue
		}
		if EvidenceImpliesComplete(evidenceLower, strings.ToLower(a.ID)) {
			drift = append(drift, a)
		}
	}

	return &CorrelateResult{Found: found, Missing: missing, Drift: drift, Scope: scope}, nil
}

// ExtractIDs returns the set of unique uppercase artifact IDs found in text.
func ExtractIDs(text string) map[string]bool {
	matches := artifactIDRe.FindAllString(text, -1)
	out := make(map[string]bool, len(matches))
	for _, m := range matches {
		out[strings.ToUpper(m)] = true
	}
	return out
}

// EvidenceImpliesComplete returns true when a completion signal appears near the ID.
func EvidenceImpliesComplete(evidenceLower, idLower string) bool {
	idx := strings.Index(evidenceLower, idLower)
	if idx < 0 {
		return false
	}
	start := idx - 60
	if start < 0 {
		start = 0
	}
	end := idx + len(idLower) + 60
	if end > len(evidenceLower) {
		end = len(evidenceLower)
	}
	window := evidenceLower[start:end]
	for _, sig := range completionSignals {
		if strings.Contains(window, sig) {
			return true
		}
	}
	return false
}

// RenderCorrelateReport formats the correlate result as a human-readable string.
func RenderCorrelateReport(r *CorrelateResult) string {
	var b strings.Builder
	if r.Scope != "" {
		fmt.Fprintf(&b, "Scope: %s\n\n", r.Scope)
	}
	fmt.Fprintf(&b, "Found (%d):\n", len(r.Found))
	if len(r.Found) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range r.Found {
		fmt.Fprintf(&b, "  %s [%s] %s\n", a.ID, a.Label(parchment.LabelPrefixStatus), a.Title)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Missing / unaccounted (%d):\n", len(r.Missing))
	if len(r.Missing) == 0 {
		b.WriteString("  (none — all active artifacts mentioned)\n")
	}
	for _, a := range r.Missing {
		fmt.Fprintf(&b, "  %s [%s] %s\n", a.ID, a.Label(parchment.LabelPrefixStatus), a.Title)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "Status drift (%d):\n", len(r.Drift))
	if len(r.Drift) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range r.Drift {
		fmt.Fprintf(&b, "  %s [%s] %s — evidence implies complete\n", a.ID, a.Label(parchment.LabelPrefixStatus), a.Title)
	}
	if len(r.Drift) > 0 || len(r.Missing) > 0 {
		b.WriteString("\nNext:\n")
		for _, a := range r.Drift {
			fmt.Fprintf(&b, "  set status=complete on %s (%s)\n", a.ID, a.Title)
		}
		if len(r.Missing) > 0 {
			fmt.Fprintf(&b, "  %d active artifact(s) not in evidence — verify or update status\n", len(r.Missing))
		}
	}
	return b.String()
}
