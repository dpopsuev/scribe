package service

import (
	"context"
	"sort"

	parchment "github.com/dpopsuev/parchment"
)

// FanIn returns the count of incoming edges for an artifact.
func FanIn(ctx context.Context, store parchment.GraphStore, id string) (int, error) {
	edges, err := store.Neighbors(ctx, id, "", parchment.Incoming)
	if err != nil {
		return 0, err
	}
	return len(edges), nil
}

// FanOut returns the count of outgoing edges for an artifact.
func FanOut(ctx context.Context, store parchment.GraphStore, id string) (int, error) {
	edges, err := store.Neighbors(ctx, id, "", parchment.Outgoing)
	if err != nil {
		return 0, err
	}
	return len(edges), nil
}

// FanScore holds fan-in/fan-out counts for a single artifact.
type FanScore struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	FanIn  int    `json:"fan_in"`
	FanOut int    `json:"fan_out"`
}

// CommonNeighbors returns IDs that appear in both a's and b's neighbor sets
// for the given direction. Co-citation uses Incoming; bibliographic coupling uses Outgoing.
func CommonNeighbors(ctx context.Context, store parchment.GraphStore, idA, idB string, dir parchment.Direction) ([]string, error) {
	edgesA, err := store.Neighbors(ctx, idA, "", dir)
	if err != nil {
		return nil, err
	}
	edgesB, err := store.Neighbors(ctx, idB, "", dir)
	if err != nil {
		return nil, err
	}

	setA := make(map[string]bool, len(edgesA))
	for _, e := range edgesA {
		setA[neighborID(e, dir)] = true
	}
	var common []string
	for _, e := range edgesB {
		if setA[neighborID(e, dir)] {
			common = append(common, neighborID(e, dir))
		}
	}
	return common, nil
}

func neighborID(e parchment.Edge, dir parchment.Direction) string {
	if dir == parchment.Incoming {
		return e.From
	}
	return e.To
}

// CoCitationResult represents an artifact that shares common incoming sources with a target.
type CoCitationResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Overlap int    `json:"overlap"`
}

// FindCoCitations finds artifacts that share common incoming neighbors with the target.
// Uses an optimized approach: collect target's sources, then find their other targets.
func FindCoCitations(ctx context.Context, store parchment.Store, targetID string, dir parchment.Direction, minShared, limit int) ([]CoCitationResult, error) {
	sources, err := store.Neighbors(ctx, targetID, "", dir)
	if err != nil {
		return nil, err
	}

	overlap := map[string]int{}
	for _, src := range sources {
		srcID := neighborID(src, dir)
		reverseDir := parchment.Outgoing
		if dir == parchment.Outgoing {
			reverseDir = parchment.Incoming
		}
		targets, err := store.Neighbors(ctx, srcID, "", reverseDir)
		if err != nil {
			continue
		}
		for _, t := range targets {
			tid := neighborID(t, reverseDir)
			if tid != targetID {
				overlap[tid]++
			}
		}
	}

	var results []CoCitationResult
	for id, count := range overlap {
		if count < minShared {
			continue
		}
		art, err := store.Get(ctx, id)
		if err != nil {
			continue
		}
		results = append(results, CoCitationResult{
			ID:      id,
			Title:   art.Title,
			Kind:    art.Label(parchment.LabelPrefixKind),
			Overlap: count,
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Overlap > results[j].Overlap })
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// ShortestPath finds the shortest path from one artifact to another using BFS.
// Returns nil if no path exists within maxDepth hops.
func ShortestPath(ctx context.Context, store parchment.GraphStore, from, to string, maxDepth int) ([]parchment.Edge, error) {
	if from == to {
		return nil, nil
	}
	type state struct {
		id   string
		path []parchment.Edge
	}
	visited := map[string]bool{from: true}
	queue := []state{{id: from}}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if maxDepth > 0 && len(cur.path) >= maxDepth {
			continue
		}
		edges, err := store.Neighbors(ctx, cur.id, "", parchment.Outgoing)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if e.To == to {
				return append(cur.path, e), nil
			}
			if !visited[e.To] {
				visited[e.To] = true
				newPath := make([]parchment.Edge, len(cur.path)+1)
				copy(newPath, cur.path)
				newPath[len(cur.path)] = e
				queue = append(queue, state{id: e.To, path: newPath})
			}
		}
	}
	return nil, nil
}

// PageRankResult holds the PageRank score for a single artifact.
type PageRankResult struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Kind  string  `json:"kind"`
	Score float64 `json:"score"`
}

// ComputePageRank runs iterative PageRank over artifacts matching the given labels.
func ComputePageRank(ctx context.Context, store parchment.Store, labels []string, iterations int, damping float64) ([]PageRankResult, error) {
	arts, err := store.List(ctx, parchment.Filter{Labels: labels})
	if err != nil {
		return nil, err
	}
	n := len(arts)
	if n == 0 {
		return nil, nil
	}

	artByID := make(map[string]*parchment.Artifact, n)
	inScope := make(map[string]bool, n)
	for _, a := range arts {
		artByID[a.ID] = a
		inScope[a.ID] = true
	}

	outgoing := make(map[string][]string, n)
	for _, a := range arts {
		edges, err := store.Neighbors(ctx, a.ID, "", parchment.Outgoing)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if inScope[e.To] {
				outgoing[a.ID] = append(outgoing[a.ID], e.To)
			}
		}
	}

	scores := make(map[string]float64, n)
	for _, a := range arts {
		scores[a.ID] = 1.0 / float64(n)
	}

	base := (1 - damping) / float64(n)
	for range iterations {
		newScores := make(map[string]float64, n)
		var danglingSum float64
		for _, a := range arts {
			if len(outgoing[a.ID]) == 0 {
				danglingSum += scores[a.ID]
			}
		}
		danglingShare := damping * danglingSum / float64(n)
		for _, a := range arts {
			newScores[a.ID] = base + danglingShare
		}
		for _, a := range arts {
			targets := outgoing[a.ID]
			if len(targets) == 0 {
				continue
			}
			share := damping * scores[a.ID] / float64(len(targets))
			for _, t := range targets {
				newScores[t] += share
			}
		}
		scores = newScores
	}

	results := make([]PageRankResult, 0, n)
	for _, a := range arts {
		results = append(results, PageRankResult{
			ID:    a.ID,
			Title: a.Title,
			Kind:  a.Label(parchment.LabelPrefixKind),
			Score: scores[a.ID],
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	return results, nil
}
