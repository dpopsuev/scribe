package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opCommentAdd, opCommentList)
}

const (
	edgeSourceComment = "comment"
	labelRoleComment  = "role:comment"
	labelOnPrefix     = "on:"
	commentTitleMax   = 72
)

type commentAddInput struct {
	ID     string `json:"id"` // issue / artifact being discussed
	Text   string `json:"text"`
	Author string `json:"author,omitempty"`
	Title  string `json:"title,omitempty"`
	Scope  string `json:"scope,omitempty"`
}

type commentListInput struct {
	ID    string `json:"id"`
	Since int64  `json:"since,omitempty"` // unix ms; omit = all
	Limit int    `json:"limit,omitempty"`
}

var opCommentAdd = Op{
	Name: "comment_add",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in commentAddInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" || strings.TrimSpace(in.Text) == "" {
			return "", fmt.Errorf("comment_add requires id= and text=") //nolint:err113 // agent-facing
		}
		if _, err := svc.Proto.GetArtifact(ctx, in.ID); err != nil {
			return "", fmt.Errorf("target %s: %w", in.ID, err)
		}

		title := in.Title
		if title == "" {
			title = truncateTitle(in.Text, commentTitleMax)
		}
		labels := []string{kindLabelKnowledge, labelRoleComment, labelOnPrefix + in.ID}
		if in.Scope != "" {
			labels = append(labels, parchment.LabelPrefixScope+in.Scope)
		}
		body := in.Text
		if in.Author != "" {
			body = "@" + in.Author + ": " + body
		}
		art, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Title:    title,
			Labels:   labels,
			Sections: []parchment.Section{{Name: sectionKeyBody, Text: body}},
		})
		if err != nil {
			return "", fmt.Errorf("create comment: %w", err)
		}
		if err := svc.Proto.Store().AddEdgeSource(ctx, art.ID, parchment.RelDiscusses, in.ID, edgeSourceComment); err != nil {
			return "", fmt.Errorf("discusses link: %w", err)
		}
		return fmt.Sprintf("comment %s discusses %s", art.ID, in.ID), nil
	},
}

var opCommentList = Op{
	Name: "comment_list",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in commentListInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" {
			return "", fmt.Errorf("comment_list requires id=") //nolint:err113 // agent-facing
		}
		edges, err := svc.Proto.Store().Neighbors(ctx, in.ID, parchment.RelDiscusses, parchment.Incoming)
		if err != nil {
			return "", err
		}
		type row struct {
			art *parchment.Artifact
			ms  int64
		}
		rows := make([]row, 0, len(edges))
		for _, e := range edges {
			art, err := svc.Proto.GetArtifact(ctx, e.From)
			if err != nil {
				continue
			}
			ms := art.UpdatedAt.UnixMilli()
			if in.Since > 0 && ms <= in.Since {
				continue
			}
			rows = append(rows, row{art: art, ms: ms})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ms < rows[j].ms })
		limit := in.Limit
		if limit <= 0 {
			limit = 50
		}
		if len(rows) > limit {
			rows = rows[:limit]
		}
		if len(rows) == 0 {
			return "(no comments)", nil
		}
		var b strings.Builder
		for _, r := range rows {
			body := commentBody(r.art)
			fmt.Fprintf(&b, "%s\t%d\t%s\n%s\n---\n", r.art.ID, r.ms, r.art.Title, body)
		}
		return strings.TrimSuffix(b.String(), "---\n"), nil
	},
}

func truncateTitle(text string, maxLen int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-1] + "…"
}

func commentBody(art *parchment.Artifact) string {
	for _, s := range art.Sections {
		if s.Name == sectionKeyBody {
			return s.Text
		}
	}
	return ""
}
