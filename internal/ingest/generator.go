//nolint:gosec // weak rand is intentional — generator produces synthetic test data, not security-sensitive values
package ingest

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"time"
)

// ShapeFunc produces one NodeRecord for index i.
// The returned record must have ID and Kind set; Title, Labels, Extra are optional.
type ShapeFunc func(i int, source, scanSHA string) NodeRecord

// Generator streams synthetic NDJSON to any io.Writer.
// One Shape produces all node records; edges are added between nodes.
type Generator struct {
	Source       string
	NodeCount    int
	EdgesPerNode int
	ScanSHA      string
	Shape        ShapeFunc
}

// WriteTo emits all node records, then edge records, then a meta record.
// Returns the counts for test assertions.
// Generate streams NDJSON records to w and returns the counts.
func (g *Generator) Generate(w io.Writer) (nodes, edges int, err error) { //nolint:nonamedreturns,gosec // named returns intentional; weak rand fine for synthetic data
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	sha := g.ScanSHA
	if sha == "" {
		sha = fmt.Sprintf("%x", rand.Uint64()) //nolint:gosec // weak rand fine for synthetic test data
	}

	ids := make([]string, g.NodeCount)

	// Emit nodes.
	for i := range g.NodeCount {
		rec := g.Shape(i, g.Source, sha)
		rec.Type = "node"
		ids[i] = rec.ID
		if err = enc.Encode(rec); err != nil {
			return
		}
		nodes++
	}

	// Emit edges between random pairs.
	for i, from := range ids {
		for range g.EdgesPerNode {
			j := rand.IntN(len(ids))
			if j == i {
				continue
			}
			if err = enc.Encode(EdgeRecord{
				Type:     "edge",
				From:     from,
				To:       ids[j],
				Relation: "relates_to",
			}); err != nil {
				return
			}
			edges++
		}
	}

	// Meta record closes the stream.
	err = enc.Encode(MetaRecord{
		Type:       "meta",
		Source:     g.Source,
		ScanSHA:    sha,
		ScannedAt:  time.Now().UTC().Format(time.RFC3339),
		TotalNodes: nodes,
		TotalEdges: edges,
	})
	return
}

// ── Built-in shape functions ──────────────────────────────────────────────

// LocusComponentShape produces a node shaped like a Locus ArchService.
var LocusComponentShape ShapeFunc = func(i int, source, sha string) NodeRecord { //nolint:gosec // weak rand fine for synthetic test data
	return NodeRecord{
		ID:     fmt.Sprintf("%s:component:pkg/comp%d", source, i),
		Kind:   "note",
		Title:  fmt.Sprintf("pkg/comp%d", i),
		Status: "active",
		Labels: []string{
			"source:" + source,
			"kind:component",
			"lang:go",
		},
		Extra: map[string]any{
			"fan_in":     rand.IntN(20),
			"fan_out":    rand.IntN(30),
			"churn":      rand.IntN(50),
			"loc":        rand.IntN(2000) + 100,
			"scan_sha":   sha,
			"scanned_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
}

// JiraIssueShape produces a node shaped like a Jira issue.
var JiraIssueShape ShapeFunc = func(i int, source, sha string) NodeRecord { //nolint:gosec // weak rand fine for synthetic test data
	priorities := []string{"high", "medium", "low"}
	types := []string{"bug", "story", "task"}
	return NodeRecord{
		ID:     fmt.Sprintf("%s:PROJ-%d", source, i+1),
		Kind:   "note",
		Title:  fmt.Sprintf("Issue %d: synthetic Jira ticket", i+1),
		Status: "active",
		Labels: []string{
			"source:" + source,
			"kind:issue",
			"priority:" + priorities[rand.IntN(len(priorities))],
			"type:" + types[rand.IntN(len(types))],
		},
		Extra: map[string]any{
			"issue_key": fmt.Sprintf("PROJ-%d", i+1),
			"scan_sha":  sha,
		},
	}
}

// GitHubPRShape produces a node shaped like a GitHub pull request.
var GitHubPRShape ShapeFunc = func(i int, source, sha string) NodeRecord { //nolint:gosec // weak rand fine for synthetic test data
	states := []string{"open", "merged", "closed"}
	return NodeRecord{
		ID:     fmt.Sprintf("%s:octocat/hello-world#%d", source, i+1),
		Kind:   "note",
		Title:  fmt.Sprintf("PR #%d: synthetic pull request", i+1),
		Status: "active",
		Labels: []string{
			"source:" + source,
			"kind:pr",
			"state:" + states[rand.IntN(len(states))],
		},
		Extra: map[string]any{
			"number":   i + 1,
			"scan_sha": sha,
		},
	}
}
