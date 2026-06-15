package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

type recentInput struct {
	Scope string `json:"scope,omitempty"`
	Since string `json:"since,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

var opRecent = Op{
	Name: "recent",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in recentInput
		_ = json.Unmarshal(raw, &in)

		since := time.Now().Add(-24 * time.Hour)
		if in.Since != "" {
			if d, err := time.ParseDuration(in.Since); err == nil {
				since = time.Now().Add(-d)
			} else if t, err := time.Parse(time.RFC3339, in.Since); err == nil {
				since = t
			}
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}

		var labels []string
		if in.Scope != "" {
			labels = append(labels, parchment.LabelPrefixScope+in.Scope)
		}
		arts, err := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels:       labels,
			UpdatedAfter: since.Format(time.RFC3339),
		})
		if err != nil {
			return "", err
		}
		if len(arts) > limit {
			arts = arts[:limit]
		}
		if len(arts) == 0 {
			return "no recent changes", nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "recent changes (since %s):\n\n", since.Format("2006-01-02 15:04"))
		for _, a := range arts {
			kind := a.Label(parchment.LabelPrefixKind)
			status := parchment.StatusFromLabels(a.Labels)
			fmt.Fprintf(&b, "%s  %-14s %-12s %s\n",
				a.UpdatedAt.Format("01-02 15:04"),
				kind, status, a.Title)
		}
		fmt.Fprintf(&b, "\n(%d artifacts)\n", len(arts))
		return b.String(), nil
	},
}
