package service

import "time"

// Provenance tracks how and by whom an artifact was created.
type Provenance struct {
	CreatedBy      string `json:"created_by"`                // "agent" | "user" | "ingest"
	Harness        string `json:"harness,omitempty"`         // "claude-code" | "cursor" | "alef" | "cli"
	HarnessVersion string `json:"harness_version,omitempty"` // "1.0.32"
	Model          string `json:"model,omitempty"`           // "claude-opus-4-6"
	SessionID      string `json:"session_id,omitempty"`      // "ses-abc123"
	Confidence     string `json:"confidence"`                // "evidence" | "instruction"
	Timestamp      string `json:"timestamp"`                 // RFC3339
}

// StampProvenance merges provenance metadata into an artifact's Extra map.
func StampProvenance(extra map[string]any, p Provenance) map[string]any {
	if extra == nil {
		extra = make(map[string]any)
	}
	prov := map[string]any{
		"created_by": p.CreatedBy,
		"confidence": p.Confidence,
		"timestamp":  p.Timestamp,
	}
	if p.Harness != "" {
		prov["harness"] = p.Harness
	}
	if p.HarnessVersion != "" {
		prov["harness_version"] = p.HarnessVersion
	}
	if p.Model != "" {
		prov["model"] = p.Model
	}
	if p.SessionID != "" {
		prov["session_id"] = p.SessionID
	}
	extra["provenance"] = prov
	return extra
}

// AgentProvenance returns default provenance for agent-created artifacts.
func AgentProvenance(harness, harnessVersion, sessionID string) Provenance {
	return Provenance{
		CreatedBy:      "agent",
		Harness:        harness,
		HarnessVersion: harnessVersion,
		SessionID:      sessionID,
		Confidence:     "evidence",
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
}

// UserProvenance returns default provenance for user-created artifacts.
func UserProvenance(harness string) Provenance {
	return Provenance{
		CreatedBy:  "user",
		Harness:    harness,
		Confidence: "instruction",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// IngestProvenance returns default provenance for ingested artifacts.
func IngestProvenance(source string) Provenance {
	return Provenance{
		CreatedBy:  "ingest",
		Harness:    source,
		Confidence: "evidence",
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}
