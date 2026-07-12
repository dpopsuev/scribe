package mcp

import (
	"context"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestOnInitialized_FallbackWithoutWorkspaceMeta(t *testing.T) {
	store := parchment.NewMemoryStore()
	proto := parchment.New(store, nil, []string{"test"}, nil, parchment.ProtocolConfig{})
	svc := service.New(proto, nil, nil)
	h := &handler{proto: proto, svc: svc}

	h.onInitialized(context.Background(), &sdkmcp.InitializedRequest{})
	if !h.workspaceConfigured {
		t.Fatal("expected workspaceConfigured after CWD fallback")
	}
	if len(h.workspaceLabels) == 0 {
		t.Fatal("expected workspace labels from Detect")
	}
	if len(svc.WorkspaceLabels) == 0 {
		t.Fatal("expected svc.WorkspaceLabels synced")
	}
}
