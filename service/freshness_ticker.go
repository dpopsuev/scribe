package service

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

const (
	freshCurrent        = 0.7
	freshStale          = 0.3
	defaultHalfLifeDays = 1

	logKeyScanned      = "scanned"
	logKeyTransitioned = "transitioned"
)

const (
	edgeContains   = "contains"
	edgeImplements = "implements" //nolint:goconst // freshness-specific
	edgeEmbeds     = "embeds"
	edgeFieldRef   = "field_ref"
	edgeCalls      = "calls"
	edgeDependsOn  = "depends_on" //nolint:goconst // freshness-specific
	edgeMentions   = "mentions"
)

var edgeWeights = map[string]float64{
	edgeContains:   1.0,
	edgeImplements: 0.8,
	edgeEmbeds:     0.8,
	edgeFieldRef:   0.6,
	edgeCalls:      0.4,
	edgeDependsOn:  0.3,
	edgeMentions:   0.1,
}

// Freshness computes a multi-signal freshness score in [0, 1] for a code artifact.
//
//	freshness = recency × neighborHealth × max(structuralWeight, 0.1)
//
// recency: Ebbinghaus decay e^(−t/halfLife) based on InsertedAt.
// neighborHealth: 1 − Σ(edgeWeight × neighborIsStale) / valence.
// structuralWeight: tanh(fanIn / 10) — heavily-connected nodes decay slower.
func Freshness(ctx context.Context, store parchment.Store, art *parchment.Artifact) float64 {
	kind := art.Label(parchment.LabelPrefixKind)
	halfLife := float64(defaultHalfLifeDays)
	trait, ok := store.(interface {
		LabelTraitFor(string) (parchment.LabelTrait, bool)
	})
	if ok {
		if lt, found := trait.LabelTraitFor(parchment.LabelPrefixKind + kind); found && lt.HalfLifeDays > 0 {
			halfLife = float64(lt.HalfLifeDays)
		}
	}

	recency := computeRecencyDays(art.InsertedAt, halfLife)

	inEdges, _ := store.Neighbors(ctx, art.ID, "", parchment.Incoming)
	outEdges, _ := store.Neighbors(ctx, art.ID, "", parchment.Outgoing)
	fanIn := len(inEdges)
	structuralWeight := math.Max(0.1, math.Tanh(float64(fanIn)/10.0))

	allEdges := make([]parchment.Edge, 0, len(inEdges)+len(outEdges))
	allEdges = append(allEdges, inEdges...)
	allEdges = append(allEdges, outEdges...)
	neighborHealth := computeNeighborHealth(ctx, store, art, allEdges)

	return recency * neighborHealth * structuralWeight
}

func computeRecencyDays(insertedAt time.Time, halfLifeDays float64) float64 {
	if insertedAt.IsZero() {
		return 0
	}
	days := time.Since(insertedAt).Hours() / 24.0
	if days <= 0 {
		return 1.0
	}
	return math.Exp(-days / halfLifeDays * math.Ln2)
}

func computeNeighborHealth(ctx context.Context, store parchment.Store, art *parchment.Artifact, edges []parchment.Edge) float64 {
	if len(edges) == 0 {
		return 1.0
	}
	var weightedStale, totalWeight float64
	seen := make(map[string]bool)
	for _, e := range edges {
		nID := e.To
		if e.To == art.ID {
			nID = e.From
		}
		if seen[nID] {
			continue
		}
		seen[nID] = true
		w := edgeWeights[e.Relation]
		if w == 0 {
			w = 0.2
		}
		totalWeight += w
		neighbor, err := store.Get(ctx, nID)
		if err != nil {
			continue
		}
		if neighbor.UpdatedAt.After(art.UpdatedAt) {
			weightedStale += w
		}
	}
	if totalWeight == 0 {
		return 1.0
	}
	return 1.0 - (weightedStale/totalWeight)*0.5
}

// FreshnessTicker runs a periodic sweep that transitions code artifacts
// between code.current, code.stale, and code.outdated based on freshness score.
type FreshnessTicker struct {
	store    parchment.Store
	proto    *parchment.Protocol
	interval time.Duration
	stop     chan struct{}
}

// NewFreshnessTicker creates and starts a background freshness sweep.
func NewFreshnessTicker(proto *parchment.Protocol, interval time.Duration) *FreshnessTicker {
	ft := &FreshnessTicker{
		store:    proto.Store(),
		proto:    proto,
		interval: interval,
		stop:     make(chan struct{}),
	}
	go ft.run()
	return ft
}

// Stop halts the background sweep.
func (ft *FreshnessTicker) Stop() {
	close(ft.stop)
}

func (ft *FreshnessTicker) run() {
	ft.sweep()
	ticker := time.NewTicker(ft.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ft.stop:
			return
		case <-ticker.C:
			ft.sweep()
		}
	}
}

func (ft *FreshnessTicker) sweep() {
	ctx := context.Background()
	codeKinds := []string{"code.struct", "code.function", "code.method", "code.interface", "code.file", "code.test"}

	total, transitioned := 0, 0
	for _, kind := range codeKinds {
		arts, _ := ft.proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: []string{parchment.LabelPrefixKind + kind},
		})
		for _, art := range arts {
			total++
			score := Freshness(ctx, ft.store, art)
			status := parchment.StatusFromLabels(art.Labels)

			var target string
			switch {
			case score >= freshCurrent && status != "code.current" && status != "code.indexed":
				target = "code.current"
			case score < freshCurrent && score >= freshStale && status != "code.stale":
				target = "code.stale"
			case score < freshStale && status != "code.outdated" && !strings.HasPrefix(status, "status:"):
				target = "code.outdated"
			}

			if target == "" {
				continue
			}
			results, err := ft.proto.SetField(ctx, []string{art.ID}, "status", target, parchment.SetFieldOptions{Force: true})
			if err == nil && len(results) > 0 && results[0].OK {
				transitioned++
			}
		}
	}

	if transitioned > 0 {
		slog.InfoContext(ctx, "freshness sweep",
			slog.Int(logKeyScanned, total),
			slog.Int(logKeyTransitioned, transitioned))
	}
}
