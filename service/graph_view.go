package service

import (
	"context"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// GraphNode is a node in the 3d-force-graph payload.
type GraphNode struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Status     string `json:"status"`
	Scope      string `json:"scope"`
	Group      string `json:"group,omitempty"`
	Val        int    `json:"val"`
	Violations int    `json:"violations"`
}

// GraphLink is an edge in the 3d-force-graph payload.
type GraphLink struct {
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// GraphData is the full payload returned by graph endpoints.
type GraphData struct {
	Nodes []GraphNode `json:"nodes"`
	Links []GraphLink `json:"links"`
}

// BuildScopeGraph returns one node per scope and one link per cross-scope edge.
func BuildScopeGraph(ctx context.Context, svc *Service) (GraphData, error) {
	counts, weights, err := svc.Proto.Store().ScopeGraph(ctx)
	if err != nil {
		return GraphData{}, err
	}
	nodes := make([]GraphNode, 0, len(counts))
	for _, sc := range counts {
		if sc.Scope == "" || sc.Scope == parchment.SchemaScope || strings.HasPrefix(sc.Scope, "scribe-session") {
			continue
		}
		nodes = append(nodes, GraphNode{
			ID: "project:" + sc.Scope, Name: sc.Scope,
			Kind: "project", Scope: sc.Scope,
			Val: max(3, sc.Count/20),
		})
	}
	links := make([]GraphLink, 0, len(weights))
	for _, w := range weights {
		links = append(links, GraphLink{
			Source: "project:" + w.FromScope, Target: "project:" + w.ToScope,
			Relation: "cross-scope", Weight: float64(w.Weight),
		})
	}
	return GraphData{Nodes: nodes, Links: links}, nil
}

// BuildKindGraph returns one node per kind within a scope.
func BuildKindGraph(ctx context.Context, svc *Service, scope string, statuses, relations []string) (GraphData, error) {
	statusLabels := normalizeStatuses(statuses)
	counts, weights, err := svc.Proto.Store().KindGraph(ctx, scope, statusLabels, relations)
	if err != nil {
		return GraphData{}, err
	}
	nodes := make([]GraphNode, 0, len(counts))
	for _, kc := range counts {
		nodes = append(nodes, GraphNode{
			ID: "kind:" + scope + ":" + kc.Scope, Name: kc.Scope,
			Kind: "kind-group", Scope: scope, Group: kc.Scope,
			Val: max(2, kc.Count/5),
		})
	}
	nodeIDs := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeIDs[n.ID] = true
	}
	links := make([]GraphLink, 0, len(weights))
	for _, w := range weights {
		src := "kind:" + scope + ":" + w.FromScope
		tgt := "kind:" + scope + ":" + w.ToScope
		if !nodeIDs[src] || !nodeIDs[tgt] {
			continue
		}
		links = append(links, GraphLink{
			Source: src, Target: tgt,
			Relation: "cross-kind", Weight: float64(w.Weight),
		})
	}
	return GraphData{Nodes: nodes, Links: links}, nil
}

// BuildArtifactGraph returns individual artifact nodes and their edges.
func BuildArtifactGraph(ctx context.Context, svc *Service, scope string, statuses, relations []string, maxNodes int) (GraphData, error) {
	arts, err := fetchGraphArtifacts(ctx, svc, scope, statuses)
	if err != nil {
		return GraphData{}, err
	}
	ids := make([]string, 0, len(arts))
	for _, a := range arts {
		ids = append(ids, a.ID)
	}
	edges, _ := batchedListEdges(ctx, svc.Proto.Store(), ids, relations)
	degree := make(map[string]int, len(ids))
	for _, e := range edges {
		degree[e.From]++
		degree[e.To]++
	}
	nodes := make([]GraphNode, 0, len(arts))
	for _, a := range arts {
		nodes = append(nodes, GraphNode{
			ID:         a.ID,
			Name:       a.Title,
			Kind:       a.Label(parchment.LabelPrefixKind),
			Status:     parchment.StatusFromLabels(a.Labels),
			Scope:      a.Label(parchment.LabelPrefixScope),
			Val:        degree[a.ID] + 1,
			Violations: ViolationCount(a),
		})
	}
	if maxNodes > 0 && len(nodes) > maxNodes {
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Val > nodes[j].Val })
		nodes = nodes[:maxNodes]
	}
	kept := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		kept[n.ID] = true
	}
	links := make([]GraphLink, 0, len(edges))
	for _, e := range edges {
		if !kept[e.From] || !kept[e.To] {
			continue
		}
		links = append(links, GraphLink{
			Source: e.From, Target: e.To,
			Relation: e.Relation, Weight: e.Weight,
		})
	}
	return GraphData{Nodes: nodes, Links: links}, nil
}

const edgeBatchSize = 400

// batchedListEdges splits large ID sets into batches to avoid SQLite
// bind-parameter overhead. At 4840 IDs, ListEdges generates 9680 bind
// params — the bind() call alone takes 44% of CPU. Batching by from_id
// via ListEdgesFrom and filtering to_id in memory reduces bind params ~10×.
func batchedListEdges(ctx context.Context, store parchment.Store, ids, relations []string) ([]parchment.Edge, error) {
	if len(ids) <= edgeBatchSize {
		return store.ListEdges(ctx, ids, relations)
	}

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	var all []parchment.Edge
	for i := 0; i < len(ids); i += edgeBatchSize {
		end := min(i+edgeBatchSize, len(ids))
		batch := ids[i:end]
		edges, err := store.ListEdgesFrom(ctx, batch, relations)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if idSet[e.To] {
				all = append(all, e)
			}
		}
	}
	return all, nil
}

