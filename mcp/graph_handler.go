package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	formatJSON        = "json"
	priorityNone      = "none"
	directionIncoming = "incoming"
)

func (h *handler) handleGraph(ctx context.Context, req *sdkmcp.CallToolRequest, in graphInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocyclo,cyclop,funlen,nestif // dispatch switch
	switch in.Action {
	case "tree":
		tree, err := h.proto.ArtifactTree(ctx, parchment.TreeInput{
			ID: in.ID, Relation: in.Relation, Direction: in.Direction, Depth: in.Depth,
		})
		if err != nil {
			return nil, nil, err
		}
		if in.Format == formatJSON {
			data, _ := json.Marshal(tree)
			return text(string(data)), nil, nil
		}
		return h.handleTree(ctx, req, parchment.TreeInput{
			ID: in.ID, Relation: in.Relation, Direction: in.Direction, Depth: in.Depth,
		})
	case "link":
		if len(in.Edges) > 0 {
			return h.handleBulkEdge(ctx, in.Edges, false)
		}
		if in.ID == "" {
			return nil, nil, fmt.Errorf("id is required (source artifact ID) for link action") //nolint:err113 // agent-facing hint
		}
		if len(in.Targets) == 0 {
			return nil, nil, fmt.Errorf("targets is required (array of target artifact IDs) for link action") //nolint:err113 // agent-facing hint
		}
		if in.Relation == "" {
			return nil, nil, fmt.Errorf("relation is required (edge type: parent_of, depends_on, follows, justifies, implements, documents) for link action") //nolint:err113 // agent-facing hint
		}
		return h.handleLink(ctx, req, linkInput{
			ID: in.ID, Relation: in.Relation, Targets: in.Targets,
		})
	case "briefing":
		if in.Format == formatJSON {
			tree, err := h.proto.ArtifactTree(ctx, parchment.TreeInput{
				ID: in.ID, Relation: "*", Direction: "both",
			})
			if err != nil {
				return nil, nil, err
			}
			data, _ := json.Marshal(tree)
			return text(string(data)), nil, nil
		}
		return h.handleBriefing(ctx, in.ID, in.Depth)
	case "topo_sort":
		if in.ID == "" {
			return nil, nil, fmt.Errorf("id required for topo_sort (root artifact)") //nolint:err113 // agent-facing hint
		}
		entries, err := h.proto.TopoSort(ctx, in.ID)
		if err != nil && len(entries) == 0 {
			return nil, nil, err
		}
		if in.Unblocked { //nolint:nestif // blocking check requires walking deps per entry
			limit := in.Depth
			if limit <= 0 {
				limit = 5
			}
			schema := h.proto.Schema()
			var ready []parchment.TopoEntry
			for _, e := range entries {
				if schema.IsTerminal(e.Status) {
					continue
				}
				art, _ := h.proto.GetArtifact(ctx, e.ID)
				if art == nil {
					continue
				}
				blocked := false
				for _, depID := range art.DependsOn {
					dep, _ := h.proto.GetArtifact(ctx, depID)
					if dep != nil && !schema.IsTerminal(dep.Status) {
						blocked = true
						break
					}
				}
				if !blocked {
					ready = append(ready, e)
					if len(ready) >= limit {
						break
					}
				}
			}
			if len(ready) == 0 {
				return text("no unblocked tasks found"), nil, nil
			}
			entries = ready
		}
		if in.Format == formatJSON {
			data, _ := json.Marshal(entries)
			return text(string(data)), nil, nil
		}
		var b strings.Builder
		for i, e := range entries {
			fmt.Fprintf(&b, "%d. %s [%s] %s", i+1, e.ID, e.Status, e.Title)
			if e.Priority != "" && e.Priority != priorityNone {
				fmt.Fprintf(&b, " (%s)", e.Priority)
			}
			b.WriteString("\n")
		}
		if err != nil {
			fmt.Fprintf(&b, "\n%s\n", err)
		}
		return text(b.String()), nil, nil
	case "unlink":
		if len(in.Edges) > 0 {
			return h.handleBulkEdge(ctx, in.Edges, true)
		}
		return h.handleLink(ctx, req, linkInput{
			ID: in.ID, Relation: in.Relation, Targets: in.Targets, Unlink: true,
		})
	case "replace":
		if in.ID == "" || in.Relation == "" || in.OldTarget == "" || in.Target == "" {
			return nil, nil, fmt.Errorf("id, relation, old_target, and target required for replace") //nolint:err113 // agent-facing input validation
		}
		return h.handleReplace(ctx, in.ID, in.Relation, in.OldTarget, in.Target)
	default:
		return nil, nil, fmt.Errorf("unknown graph action %q — pass via artifact tool: tree, link, unlink, topo_sort, replace", in.Action) //nolint:err113 // agent-facing hint
	}
}

