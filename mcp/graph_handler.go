package mcp

import (
	"context"
	"encoding/json"
	"fmt"

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
