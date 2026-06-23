package service

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// StampContentHash computes a SHA256 hash of the artifact's content-bearing
// fields (title, labels, sections, goal) and stores it in Extra alongside
// the current timestamp. Used for change detection on re-materialization.
func StampContentHash(art *parchment.Artifact) {
	if art.Extra == nil {
		art.Extra = make(map[string]any)
	}
	art.Extra["content_hash"] = contentHash(art)
	art.Extra["materialized_at"] = time.Now().UTC().Format(time.RFC3339)
}

func contentHash(art *parchment.Artifact) string {
	h := sha256.New()
	h.Write([]byte(art.Title))
	for _, l := range art.Labels {
		h.Write([]byte(l))
	}
	for _, s := range art.Sections {
		h.Write([]byte(s.Name))
		h.Write([]byte(s.Text))
	}
	if g := art.Goal(); g != "" {
		h.Write([]byte(g))
	}
	if art.Extra != nil {
		if desc, ok := art.Extra["description"].(string); ok {
			h.Write([]byte(desc))
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ContentChanged returns true if the artifact's current content hash differs
// from the stored hash. Returns true if no previous hash exists (first materialization).
func ContentChanged(art *parchment.Artifact) bool {
	if art.Extra == nil {
		return true
	}
	stored, ok := art.Extra["content_hash"].(string)
	if !ok || stored == "" {
		return true
	}
	return stored != contentHash(art)
}

// ContentHashJSON computes a SHA256 of a JSON-serialized value. Used for
// hashing translate.Record or other structured data.
func ContentHashJSON(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:])
}