func (h *handler) handleTree(ctx context.Context, _ *sdkmcp.CallToolRequest, in parchment.TreeInput) (*sdkmcp.CallToolResult, any, error) {
	tree, err := h.proto.ArtifactTree(ctx, in)
	if err != nil {
		return nil, nil, err
	}
	return text(service.RenderTree(tree)), nil, nil
}

func (h *handler) handleBriefing(ctx context.Context, id string, depth int) (*sdkmcp.CallToolResult, any, error) {
	tree, err := h.proto.ArtifactTree(ctx, parchment.TreeInput{
		ID:        id,
		Relation:  "*",
		Direction: "both",
		Depth:     depth,
	})
	if err != nil {
		return nil, nil, err
	}
	return text(service.RenderBriefing(tree)), nil, nil
}

func (h *handler) handleLink(ctx context.Context, _ *sdkmcp.CallToolRequest, in linkInput) (*sdkmcp.CallToolResult, any, error) {
	var results []parchment.Result
	var err error
	if in.Unlink {
		results, err = h.proto.UnlinkArtifacts(ctx, in.ID, in.Relation, in.Targets)
	} else {
		results, err = h.proto.LinkArtifacts(ctx, in.ID, in.Relation, in.Targets)
	}
	if err != nil {
		return nil, nil, err
	}
	verb := "linked"
	if in.Unlink {
		verb = "unlinked"
	}
	var lines []string
	for _, r := range results {
		if r.OK {
			lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, in.ID, in.Relation, r.ID))
		} else {
			lines = append(lines, fmt.Sprintf("%s -> error: %s", r.ID, r.Error))
		}
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleImpact(ctx context.Context, id string) (*sdkmcp.CallToolResult, any, error) { //nolint:gocyclo,cyclop,funlen // impact analysis is inherently multi-check
	art, err := h.proto.GetArtifact(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Impact analysis for %s [%s] %s:", id, art.Status, art.Title))

	// Children (parent_of)
	children, _ := h.proto.ListArtifacts(ctx, parchment.ListInput{Parent: id})
	if len(children) > 0 {
		lines = append(lines, fmt.Sprintf("\nChildren (%d):", len(children)))
		for _, ch := range children {
			lines = append(lines, fmt.Sprintf("  %s [%s] %s", ch.ID, ch.Status, ch.Title))
		}
	}

	// Inbound depends_on (things that depend on this)
	depEdges, _ := h.proto.GetArtifactEdges(ctx, id)
	var dependents, implementors []string
	for _, e := range depEdges {
		if e.Direction == directionIncoming {
			switch e.Relation {
			case "depends_on":
				dependents = append(dependents, fmt.Sprintf("  %s [%s] %s", e.Target.ID, e.Target.Status, e.Target.Title))
			case "implements":
				implementors = append(implementors, fmt.Sprintf("  %s [%s] %s", e.Target.ID, e.Target.Status, e.Target.Title))
			}
		}
	}
	if len(dependents) > 0 {
		lines = append(lines, fmt.Sprintf("\nDepends on this (%d):", len(dependents)))
		lines = append(lines, dependents...)
	}
	if len(implementors) > 0 {
		lines = append(lines, fmt.Sprintf("\nImplements this (%d):", len(implementors)))
		lines = append(lines, implementors...)
	}

	// Warnings
	var warnings []string
	if len(children) > 0 {
		nonTerminal := 0
		for _, ch := range children {
			if ch.Status != "complete" && ch.Status != "archived" && ch.Status != "canceled" {
				nonTerminal++
			}
		}
		if nonTerminal > 0 {
			warnings = append(warnings, fmt.Sprintf("%d children would be orphaned (non-terminal)", nonTerminal))
		}
	}
	if len(dependents) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d artifacts depend on this — their dependency chain would break", len(dependents)))
	}
	if len(warnings) > 0 {
		lines = append(lines, "\nWarnings:")
		for _, w := range warnings {
			lines = append(lines, "  ⚠ "+w)
		}
	}

	if len(children) == 0 && len(dependents) == 0 && len(implementors) == 0 {
		lines = append(lines, "\nNo downstream impact — safe to archive.")
	}

	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleBulkEdge(ctx context.Context, edges []edgeInput, unlink bool) (*sdkmcp.CallToolResult, any, error) {
	if len(edges) == 0 {
		return nil, nil, fmt.Errorf("edges array is required for bulk_link/bulk_unlink") //nolint:err113 // agent-facing input validation
	}
	var lines []string
	for _, e := range edges {
		var results []parchment.Result
		var err error
		if unlink {
			results, err = h.proto.UnlinkArtifacts(ctx, e.From, e.Relation, []string{e.To})
		} else {
			results, err = h.proto.LinkArtifacts(ctx, e.From, e.Relation, []string{e.To})
		}
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, err))
			continue
		}
		for _, r := range results {
			if r.OK {
				verb := "linked"
				if unlink {
					verb = "unlinked"
				}
				lines = append(lines, fmt.Sprintf("%s %s -[%s]-> %s", verb, e.From, e.Relation, e.To))
			} else {
				lines = append(lines, fmt.Sprintf("%s -[%s]-> %s: error: %s", e.From, e.Relation, e.To, r.Error))
			}
		}
	}
	return text(strings.Join(lines, "\n")), nil, nil
}

