package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type deleteInput struct {
	ID            string   `json:"id,omitempty"`
	IDs           []string `json:"ids,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Scope         string   `json:"scope,omitempty"`
	Status        string   `json:"status,omitempty"`
	Query         string   `json:"query,omitempty"`
	Labels        []string `json:"labels,omitempty"`
	ExcludeLabels []string `json:"exclude_labels,omitempty"`
	DryRun        bool     `json:"dry_run,omitempty"`
	Force         bool     `json:"force,omitempty"`
}

var opDelete = Op{
	Name: "delete",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in deleteInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}

		ids := in.IDs
		if in.ID != "" {
			ids = append(ids, in.ID)
		}

		if len(ids) == 0 {
			if in.Kind == "" && in.Scope == "" && in.Query == "" && len(in.Labels) == 0 {
				return "", fmt.Errorf("id, ids, or query filters (kind/scope/status/query/labels) required") //nolint:err113 // agent-facing
			}
			var labels []string
			if in.Kind != "" {
				labels = append(labels, parchment.LabelPrefixKind+in.Kind)
			}
			if in.Scope != "" {
				labels = append(labels, parchment.LabelPrefixScope+in.Scope)
			}
			if in.Status != "" {
				labels = append(labels, statusLabelFor(in.Status))
			}
			labels = append(labels, in.Labels...)

			li := parchment.ListInput{Labels: labels, ExcludeLabels: in.ExcludeLabels, Query: in.Query}
			var arts []*parchment.Artifact
			var err error
			if li.Query != "" {
				arts, err = svc.Proto.SearchArtifacts(ctx, li.Query, li)
			} else {
				arts, err = svc.Proto.ListArtifacts(ctx, li)
			}
			if err != nil {
				return "", err
			}
			for _, a := range arts {
				ids = append(ids, a.ID)
			}
		}

		if len(ids) == 0 {
			return "no matching artifacts to delete", nil
		}

		if in.DryRun {
			var b strings.Builder
			fmt.Fprintf(&b, "dry run: would delete %d artifact(s):\n", len(ids))
			for _, id := range ids {
				art, err := svc.Proto.GetArtifact(ctx, id)
				if err != nil {
					fmt.Fprintf(&b, "  %s (not found)\n", id)
					continue
				}
				fmt.Fprintf(&b, "  %s  %s\n", id, art.Title)
			}
			return b.String(), nil
		}

		var deleted int
		for _, id := range ids {
			if err := svc.Proto.DeleteArtifact(ctx, id, in.Force); err != nil {
				slog.WarnContext(ctx, "delete failed",
					slog.String(parchment.LogKeyID, id), slog.Any(parchment.LogKeyError, err))
				continue
			}
			deleted++
		}
		return fmt.Sprintf("deleted %d artifact(s)", deleted), nil
	},
}
