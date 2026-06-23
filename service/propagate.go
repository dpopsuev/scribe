package service

import (
	"context"
	"log/slog"

	parchment "github.com/dpopsuev/parchment"
)

// PropagateResult summarizes kernel invalidation after pointer changes.
type PropagateResult struct {
	PointerID    string   `json:"pointer_id"`
	Invalidated  []string `json:"invalidated,omitempty"`
	AlreadyStale []string `json:"already_stale,omitempty"`
	Errors       []string `json:"errors,omitempty"`
}

// PropagateChanges finds all kernels that trace_to the changed pointer
// and resets their status to kernel.pending so they are re-reviewed.
// Only kernels in kernel.confirmed state are invalidated; those already
// pending or rejected are left alone.
func PropagateChanges(ctx context.Context, proto *parchment.Protocol, pointerID string) *PropagateResult {
	result := &PropagateResult{PointerID: pointerID}

	store := proto.Store()
	incoming, err := store.Neighbors(ctx, pointerID, "traces_to", parchment.Incoming)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}

	for _, edge := range incoming {
		kernelID := edge.From
		art, err := store.Get(ctx, kernelID)
		if err != nil {
			continue
		}

		kind := art.Label(parchment.LabelPrefixKind)
		if kind != KindKernel {
			continue
		}

		status := parchment.StatusFromLabels(art.Labels)
		switch status {
		case StatusKernelConfirmed:
			results, err := proto.SetField(ctx, []string{kernelID}, "status", StatusKernelPending, parchment.SetFieldOptions{Force: true})
			if err != nil {
				result.Errors = append(result.Errors, kernelID+": "+err.Error())
				continue
			}
			if len(results) > 0 && results[0].OK {
				result.Invalidated = append(result.Invalidated, kernelID)
			} else if len(results) > 0 {
				result.Errors = append(result.Errors, kernelID+": "+results[0].Error)
			}
		case StatusKernelPending, StatusKernelRejected:
			result.AlreadyStale = append(result.AlreadyStale, kernelID)
		}
	}

	if len(result.Invalidated) > 0 {
		slog.InfoContext(ctx, "propagate: kernels invalidated",
			slog.String("pointer", pointerID),          //nolint:sloglint // domain-specific key
			slog.Int("count", len(result.Invalidated))) //nolint:sloglint // domain-specific key
	}

	return result
}

// PropagateFromChanges processes a RematerializeResult and propagates
// changes to dependent kernels for every updated pointer.
func PropagateFromChanges(ctx context.Context, proto *parchment.Protocol, remat *RematerializeResult) []PropagateResult {
	var results []PropagateResult
	for _, change := range remat.Changes {
		if change.Change != ChangeUpdated {
			continue
		}
		pr := PropagateChanges(ctx, proto, change.ID)
		if len(pr.Invalidated) > 0 || len(pr.Errors) > 0 {
			results = append(results, *pr)
		}
	}
	return results
}
