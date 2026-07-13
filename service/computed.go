package service

import (
	"context"
	"math"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// StampComputedFields adds virtual computed fields to art.Extra.
// Fields: age_days, staleness_days, content/delivery/verified progress.
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

	m := ComputeProgress(ctx, svc, art)
	art.Extra["content_completeness"] = m.ContentCompleteness
	art.Extra["delivery_progress"] = m.DeliveryProgress
	art.Extra["verified_progress"] = m.VerifiedProgress
	art.Extra["completeness"] = m.Completeness
}

// StampComputedFieldsBatch stamps computed fields on a slice of artifacts.
func StampComputedFieldsBatch(ctx context.Context, svc *Service, arts []*parchment.Artifact) {
	for _, art := range arts {
		StampComputedFields(ctx, svc, art)
	}
}
