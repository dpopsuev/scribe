//nolint:goconst,mnd // status literals and delivery weights
package service

import (
	"context"
	"math"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// ProgressMetrics separates content fill from delivery and verification.
type ProgressMetrics struct {
	ContentCompleteness float64 `json:"content_completeness"`
	DeliveryProgress    float64 `json:"delivery_progress"`
	VerifiedProgress    float64 `json:"verified_progress"`
	// Completeness is deprecated alias of ContentCompleteness for older clients.
	Completeness float64 `json:"completeness"`
}

// ComputeProgress returns the three AX progress metrics for an artifact.
func ComputeProgress(ctx context.Context, svc *Service, art *parchment.Artifact) ProgressMetrics {
	content := contentCompleteness(svc, art)
	delivery := deliveryProgress(ctx, svc, art)
	verified := verifiedProgress(ctx, svc, art)
	return ProgressMetrics{
		ContentCompleteness: round2(content),
		DeliveryProgress:    round2(delivery),
		VerifiedProgress:    round2(verified),
		Completeness:        round2(content),
	}
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func contentCompleteness(svc *Service, art *parchment.Artifact) float64 {
	kind := art.Label(parchment.LabelPrefixKind)
	must := svc.Proto.MustSections(kind)
	should := svc.Proto.ShouldSections(kind)
	required := append(append([]string{}, must...), should...)
	if len(required) == 0 {
		if len(art.Sections) == 0 {
			return 0
		}
		filled := 0
		for _, s := range art.Sections {
			if strings.TrimSpace(s.Text) != "" {
				filled++
			}
		}
		if filled == 0 {
			return 0
		}
		return float64(filled) / float64(len(art.Sections))
	}
	have := make(map[string]bool, len(art.Sections))
	for _, s := range art.Sections {
		if strings.TrimSpace(s.Text) != "" {
			have[s.Name] = true
		}
	}
	filled := 0
	for _, name := range required {
		if have[name] {
			filled++
		}
	}
	return float64(filled) / float64(len(required))
}

// deliveryProgress is the lifecycle-weighted ratio of terminal work descendants.
// Draft-only trees score 0 even when sections are fully filled.
func deliveryProgress(ctx context.Context, svc *Service, art *parchment.Artifact) float64 {
	leaves := workLeaves(ctx, svc, art.ID)
	if len(leaves) == 0 {
		status := parchment.StatusFromLabels(art.Labels)
		if svc.Proto.IsTerminal(status) {
			return 1
		}
		return 0
	}
	var weight, done float64
	for _, leaf := range leaves {
		w := deliveryWeight(parchment.StatusFromLabels(leaf.Labels))
		weight++
		done += w
	}
	if weight == 0 {
		return 0
	}
	return done / weight
}

func deliveryWeight(status string) float64 {
	switch {
	case strings.HasSuffix(status, ".complete") || status == "decision.accepted" || status == "note.evergreen":
		return 1
	case strings.Contains(status, "active") || status == "decision.proposed" || status == "work.blocked":
		return 0.35
	default:
		return 0
	}
}

func verifiedProgress(ctx context.Context, svc *Service, art *parchment.Artifact) float64 {
	leaves := workLeaves(ctx, svc, art.ID)
	if len(leaves) == 0 {
		if svc.Proto.IsTerminal(parchment.StatusFromLabels(art.Labels)) && hasVerificationEvidence(ctx, svc, art) {
			return 1
		}
		return 0
	}
	var done, verified float64
	for _, leaf := range leaves {
		if !svc.Proto.IsTerminal(parchment.StatusFromLabels(leaf.Labels)) {
			continue
		}
		done++
		if hasVerificationEvidence(ctx, svc, leaf) {
			verified++
		}
	}
	if done == 0 {
		return 0
	}
	return verified / done
}

func workLeaves(ctx context.Context, svc *Service, id string) []*parchment.Artifact {
	var leaves []*parchment.Artifact
	var walk func(string)
	walk = func(parent string) {
		children, _ := svc.Proto.Neighbors(ctx, parent, parchment.RelParentOf, parchment.Outgoing)
		if len(children) == 0 {
			if art, err := svc.Proto.GetArtifact(ctx, parent); err == nil && isWorkKind(art.Label(parchment.LabelPrefixKind)) {
				leaves = append(leaves, art)
			}
			return
		}
		for _, e := range children {
			walk(e.To)
		}
	}
	walk(id)
	return leaves
}

func isWorkKind(kind string) bool {
	return strings.HasPrefix(kind, "effort.") || kind == "intent.bug" || kind == "intent.spec"
}

func hasVerificationEvidence(ctx context.Context, svc *Service, art *parchment.Artifact) bool {
	for _, s := range art.Sections {
		if (s.Name == "evidence" || s.Name == "verification") && strings.TrimSpace(s.Text) != "" {
			return true
		}
	}
	if art.Extra != nil {
		if v, ok := art.Extra["verification"]; ok && v != nil && v != "" {
			return true
		}
	}
	edges, _ := svc.Proto.Neighbors(ctx, art.ID, parchment.RelEvidencedBy, parchment.Outgoing)
	return len(edges) > 0
}
