package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

func (h *handler) recordTurn(ctx context.Context, action string, raw json.RawMessage) {
	if !h.recordSession || h.proto == nil {
		return
	}
	if h.sessionArtifactID == "" {
		sess, err := h.proto.CreateArtifact(ctx, parchment.CreateInput{
			Title:  fmt.Sprintf("session %s", h.svc.SessionID),
			Labels: []string{"kind:ctx.session"},
		})
		if err != nil {
			slog.WarnContext(ctx, "session recording: create session failed", slog.Any(parchment.LogKeyError, err))
			return
		}
		h.sessionArtifactID = sess.ID
	}
	input := string(raw)
	if len(input) > 500 {
		input = input[:500] + "…"
	}
	_, _ = h.proto.CreateArtifact(ctx, parchment.CreateInput{ //nolint:gosec // advisory recording
		Title:    fmt.Sprintf("turn: %s", action),
		Labels:   []string{"kind:ctx.turn"},
		Parent:   h.sessionArtifactID,
		Sections: []parchment.Section{{Name: "content", Text: input}},
	})
}

func (h *handler) handleArtifact(ctx context.Context, req *sdkmcp.CallToolRequest, in artifactInput) (*sdkmcp.CallToolResult, any, error) { //nolint:gocritic // hugeParam: value semantics intentional
	start := time.Now()
	defer func() {
		slog.InfoContext(ctx, "mcp",
			slog.String(parchment.LogKeyOp, in.Action),
			slog.String("dur", time.Since(start).Round(time.Microsecond).String()), //nolint:sloglint // request-scoped timing
		)
	}()
	// Workspace stamping on create/query.
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

	// Attachment actions are handled here; they do not go through service.Find.
	switch in.Action {
	case "attach":
		return h.handleAttach(ctx, &in)
	case "detach":
		return h.handleDetach(ctx, &in)
	}

	if op := service.Find(in.Action); op != nil {
		raw, _ := json.Marshal(in)
		if isWriteAction(in.Action) {
			h.recordTurn(ctx, in.Action, raw)
		}
		out, err := op.Run(ctx, h.svc, raw)
		if err != nil {
			return nil, nil, err
		}
		// For get: append image content blocks when attachments exist.
		if in.Action == "get" && in.ID != "" {
			return h.buildGetResult(ctx, in.ID, out)
		}
		// Warn on write operations only — read responses must not be polluted.
		if !h.workspaceConfigured && isWriteAction(in.Action) {
			out += workspaceUnconfiguredSuffix
		}
		return text(out), nil, nil
	}
	return nil, nil, fmt.Errorf("unknown artifact action %q", in.Action) //nolint:err113 // agent-facing hint
}

// handleAttach stores a base64-encoded binary blob as a named attachment.
func (h *handler) handleAttach(ctx context.Context, in *artifactInput) (*sdkmcp.CallToolResult, any, error) {
	if in.ID == "" || in.Name == "" || in.Data == "" || in.ContentType == "" {
		return nil, nil, fmt.Errorf("attach requires id, name, content_type, and data") //nolint:err113 // agent-facing
	}
	decoded, err := base64.StdEncoding.DecodeString(in.Data)
	if err != nil {
		// Try raw base64 without padding.
		decoded, err = base64.RawStdEncoding.DecodeString(in.Data)
		if err != nil {
			return nil, nil, fmt.Errorf("attach: data must be base64-encoded: %w", err)
		}
	}
	if err := h.svc.Proto.Store().PutAttachment(ctx, in.ID, in.Name, in.ContentType, decoded); err != nil {
		return nil, nil, err
	}
	out := fmt.Sprintf("attached %s (%s, %d bytes) to %s", in.Name, in.ContentType, len(decoded), in.ID)
	if !h.workspaceConfigured {
		out += workspaceUnconfiguredSuffix
	}
	return text(out), nil, nil
}

// handleDetach removes a named attachment from an artifact.
func (h *handler) handleDetach(ctx context.Context, in *artifactInput) (*sdkmcp.CallToolResult, any, error) {
	if in.ID == "" || in.Name == "" {
		return nil, nil, fmt.Errorf("detach requires id and name") //nolint:err113 // agent-facing
	}
	if err := h.svc.Proto.Store().DeleteAttachment(ctx, in.ID, in.Name); err != nil {
		return nil, nil, err
	}
	out := fmt.Sprintf("detached %s from %s", in.Name, in.ID)
	if !h.workspaceConfigured {
		out += workspaceUnconfiguredSuffix
	}
	return text(out), nil, nil
}

// buildGetResult assembles a mixed MCP content result for action=get.
// Text sections are in the first TextContent block; each image attachment
// becomes an ImageContent block so vision-capable models see them inline.
func (h *handler) buildGetResult(ctx context.Context, id, textOut string) (*sdkmcp.CallToolResult, any, error) {
	attachments, err := h.svc.Proto.Store().GetAttachments(ctx, id)
	if err != nil || len(attachments) == 0 {
		return text(textOut), nil, nil
	}
	content := []sdkmcp.Content{&sdkmcp.TextContent{Text: textOut}}
	for _, a := range attachments {
		if !strings.HasPrefix(a.ContentType, "image/") {
			continue // non-image MIME types are not passed to the model
		}
		content = append(content, &sdkmcp.ImageContent{
			MIMEType: a.ContentType,
			Data:     a.Data,
		})
	}
	return &sdkmcp.CallToolResult{Content: content}, nil, nil
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
