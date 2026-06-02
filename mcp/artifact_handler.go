package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *handler) handleArtifact(ctx context.Context, req *sdkmcp.CallToolRequest, in artifactInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if op := service.Find(in.Action); op != nil {
		raw, _ := json.Marshal(in)
		out, err := op.Run(ctx, h.svc, raw)
		if err != nil {
			return nil, nil, err
		}
		return text(out), nil, nil
	}
	switch in.Action {
	case "replace":
		return h.handleGraph(ctx, req, graphInput{
			Action:    in.Action,
			ID:        in.ID,
			Relation:  in.Relation,
			Direction: in.Direction,
			Depth:     in.Depth,
			Unblocked: in.Unblocked,
			Targets:   in.Targets,
			Target:    in.Target,
			OldTarget: in.OldTarget,
			Edges:     in.Edges,
			Format:    in.Format,
		})
	default:
		return nil, nil, fmt.Errorf("unknown artifact action %q", in.Action) //nolint:err113 // agent-facing hint
	}
}

func newSessionID() string { return service.NewSessionID() }

func (h *handler) persistReadLog(ctx context.Context) {
	h.svc.PersistReadLog(ctx, h.svc.SessionID, h.svc.ReadLog)
}

func loadReadLog(ctx context.Context, store parchment.Store, proto *parchment.Protocol, sessionID string) map[string]bool {
	return service.LoadReadLog(ctx, store, proto, sessionID)
}

func (h *handler) handleGraph(ctx context.Context, _ *sdkmcp.CallToolRequest, in graphInput) (*sdkmcp.CallToolResult, any, error) {
	if in.Action == "replace" {
		return h.handleReplace(ctx, in.ID, in.Relation, in.OldTarget, in.Target)
	}
	return nil, nil, fmt.Errorf("unknown graph action %q", in.Action) //nolint:err113 // agent-facing hint
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
