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
	opActionGet    = "get"
	opActionUpdate = "update"
)

func (h *handler) handleArtifact(ctx context.Context, req *sdkmcp.CallToolRequest, in artifactInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	// Route aliases to canonical ops before registry lookup.
	switch in.Action {
	case "tree":
		in.Action = opActionGet
		in.Format = "tree"
	case "briefing":
		in.Action = opActionGet
		in.Format = "briefing"
	case "impact":
		in.Action = opActionGet
		in.Format = "impact"
	case "diff":
		in.Action = opActionGet
	case "recall":
		in.Action = "list"
		in.Ranked = true
	case "unlink":
		in.Action = "link"
		in.Mode = "remove"
	case "attach_section", "bulk_section_update":
		in.Action = opActionUpdate
	case "detach_section":
		in.Action = opActionUpdate
	case "catalog":
		in.Action = "orient"
	case "retire":
		in.Action = opActionUpdate
		in.Status = "retired"
	}
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

func newSessionID() string { return service.NewSessionID() }

func loadReadLog(ctx context.Context, store parchment.Store, proto *parchment.Protocol, sessionID string) map[string]bool {
	return service.LoadReadLog(ctx, store, proto, sessionID)
}
