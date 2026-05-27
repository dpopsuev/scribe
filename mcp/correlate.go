package mcp

// correlate.go — admin(action=correlate) implementation.
//
// Parses freeform evidence text (CI logs, PR descriptions, standup notes)
// for artifact IDs. Matches them against live artifacts to surface:
//   - Found: IDs in evidence that resolve to real artifacts
//   - Missing: active artifacts in scope not mentioned in evidence
//   - Drift: found artifacts that evidence implies are complete but Scribe still shows active
//   - Recommendations: concrete next actions for the agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// artifactIDRe matches scoped IDs of the form SCOPE-KIND-SEQ (e.g. PROJ-TSK-42)
// and bare prefix IDs of the form PREFIX-SEQ (e.g. T-001).
var artifactIDRe = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,6}-[A-Z]{2,5}-\d+|[A-Z]{1,6}-\d+)\b`)

// completionSignals are words in evidence that imply an artifact is done.
var completionSignals = []string{
	"passed", "done", "deployed", "shipped", "complete", "completed",
	"fixed", "merged", "closed", "resolved", "released", "delivered",
}

func (h *handler) handleCorrelate(ctx context.Context, in adminInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: consistent with all other admin handlers
	if in.Evidence == "" {
		return text("evidence is required for correlate"), nil, nil
	}

	scope := in.Scope
	if scope == "" && len(h.homeScopes) > 0 {
		scope = h.homeScopes[0]
	}

	// Extract all candidate IDs from the evidence text.
	evidenceLower := strings.ToLower(in.Evidence)
	mentionedIDs := extractIDs(in.Evidence)

	// Load active (non-terminal) artifacts in scope.
	all, err := h.proto.ListArtifacts(ctx, parchment.ListInput{Scope: scope})
	if err != nil {
		return text(fmt.Sprintf("correlate: list artifacts: %v", err)), nil, nil
	}
	schema := h.proto.Schema()

	byID := make(map[string]*parchment.Artifact, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}

	// --- Found: mentioned IDs that resolve to real artifacts ---
	var found []*parchment.Artifact
	for id := range mentionedIDs {
		if a, ok := byID[id]; ok {
			found = append(found, a)
		}
	}

	// --- Missing: active artifacts not mentioned in evidence ---
	var missing []*parchment.Artifact
	for _, a := range all {
		if schema.IsTerminal(a.Status) {
			continue
		}
		if !mentionedIDs[a.ID] {
			missing = append(missing, a)
		}
	}

	// --- Drift: found artifacts evidence implies complete but Scribe shows active ---
	var drift []*parchment.Artifact
	for _, a := range found {
		if schema.IsTerminal(a.Status) {
			continue
		}
		// Check if evidence around this ID contains a completion signal.
		if evidenceImpliesComplete(evidenceLower, strings.ToLower(a.ID)) {
			drift = append(drift, a)
		}
	}

	return text(renderCorrelateReport(found, missing, drift, scope)), nil, nil
}

// extractIDs returns the set of unique uppercase artifact IDs found in text.
func extractIDs(text string) map[string]bool {
	matches := artifactIDRe.FindAllString(text, -1)
	out := make(map[string]bool, len(matches))
	for _, m := range matches {
		out[strings.ToUpper(m)] = true
	}
	return out
}

// evidenceImpliesComplete returns true when the evidence contains a completion
// signal word within 120 characters of the artifact ID.
func evidenceImpliesComplete(evidenceLower, idLower string) bool {
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

func renderCorrelateReport(
	found, missing, drift []*parchment.Artifact,
	scope string,
) string {
	var b strings.Builder

	if scope != "" {
		fmt.Fprintf(&b, "Scope: %s\n\n", scope)
	}

	// Found
	fmt.Fprintf(&b, "Found (%d):\n", len(found))
	if len(found) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range found {
		fmt.Fprintf(&b, "  %s [%s] %s\n", a.ID, a.Status, a.Title)
	}

	// Missing
	b.WriteString("\n")
	fmt.Fprintf(&b, "Missing / unaccounted (%d):\n", len(missing))
	if len(missing) == 0 {
		b.WriteString("  (none — all active artifacts mentioned)\n")
	}
	for _, a := range missing {
		fmt.Fprintf(&b, "  %s [%s] %s\n", a.ID, a.Status, a.Title)
	}

	// Drift
	b.WriteString("\n")
	fmt.Fprintf(&b, "Status drift (%d):\n", len(drift))
	if len(drift) == 0 {
		b.WriteString("  (none)\n")
	}
	for _, a := range drift {
		fmt.Fprintf(&b, "  %s [%s] %s — evidence implies complete\n", a.ID, a.Status, a.Title)
	}

	// Recommendations
	if len(drift) > 0 || len(missing) > 0 {
		b.WriteString("\nNext:\n")
		for _, a := range drift {
			fmt.Fprintf(&b, "  set status=complete on %s (%s)\n", a.ID, a.Title)
		}
		if len(missing) > 0 {
			fmt.Fprintf(&b, "  %d active artifact(s) not in evidence — verify or update status\n", len(missing))
		}
	}

	return b.String()
}
