package service

import (
	"context"
	"math"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// StampComputedFields adds virtual computed fields to art.Extra.
// Fields: age_days, staleness_days, completeness (0.0–1.0).
func StampComputedFields(ctx context.Context, svc *Service, art *parchment.Artifact) {
	if art.Extra == nil {
		art.Extra = make(map[string]any)
	}
	now := time.Now()

	if !art.CreatedAt.IsZero() {
		art.Extra["age_days"] = int(math.Floor(now.Sub(art.CreatedAt).Hours() / 24))
	}

	if !art.UpdatedAt.IsZero() {
		days := int(math.Floor(now.Sub(art.UpdatedAt).Hours() / 24))
		art.Extra["staleness_days"] = days
	}

	score := svc.Proto.CompletionScore(ctx, art)
	art.Extra["completeness"] = math.Round(score*100) / 100
}

// StampComputedFieldsBatch stamps computed fields on a slice of artifacts.
func StampComputedFieldsBatch(ctx context.Context, svc *Service, arts []*parchment.Artifact) {
	for _, art := range arts {
		StampComputedFields(ctx, svc, art)
	}
}