// BuildLocalGraph returns a neighborhood graph rooted at one artifact.
func BuildLocalGraph(ctx context.Context, svc *Service, rootID string, hops int) (GraphData, error) {
	collected, edges, err := bfsCollect(ctx, svc, rootID, hops)
	if err != nil {
		return GraphData{}, err
	}
	return GraphData{
		Nodes: artifactsToNodes(collected),
		Links: dedupeLinks(edges, collected),
	}, nil
}

func bfsCollect(ctx context.Context, svc *Service, rootID string, hops int) (map[string]*parchment.Artifact, []parchment.Edge, error) {
	collected := make(map[string]*parchment.Artifact)
	var edges []parchment.Edge

	root, err := svc.Proto.GetArtifact(ctx, rootID)
	if err != nil {
		return nil, nil, err
	}
	collected[root.ID] = root

	frontier := []string{rootID}
	for range hops {
		var next []string
		for _, id := range frontier {
			neighbors, _ := svc.Proto.Store().Neighbors(ctx, id, "", parchment.Both)
			for _, e := range neighbors {
				edges = append(edges, e)
				peerID := peerOf(e, id)
				if _, ok := collected[peerID]; ok {
					continue
				}
				peer, err := svc.Proto.GetArtifact(ctx, peerID)
				if err != nil {
					continue
				}
				collected[peerID] = peer
				next = append(next, peerID)
			}
		}
		frontier = next
	}
	return collected, edges, nil
}

func peerOf(e parchment.Edge, self string) string {
	if e.To == self {
		return e.From
	}
	return e.To
}

func artifactsToNodes(arts map[string]*parchment.Artifact) []GraphNode {
	nodes := make([]GraphNode, 0, len(arts))
	for _, a := range arts {
		nodes = append(nodes, GraphNode{
			ID: a.ID, Name: a.Title,
			Kind:   a.Label(parchment.LabelPrefixKind),
			Status: parchment.StatusFromLabels(a.Labels),
			Scope:  a.Label(parchment.LabelPrefixScope),
			Val:    1,
		})
	}
	return nodes
}

func dedupeLinks(edges []parchment.Edge, valid map[string]*parchment.Artifact) []GraphLink {
	seen := make(map[string]bool)
	links := make([]GraphLink, 0, len(edges))
	for _, e := range edges {
		if valid[e.From] == nil || valid[e.To] == nil {
			continue
		}
		key := e.From + "|" + e.Relation + "|" + e.To
		if seen[key] {
			continue
		}
		seen[key] = true
		links = append(links, GraphLink{
			Source: e.From, Target: e.To,
			Relation: e.Relation, Weight: e.Weight,
		})
	}
	return links
}

// BuildLensGraph returns a GraphData payload from a lens projection.
func BuildLensGraph(ctx context.Context, svc *Service, spec parchment.LensSpec) (GraphData, error) {
	result, err := svc.Proto.ApplyLens(ctx, spec)
	if err != nil {
		return GraphData{}, err
	}
	validIDs := make(map[string]bool, len(result.Entries))
	nodes := make([]GraphNode, 0, len(result.Entries))
	for _, e := range result.Entries {
		validIDs[e.ID] = true
		val := int(e.Score*10) + 1
		if val < 1 {
			val = 1
		}
		nodes = append(nodes, GraphNode{
			ID:     e.ID,
			Name:   e.Title,
			Kind:   parchment.LabelValue(e.Labels, parchment.LabelPrefixKind),
			Status: parchment.StatusFromLabels(e.Labels),
			Scope:  parchment.LabelValue(e.Labels, parchment.LabelPrefixScope),
			Val:    val,
		})
	}
	seen := make(map[string]bool)
	links := make([]GraphLink, 0, len(result.Edges))
	for _, e := range result.Edges {
		if !validIDs[e.From] || !validIDs[e.To] {
			continue
		}
		key := e.From + "|" + e.Relation + "|" + e.To
		if seen[key] {
			continue
		}
		seen[key] = true
		links = append(links, GraphLink{
			Source: e.From, Target: e.To,
			Relation: e.Relation, Weight: e.Weight,
		})
	}
	return GraphData{Nodes: nodes, Links: links}, nil
}

func fetchGraphArtifacts(ctx context.Context, svc *Service, scope string, statuses []string) ([]*parchment.Artifact, error) {
	labelsOr := normalizeStatuses(statuses)
	var labels []string
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}
	return svc.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels:   labels,
		LabelsOr: labelsOr,
	})
}

func normalizeStatuses(statuses []string) []string {
	out := make([]string, 0, len(statuses))
	for _, st := range statuses {
		out = append(out, statusLabelFor(strings.TrimSpace(st)))
	}
	return out
}

// ViolationCount returns the number of compliance violations on an artifact.
func ViolationCount(a *parchment.Artifact) int {
	v, ok := a.Extra["compliance_violations"]
	if ok {
		if arr, isArr := v.([]any); isArr {
			return len(arr)
		}
	}
	for _, l := range a.Labels {
		if l == "compliance:violation" {
			return 1
		}
	}
	return 0
}
