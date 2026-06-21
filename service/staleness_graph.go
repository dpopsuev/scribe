package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// StaleNeighbor describes a linked artifact that changed after the subject.
type StaleNeighbor struct {
	ID        string
	Title     string
	Relation  string
	UpdatedAt time.Time
}

// stalenessThreshold is the minimum gap between subject and neighbor
// UpdatedAt before the reference is considered stale. Artifacts co-edited
// within this window are normal planning churn, not defects.
const stalenessThreshold = 24 * time.Hour

// NeighborStaleness checks if any artifact linked to id has been updated
// significantly more recently than the subject artifact. Neighbors updated
// within stalenessThreshold of the subject are treated as co-editing churn
// and excluded.
func NeighborStaleness(ctx context.Context, store parchment.Store, art *parchment.Artifact) []StaleNeighbor {
	outEdges, _ := store.Neighbors(ctx, art.ID, "", parchment.Outgoing)
	inEdges, _ := store.Neighbors(ctx, art.ID, "", parchment.Incoming)

	var stale []StaleNeighbor
	seen := make(map[string]bool)

	for _, e := range append(outEdges, inEdges...) {
		neighborID := e.To
		rel := e.Relation
		if e.To == art.ID {
			neighborID = e.From
		}
		if seen[neighborID] {
			continue
		}
		seen[neighborID] = true

		neighbor, err := store.Get(ctx, neighborID)
		if err != nil {
			continue
		}
		gap := neighbor.UpdatedAt.Sub(art.UpdatedAt)
		if gap > stalenessThreshold {
			stale = append(stale, StaleNeighbor{
				ID:        neighborID,
				Title:     neighbor.Title,
				Relation:  rel,
				UpdatedAt: neighbor.UpdatedAt,
			})
		}
	}
	return stale
}

// FormatStalenessHint renders stale neighbors as a human-readable hint
// for agents to reassess linked artifacts.
func FormatStalenessHint(stale []StaleNeighbor) string {
	if len(stale) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n**Stale references** (%d neighbor(s) changed since last update):\n", len(stale))
	for _, s := range stale {
		fmt.Fprintf(&b, "  - %s %q (via %s, updated %s)\n",
			s.ID, s.Title, s.Relation,
			s.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return b.String()
}
