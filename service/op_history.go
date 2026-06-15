package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

type historyInput struct {
	ID    string `json:"id,omitempty"`
	Scope string `json:"scope,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

var opHistory = Op{
	Name: "history",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in historyInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" && in.Scope == "" {
			return "", fmt.Errorf("id or scope required") //nolint:err113 // agent-facing
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}

		filter := parchment.EventFilter{
			ArtifactID: in.ID,
			Scope:      in.Scope,
		}
		events, err := svc.Proto.GetEvents(ctx, time.Time{}, filter)
		if err != nil {
			return "", err
		}

		if len(events) > limit {
			events = events[len(events)-limit:]
		}
		if len(events) == 0 {
			return "no events found", nil
		}

		var b strings.Builder
		for _, e := range events {
			ts := e.Ts.Format("2006-01-02 15:04")
			fmt.Fprintf(&b, "%s  %-16s  %s", ts, e.EventType, e.ArtifactID)
			if e.Actor != "" {
				fmt.Fprintf(&b, "  actor=%s", e.Actor)
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "\n(%d events)\n", len(events))
		return b.String(), nil
	},
}
