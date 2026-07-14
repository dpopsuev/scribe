package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opMessageAdd, opMessageList, opCursorMark, opCursorGet)
}

const (
	edgeSourceComment  = "comment"
	edgeSourceMessage  = "message"
	labelRoleComment   = "role:comment"
	labelRoleMessage   = "role:message"
	labelOnPrefix      = "on:"
	kindLabelSession   = "kind:agent.session"
	commentTitleMax    = 72
	messageStreamLimit = 50
	extraReadCursors   = "read_cursors"
	msgEmptyStream     = "(no messages)"
	modeChildren       = "children"
	modeDiscusses      = "discusses"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

type messageCreateOpts struct {
	Text      string
	Author    string
	Title     string
	Scope     string
	Role      string // role:comment | role:message
	Discusses string // optional target id
	Parent    string // optional container
	EdgeSrc   string
}

func createMessage(ctx context.Context, svc *Service, opts messageCreateOpts) (*parchment.Artifact, error) {
	if strings.TrimSpace(opts.Text) == "" {
		return nil, fmt.Errorf("message text is required") //nolint:err113 // agent-facing
	}
	if opts.Discusses != "" {
		if _, err := svc.Proto.GetArtifact(ctx, opts.Discusses); err != nil {
			return nil, fmt.Errorf("discusses target %s: %w", opts.Discusses, err)
		}
	}
	if opts.Parent != "" {
		if _, err := svc.Proto.GetArtifact(ctx, opts.Parent); err != nil {
			return nil, fmt.Errorf("parent %s: %w", opts.Parent, err)
		}
	}
	title := opts.Title
	if title == "" {
		title = truncateTitle(opts.Text, commentTitleMax)
	}
	role := opts.Role
	if role == "" {
		role = labelRoleMessage
	}
	labels := []string{kindLabelKnowledge, role}
	if opts.Discusses != "" {
		labels = append(labels, labelOnPrefix+opts.Discusses)
	}
	if opts.Scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+opts.Scope)
	}
	body := opts.Text
	if opts.Author != "" {
		body = "@" + opts.Author + ": " + body
	}
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:    title,
		Labels:   labels,
		Parent:   opts.Parent,
		Sections: []parchment.Section{{Name: sectionKeyBody, Text: body}},
	})
	if err != nil {
		return nil, fmt.Errorf("create message: %w", err)
	}
	if opts.Discusses != "" {
		src := opts.EdgeSrc
		if src == "" {
			src = edgeSourceMessage
		}
		if err := svc.Proto.Store().AddEdgeSource(ctx, art.ID, parchment.RelDiscusses, opts.Discusses, src); err != nil {
			return nil, fmt.Errorf("discusses link: %w", err)
		}
	}
	return art, nil
}

type messageAddInput struct {
	Text      string `json:"text"`
	Content   string `json:"content,omitempty"`
	Author    string `json:"author,omitempty"`
	Title     string `json:"title,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Discusses string `json:"discusses,omitempty"`
}

type messageListInput struct {
	ID      string `json:"id"`             // parent id (children) or discussed target (discusses)
	Mode    string `json:"mode,omitempty"` // children | discusses (default children if parent-like)
	Since   int64  `json:"since,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Session string `json:"session,omitempty"` // with since=0, use read_cursors[key]
	Key     string `json:"key,omitempty"`     // cursor key; default = id
}

type cursorInput struct {
	Session string `json:"session"`
	Key     string `json:"key"`
	Since   int64  `json:"since,omitempty"`
	Scope   string `json:"scope,omitempty"`
}

var opMessageAdd = Op{
	Name: "message_add",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in messageAddInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		text := strings.TrimSpace(in.Text)
		if text == "" {
			text = strings.TrimSpace(in.Content)
		}
		if text == "" {
			return "", fmt.Errorf("message_add requires text= or content=") //nolint:err113 // agent-facing
		}
		if in.Parent == "" && in.Discusses == "" {
			return "", fmt.Errorf("message_add requires parent= and/or discusses=") //nolint:err113 // agent-facing
		}
		art, err := createMessage(ctx, svc, messageCreateOpts{
			Text:      text,
			Author:    in.Author,
			Title:     in.Title,
			Scope:     in.Scope,
			Role:      labelRoleMessage,
			Discusses: in.Discusses,
			Parent:    in.Parent,
			EdgeSrc:   edgeSourceMessage,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("message %s", art.ID), nil
	},
}

var opMessageList = Op{
	Name: "message_list",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in messageListInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" {
			return "", fmt.Errorf("message_list requires id=") //nolint:err113 // agent-facing
		}
		mode := strings.ToLower(strings.TrimSpace(in.Mode))
		if mode == "" {
			mode = modeChildren
		}
		since := in.Since
		if since == 0 && in.Session != "" {
			key := in.Key
			if key == "" {
				key = in.ID
			}
			if c, ok := getReadCursor(ctx, svc, in.Session, key); ok {
				since = c
			}
		}
		var rows []streamRow
		var err error
		switch mode {
		case modeDiscusses:
			rows, err = collectDiscussesStream(ctx, svc, in.ID, since, in.Limit)
		case modeChildren:
			rows, err = collectChildrenStream(ctx, svc, in.ID, since, in.Limit)
		default:
			return "", fmt.Errorf("message_list mode must be children|discusses") //nolint:err113 // agent-facing
		}
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return msgEmptyStream, nil
		}
		return formatMessageStream(rows), nil
	},
}

