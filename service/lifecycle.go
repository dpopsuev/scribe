//nolint:goconst // status and Extra key literals
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// LifecycleWaiver is stamped when completing with unfinished descendants.
type LifecycleWaiver struct {
	Reason             string   `json:"reason"`
	IncompleteChildren []string `json:"incomplete_children,omitempty"`
	SessionID          string   `json:"session_id,omitempty"`
	Timestamp          string   `json:"timestamp"`
	Source             string   `json:"source"` // asserted
}

// IncompleteChildren lists non-terminal parent_of descendants (direct children).
func IncompleteChildren(ctx context.Context, svc *Service, id string) []string {
	children, err := svc.Proto.Store().Children(ctx, id)
	if err != nil {
		return nil
	}
	var out []string
	for _, ch := range children {
		st := parchment.StatusFromLabels(ch.Labels)
		if !svc.Proto.IsTerminal(st) {
			out = append(out, fmt.Sprintf("%s [%s]", ch.ID, st))
		}
	}
	return out
}

// WaiveComplete sets a terminal status despite incomplete children, with audit evidence.
func WaiveComplete(ctx context.Context, svc *Service, id, status, reason string) (*parchment.Artifact, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("waive_reason is required to complete with unfinished descendants") //nolint:err113 // agent-facing
	}
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return nil, err
	}
	incomplete := IncompleteChildren(ctx, svc, id)
	waiver := LifecycleWaiver{
		Reason:             reason,
		IncompleteChildren: incomplete,
		SessionID:          svc.SessionID,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		Source:             "asserted",
	}
	if art.Extra == nil {
		art.Extra = map[string]any{}
	}
	art.Extra["lifecycle_waiver"] = map[string]any{
		"reason":              waiver.Reason,
		"incomplete_children": waiver.IncompleteChildren,
		"session_id":          waiver.SessionID,
		"timestamp":           waiver.Timestamp,
		"source":              waiver.Source,
		"to_status":           status,
	}
	art.Labels = parchment.SetStatusLabel(art.Labels, status)
	if err := svc.Proto.Store().Put(ctx, art); err != nil {
		return nil, err
	}
	return art, nil
}

// ProposedIntentWarnings returns soft warnings when completing work that implements
// or is governed by a still-proposed decision/spec.
func ProposedIntentWarnings(ctx context.Context, svc *Service, id string) []string {
	var warnings []string
	for _, rel := range []string{parchment.RelImplements, parchment.RelSatisfies} {
		edges, _ := svc.Proto.Neighbors(ctx, id, rel, parchment.Outgoing)
		for _, e := range edges {
			tgt, err := svc.Proto.GetArtifact(ctx, e.To)
			if err != nil {
				continue
			}
			st := parchment.StatusFromLabels(tgt.Labels)
			if strings.Contains(st, "proposed") || st == "work.draft" {
				warnings = append(warnings, fmt.Sprintf(
					"warning: completing while %s %s is still %s — accept governing intent first or waive consciously",
					tgt.Label(parchment.LabelPrefixKind), tgt.ID, st))
			}
		}
	}
	if d, _ := ResolveCanonicalDecision(ctx, svc, id); d != nil {
		st := parchment.StatusFromLabels(d.Labels)
		if st == "decision.proposed" || st == "work.draft" {
			warnings = append(warnings, fmt.Sprintf(
				"warning: governing decision %s is %s (not accepted)", d.ID, st))
		}
	}
	return warnings
}

// LifecycleDriftPreview lists completed parents that still have unfinished children.
func LifecycleDriftPreview(ctx context.Context, svc *Service, scope string) []map[string]any {
	li := parchment.ListInput{}
	if scope != "" {
		li.Labels = []string{parchment.LabelPrefixScope + scope}
	}
	arts, _ := svc.Proto.ListArtifacts(ctx, li)
	var out []map[string]any
	for _, art := range arts {
		st := parchment.StatusFromLabels(art.Labels)
		if !svc.Proto.IsTerminal(st) {
			continue
		}
		incomplete := IncompleteChildren(ctx, svc, art.ID)
		if len(incomplete) == 0 {
			continue
		}
		out = append(out, map[string]any{
			"id":                  art.ID,
			"title":               art.Title,
			"status":              st,
			"incomplete_children": incomplete,
			"fix":                 fmt.Sprintf("set(id=%s, field=status, value=%s, waive_reason=...) or complete children first", art.ID, st),
		})
	}
	return out
}
