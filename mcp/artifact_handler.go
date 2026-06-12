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
	return nil, nil, fmt.Errorf("unknown artifact action %q", in.Action) //nolint:err113 // agent-facing hint
}

func (h *handler) handleRelationship(ctx context.Context, _ *sdkmcp.CallToolRequest, in relationshipInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	if op := service.Find(in.Action); op != nil {
		raw, _ := json.Marshal(in)
		out, err := op.Run(ctx, h.svc, raw)
		if err != nil {
			return nil, nil, err
		}
		return text(out), nil, nil
	}
	return nil, nil, fmt.Errorf("unknown relationship action %q", in.Action) //nolint:err113 // agent-facing hint
}

func newSessionID() string { return service.NewSessionID() }

func loadReadLog(ctx context.Context, store parchment.Store, proto *parchment.Protocol, sessionID string) map[string]bool {
	return service.LoadReadLog(ctx, store, proto, sessionID)
}
