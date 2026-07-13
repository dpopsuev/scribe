//nolint:goconst // mutation action/status literals
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
	Name:       "delete",
	Structured: runDeleteStructured,
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		r, err := runDeleteStructured(ctx, svc, raw)
		return r.Text, err
	},
}

func runDeleteStructured(ctx context.Context, svc *Service, raw json.RawMessage) (Result, error) {
	var in deleteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{}, err
	}

	ids := resolveIDs(in.IDs, in.ID)
	if len(ids) == 0 {
		resolved, err := resolveDeleteFilter(ctx, svc, in)
		if err != nil {
			return Result{}, err
		}
		ids = resolved
	}

	if len(ids) == 0 {
		mr := MutationResult{Action: "delete", Status: "ok", Count: 0, DryRun: in.DryRun}
		return Result{Text: "no matching artifacts to delete", Data: mr}, nil
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
		mr := MutationResult{Action: "delete", Status: "dry_run", DryRun: true, IDs: ids, Count: len(ids)}
		return Result{Text: b.String(), Data: mr}, nil
	}

	var deleted []string
	var warnings []string
	for _, id := range ids {
		if err := svc.Proto.DeleteArtifact(ctx, id, in.Force); err != nil {
			slog.WarnContext(ctx, "delete failed",
				slog.String(parchment.LogKeyID, id), slog.Any(parchment.LogKeyError, err))
			warnings = append(warnings, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		deleted = append(deleted, id)
	}
	mr := MutationResult{Action: "delete", Status: "ok", IDs: deleted, Count: len(deleted), Warnings: warnings}
	return Result{Text: fmt.Sprintf("deleted %d artifact(s)", len(deleted)), Data: mr}, nil
}

func resolveDeleteFilter(ctx context.Context, svc *Service, in deleteInput) ([]string, error) {
	if in.Kind == "" && in.Scope == "" && in.Query == "" && len(in.Labels) == 0 {
		return nil, fmt.Errorf("id, ids, or query filters (kind/scope/status/query/labels) required") //nolint:err113 // agent-facing
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
		return nil, err
	}
	ids := make([]string, 0, len(arts))
	for _, a := range arts {
		ids = append(ids, a.ID)
	}
	return ids, nil
}
