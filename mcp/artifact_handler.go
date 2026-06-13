package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
	"github.com/dpopsuev/scribe/service"
	"github.com/dpopsuev/scribe/workspace"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	logKeyWorkspace             = "workspace_labels"
	workspaceUnconfiguredSuffix = "\n⚠ workspace unset — artifacts have no repository context"
	actionCreate                = "create"
)

// onInitialized is called by the MCP SDK after the client sends the
// initialized notification. For HTTP transport, clients declare their
// workspace context in the initialize _meta field; this handler reads it,
// runs the detectors, and stamps the session with the resulting labels.
func (h *handler) onInitialized(ctx context.Context, req *sdkmcp.InitializedRequest) {
	if h.workspaceConfigured {
		return // stdio: already set from server CWD at startup
	}
	if req == nil || req.Params == nil {
		return
	}
	wsRaw, ok := req.Params.Meta["workspace"]
	if !ok {
		return
	}
	wsMap, ok := wsRaw.(map[string]any)
	if !ok {
		return
	}
	inputs := workspace.WorkspaceInputs{
		CWD:       stringFromMap(wsMap, "cwd"),
		GitRemote: stringFromMap(wsMap, "git_remote"),
	}
	labels := workspace.Detect(inputs, workspace.DefaultDetectors())
	if len(labels) > 0 {
		h.workspaceLabels = labels
		h.workspaceConfigured = true
		slog.InfoContext(ctx, "workspace configured from client", slog.Any(logKeyWorkspace, labels))
	}
}

// stringFromMap safely reads a string value from a map[string]any.
func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (h *handler) handleArtifact(ctx context.Context, req *sdkmcp.CallToolRequest, in artifactInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	// Stamper: merge workspace labels into create/query operations.
	if len(h.workspaceLabels) > 0 {
		switch in.Action {
		case actionCreate:
			in.Labels = mergeLabels(in.Labels, h.workspaceLabels)
		case "query":
			if len(in.Labels) == 0 && in.Kind == "" && in.Query == "" {
				in.Labels = h.workspaceLabels
			}
		}
	}

	if op := service.Find(in.Action); op != nil {
		raw, _ := json.Marshal(in)
		out, err := op.Run(ctx, h.svc, raw)
		if err != nil {
			return nil, nil, err
		}
		// Warn on write operations only — read responses carry content that must not be polluted.
		if !h.workspaceConfigured && isWriteAction(in.Action) {
			out += workspaceUnconfiguredSuffix
		}
		return text(out), nil, nil
	}
	return nil, nil, fmt.Errorf("unknown artifact action %q", in.Action) //nolint:err113 // agent-facing hint
}

// isWriteAction reports whether the action mutates the graph.
func isWriteAction(action string) bool {
	switch action {
	case actionCreate, "set", "update", "link":
		return true
	}
	return false
}

// mergeLabels returns dst with any labels from src that are not already present.
func mergeLabels(dst, src []string) []string {
	existing := make(map[string]bool, len(dst))
	for _, l := range dst {
		existing[l] = true
	}
	for _, l := range src {
		if !existing[l] {
			dst = append(dst, l)
		}
	}
	return dst
}

func newSessionID() string { return service.NewSessionID() }

func loadReadLog(ctx context.Context, store parchment.Store, proto *parchment.Protocol, sessionID string) map[string]bool {
	return service.LoadReadLog(ctx, store, proto, sessionID)
}
