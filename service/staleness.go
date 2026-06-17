package service

import (
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const (
	BackendLocus  = "locus"
	BackendEmcee  = "emcee"
	BackendConty  = "conty"
	BackendGundog = "gundog"
)

// SourceTTL defines how long ingested data stays fresh per source backend.
// Zero means immutable (never stale).
var SourceTTL = map[string]time.Duration{
	BackendLocus:  24 * time.Hour,
	BackendEmcee:  1 * time.Hour,
	BackendConty:  0,
	BackendGundog: 7 * 24 * time.Hour,
}

// RefBackend extracts the source backend name from an artifact's Extra.
func RefBackend(art *parchment.Artifact) string {
	if art.Extra == nil {
		return ""
	}
	v, _ := art.Extra["ref_backend"].(string)
	return v
}

// RefID extracts the native source ID from an artifact's Extra.
func RefID(art *parchment.Artifact) string {
	if art.Extra == nil {
		return ""
	}
	v, _ := art.Extra["ref_id"].(string)
	return v
}

// IsStale returns true if the artifact's ingested data has exceeded
// the TTL for its source backend. Artifacts without ref_backend or
// with zero TTL (immutable sources like conty) are never stale.
func IsStale(art *parchment.Artifact) bool {
	backend := RefBackend(art)
	ttl, ok := SourceTTL[backend]
	if !ok || ttl == 0 {
		return false
	}
	if art.InsertedAt.IsZero() {
		return false
	}
	return time.Since(art.InsertedAt) > ttl
}
