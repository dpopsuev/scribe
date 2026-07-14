package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func init() {
	Registry = append(Registry, opCommentAdd, opCommentList)
}

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
		art, err := createMessage(ctx, svc, messageCreateOpts{
			Text:      in.Text,
			Author:    in.Author,
			Title:     in.Title,
			Scope:     in.Scope,
			Role:      labelRoleComment,
			Discusses: in.ID,
			EdgeSrc:   edgeSourceComment,
		})
		if err != nil {
			return "", err
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
		rows, err := collectDiscussesStream(ctx, svc, in.ID, in.Since, in.Limit)
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			return "(no comments)", nil
		}
		return formatMessageStream(rows), nil
	},
}