func (h *handler) handleMove(ctx context.Context, id, newParent string) (*sdkmcp.CallToolResult, any, error) {
	art, err := h.proto.GetArtifact(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	oldParent := art.Parent
	results, err := h.proto.SetField(ctx, []string{id}, "parent", newParent)
	if err != nil {
		return nil, nil, err
	}
	if !results[0].OK {
		return nil, nil, fmt.Errorf("move %s: %s", id, results[0].Error) //nolint:err113 // agent-facing input validation
	}
	msg := fmt.Sprintf("moved %s: parent %s -> %s", id, oldParent, newParent)
	return text(msg), nil, nil
}

func (h *handler) handleReplace(ctx context.Context, id, relation, oldTarget, newTarget string) (*sdkmcp.CallToolResult, any, error) {
	// Unlink old
	results, err := h.proto.UnlinkArtifacts(ctx, id, relation, []string{oldTarget})
	if err != nil {
		return nil, nil, err
	}
	if len(results) > 0 && !results[0].OK {
		return nil, nil, fmt.Errorf("unlink old: %s", results[0].Error) //nolint:err113 // agent-facing input validation
	}
	// Link new
	results, err = h.proto.LinkArtifacts(ctx, id, relation, []string{newTarget})
	if err != nil {
		return nil, nil, err
	}
	if len(results) > 0 && !results[0].OK {
		return nil, nil, fmt.Errorf("link new: %s", results[0].Error) //nolint:err113 // agent-facing input validation
	}
	return text(fmt.Sprintf("replaced %s -[%s]-> %s with %s", id, relation, oldTarget, newTarget)), nil, nil
}

type detectInput struct {
	Check     string `json:"check,omitempty" jsonschema:"orphans, overlaps, knowledge, or all (default: all)"`
	Scope     string `json:"scope,omitempty"`
	Status    string `json:"status,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Project   string `json:"project,omitempty"`
	StaleDays int    `json:"stale_days,omitempty" jsonschema:"days before a fleeting note is considered stuck (default: 7)"`
}

