package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// Common label queries used across ops — DRY: define once, use everywhere.
var (
	labelCampaign     = parchment.LabelPrefixKind + "effort.campaign"
	labelGoal         = parchment.LabelPrefixKind + "effort.goal"
	labelTask         = parchment.LabelPrefixKind + "effort.task"
	labelStatusActive = "work.active"
	labelStatusDraft  = "work.draft"
)

const kindTask = "effort.task"

// labelVal extracts the value after prefix from the first matching label in labels.
func labelVal(labels []string, prefix string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, prefix) {
			return strings.TrimPrefix(l, prefix)
		}
	}
	return ""
}

// statusLabelFor returns the label to use for filtering/building by a status string.
// Domain statuses (work.draft, note.fleeting, etc.) are raw labels.
// System statuses (retired, archived) use the status: prefix.
func statusLabelFor(status string) string {
	if parchment.IsDomainStatusLabel(status) {
		return status
	}
	return parchment.LabelPrefixStatus + status
}

// FilterSections removes sections not in the filter list (in-place).
func FilterSections(art *parchment.Artifact, filter []string) {
	if len(filter) == 0 {
		return
	}
	keep := make(map[string]bool, len(filter))
	for _, name := range filter {
		keep[strings.ToLower(name)] = true
	}
	filtered := art.Sections[:0]
	for _, s := range art.Sections {
		if keep[strings.ToLower(s.Name)] {
			filtered = append(filtered, s)
		}
	}
	art.Sections = filtered
}

// RelevanceScore returns a numeric relevance score for top-N ranking.
func RelevanceScore(art *parchment.Artifact) float64 {
	score := 0.0
	switch parchment.StatusFromLabels(art.Labels) {
	case statusWorkActive, "decision.proposed":
		score += 3.0
	case "work.draft", "note.fleeting":
		score += 1.0
	}
	switch art.Label(parchment.LabelPrefixPriority) {
	case "critical":
		score += 4.0
	case "high":
		score += 3.0
	case "medium":
		score += 2.0
	case "low":
		score += 1.0
	}
	return score
}

// --- ReadLog persistence ---

const readLogScope = "scribe-session"
const readLogTitle = "readlog"
const statusWorkActive = "work.active"

// NewSessionID generates a random 8-byte hex session identifier.
func NewSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ReadLogArtifactID returns the stable config artifact ID for a session's readLog.
func ReadLogArtifactID(sessionID string) string {
	return readLogScope + "-" + sessionID
}

// PersistReadLog writes the current readLog to a config artifact (best-effort).
func (s *Service) PersistReadLog(ctx context.Context, sessionID string, readLog map[string]bool) {
	if sessionID == "" || s.Proto == nil {
		return
	}
	ids := make([]string, 0, len(readLog))
	for id := range readLog {
		ids = append(ids, id)
	}
	artID := ReadLogArtifactID(sessionID)
	if _, err := s.Proto.GetArtifact(ctx, artID); err != nil {
		_, _ = s.Proto.CreateArtifact(ctx, parchment.CreateInput{
			ExplicitID: artID,
			Labels:     []string{parchment.LabelPrefixKind + "support.config", statusWorkActive, parchment.LabelPrefixScope + readLogScope},
			Title:      readLogTitle,
			Extra:      map[string]any{"read_ids": ids, "session_id": sessionID},
		})
		return
	}
	_ = s.Proto.PatchArtifact(ctx, artID, parchment.ArtifactPatch{
		SetExtra: map[string]any{"read_ids": ids},
	})
}

// LoadReadLog restores a prior session's readLog from the config artifact.
func LoadReadLog(ctx context.Context, store parchment.Store, proto *parchment.Protocol, sessionID string) map[string]bool {
	result := make(map[string]bool)
	if store == nil || proto == nil {
		return result
	}
	artID := ReadLogArtifactID(sessionID)
	art, err := proto.GetArtifact(ctx, artID)
	if err != nil {
		return result
	}
	if ids, ok := art.Extra["read_ids"].([]any); ok {
		for _, raw := range ids {
			if id, ok := raw.(string); ok && id != "" {
				result[id] = true
			}
		}
	}
	return result
}
