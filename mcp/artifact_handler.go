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
	case "briefing", "replace": //nolint:goconst // action name strings
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