var opCursorMark = Op{
	Name: "cursor_mark",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in cursorInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Session == "" || in.Key == "" {
			return "", fmt.Errorf("cursor_mark requires session= and key=") //nolint:err113 // agent-facing
		}
		since := in.Since
		if since == 0 {
			since = time.Now().UTC().UnixMilli()
		}
		sess, err := ensureSessionArtifact(ctx, svc, in.Session, in.Scope)
		if err != nil {
			return "", err
		}
		cursors := readCursorsFromExtra(sess.Extra)
		cursors[in.Key] = since
		if err := svc.Proto.PatchArtifact(ctx, sess.ID, parchment.ArtifactPatch{
			SetExtra: map[string]any{extraReadCursors: cursors},
		}); err != nil {
			return "", err
		}
		return fmt.Sprintf("cursor %s %s = %d", in.Session, in.Key, since), nil
	},
}

var opCursorGet = Op{
	Name: "cursor_get",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in cursorInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.Session == "" || in.Key == "" {
			return "", fmt.Errorf("cursor_get requires session= and key=") //nolint:err113 // agent-facing
		}
		c, ok := getReadCursor(ctx, svc, in.Session, in.Key)
		if !ok {
			return "0", nil
		}
		return fmt.Sprintf("%d", c), nil
	},
}

type streamRow struct {
	art *parchment.Artifact
	ms  int64
}

func formatMessageStream(rows []streamRow) string {
	if len(rows) == 0 {
		return msgEmptyStream
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%d\t%s\n%s\n---\n", r.art.ID, r.ms, r.art.Title, messageBody(r.art))
	}
	return strings.TrimSuffix(b.String(), "---\n")
}

func collectDiscussesStream(ctx context.Context, svc *Service, targetID string, since int64, limit int) ([]streamRow, error) {
	edges, err := svc.Proto.Store().Neighbors(ctx, targetID, parchment.RelDiscusses, parchment.Incoming)
	if err != nil {
		return nil, err
	}
	rows := make([]streamRow, 0, len(edges))
	for _, e := range edges {
		art, err := svc.Proto.GetArtifact(ctx, e.From)
		if err != nil {
			continue
		}
		ms := art.UpdatedAt.UnixMilli()
		if since > 0 && ms <= since {
			continue
		}
		rows = append(rows, streamRow{art: art, ms: ms})
	}
	return sortAndLimitRows(rows, limit), nil
}

func collectChildrenStream(ctx context.Context, svc *Service, parentID string, since int64, limit int) ([]streamRow, error) {
	children, err := svc.Proto.Children(ctx, parentID)
	if err != nil {
		return nil, err
	}
	rows := make([]streamRow, 0, len(children))
	for _, art := range children {
		ms := art.UpdatedAt.UnixMilli()
		if since > 0 && ms <= since {
			continue
		}
		rows = append(rows, streamRow{art: art, ms: ms})
	}
	return sortAndLimitRows(rows, limit), nil
}

func sortAndLimitRows(rows []streamRow, limit int) []streamRow {
	sort.Slice(rows, func(i, j int) bool { return rows[i].ms < rows[j].ms })
	if limit <= 0 {
		limit = messageStreamLimit
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func truncateTitle(text string, maxLen int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-1] + "…"
}

func messageBody(art *parchment.Artifact) string {
	for _, s := range art.Sections {
		if s.Name == sectionKeyBody {
			return s.Text
		}
	}
	return ""
}

func slugPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := nonSlug.ReplaceAllString(b.String(), "-")
	out = strings.Trim(out, "-")
	if out == "" {
		return "x"
	}
	return out
}

func ensureSessionArtifact(ctx context.Context, svc *Service, session, scope string) (*parchment.Artifact, error) {
	id := "agent-session-" + slugPart(session)
	if art, err := svc.Proto.GetArtifact(ctx, id); err == nil {
		return art, nil
	}
	labels := []string{kindLabelSession, "session:" + session}
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}
	art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		ExplicitID: id,
		Title:      "session " + session,
		Labels:     labels,
		Sections:   []parchment.Section{{Name: "model", Text: "read-cursor"}},
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return art, nil
}

func readCursorsFromExtra(extra map[string]any) map[string]any {
	out := map[string]any{}
	if extra == nil {
		return out
	}
	raw, ok := extra[extraReadCursors]
	if !ok {
		return out
	}
	if m, ok := raw.(map[string]any); ok {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func getReadCursor(ctx context.Context, svc *Service, session, key string) (int64, bool) {
	id := "agent-session-" + slugPart(session)
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return 0, false
	}
	cursors := readCursorsFromExtra(art.Extra)
	v, ok := cursors[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}
