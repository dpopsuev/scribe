//nolint:goconst,gocognit,gocritic // structured hygiene payload keys
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

// hygieneReviews stores non-semantic acknowledgements keyed by artifact ID.
// Writing here does not bump artifact updated_at, avoiding stale_reference cascades.
var (
	hygieneReviewMu sync.RWMutex
	hygieneReviews  = map[string]time.Time{}
)

// AcknowledgeHygiene records a non-semantic review timestamp for ids.
func AcknowledgeHygiene(ids []string, at time.Time) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	hygieneReviewMu.Lock()
	defer hygieneReviewMu.Unlock()
	for _, id := range ids {
		if id != "" {
			hygieneReviews[id] = at
		}
	}
}

func hygieneReviewedAt(id string) (time.Time, bool) {
	hygieneReviewMu.RLock()
	defer hygieneReviewMu.RUnlock()
	t, ok := hygieneReviews[id]
	return t, ok
}

// reviewedOK returns true when a non-semantic review covers all stale neighbors.
func reviewedOK(art *parchment.Artifact, stale []StaleNeighbor) bool {
	if art == nil || len(stale) == 0 {
		return false
	}
	reviewed, ok := hygieneReviewedAt(art.ID)
	if !ok && art.Extra != nil {
		if ts, ok2 := art.Extra["hygiene_reviewed_at"].(string); ok2 && ts != "" {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				reviewed = t
				ok = true
			}
		}
	}
	if !ok {
		return false
	}
	for _, n := range stale {
		if n.UpdatedAt.After(reviewed) {
			return false
		}
	}
	return true
}

type hygieneInputExt struct {
	hygieneInput
	Prune          bool     `json:"prune,omitempty"`
	BaselineIDs    []string `json:"baseline_ids,omitempty"`
	AcknowledgeIDs []string `json:"acknowledge_ids,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Cursor         int      `json:"cursor,omitempty"`
}

func runHygieneStructured(ctx context.Context, svc *Service, raw json.RawMessage) (Result, error) {
	var in hygieneInputExt
	_ = json.Unmarshal(raw, &in)

	if len(in.AcknowledgeIDs) > 0 {
		AcknowledgeHygiene(in.AcknowledgeIDs, time.Now().UTC())
		data := map[string]any{
			"action": "acknowledge", "status": "ok",
			"ids": in.AcknowledgeIDs, "count": len(in.AcknowledgeIDs),
		}
		return Result{
			Text: fmt.Sprintf("acknowledged %d artifact(s) without bumping updated_at", len(in.AcknowledgeIDs)),
			Data: data,
		}, nil
	}

	findings := collectFindings(ctx, svc, in.Scope, in.IncludeCode)

	if len(in.BaselineIDs) > 0 {
		base := map[string]bool{}
		for _, id := range in.BaselineIDs {
			base[id] = true
		}
		var delta []HygieneFinding
		for _, f := range findings {
			if !base[f.ID] {
				delta = append(delta, f)
			}
		}
		findings = delta
	}

	revisionsPruned := 0
	if in.Prune {
		var pruneLabels []string
		if in.Scope != "" {
			pruneLabels = append(pruneLabels, parchment.LabelPrefixScope+in.Scope)
		}
		pruneArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{Labels: pruneLabels})
		for _, art := range pruneArts {
			n, _ := svc.Proto.PruneRevisions(ctx, art.ID, 20) //nolint:mnd // max revisions to keep
			revisionsPruned += n
		}
		if c, ok := svc.Proto.Store().(parchment.Compactor); ok && revisionsPruned > 0 {
			_ = c.IncrementalVacuum(ctx)
		}
	}

	if in.Severity != "" {
		var filtered []HygieneFinding
		for _, f := range findings {
			if f.Severity == in.Severity {
				filtered = append(filtered, f)
			}
		}
		findings = filtered
	}

	total := len(findings)
	if in.Cursor > 0 {
		if in.Cursor >= len(findings) {
			findings = nil
		} else {
			findings = findings[in.Cursor:]
		}
	}
	nextCursor := 0
	if in.Limit > 0 && len(findings) > in.Limit {
		nextCursor = in.Cursor + in.Limit
		findings = findings[:in.Limit]
	}

	out := HygieneOutput{
		Total: total, Summary: map[string]int{}, Findings: findings, Pruned: revisionsPruned,
	}
	for _, f := range findings {
		out.Summary[f.Impact]++
	}
	data := map[string]any{
		"action": "hygiene", "status": "ok",
		"total": out.Total, "summary": out.Summary, "findings": out.Findings,
		"pruned": out.Pruned, "next_cursor": nextCursor, "read_only": !in.Prune,
	}

	if in.Format == "full" {
		b, _ := json.Marshal(out)
		return Result{Text: string(b), Data: data}, nil
	}
	return Result{Text: formatHygieneText(in.Scope, findings, revisionsPruned), Data: data}, nil
}

func formatHygieneText(scope string, findings []HygieneFinding, revisionsPruned int) string {
	if len(findings) == 0 {
		if scope == "" {
			scope = "all scopes"
		}
		msg := fmt.Sprintf("hygiene: %s is clean — no issues found", scope)
		if revisionsPruned > 0 {
			msg += fmt.Sprintf(" (pruned %d old revisions)", revisionsPruned)
		}
		return msg
	}
	severityGroups := map[string][]HygieneFinding{}
	for _, f := range findings {
		severityGroups[f.Severity] = append(severityGroups[f.Severity], f)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "hygiene: %d issues found", len(findings))
	for sev, items := range severityGroups {
		fmt.Fprintf(&b, " | %s:%d", sev, len(items))
	}
	b.WriteString("\n")
	if revisionsPruned > 0 {
		fmt.Fprintf(&b, "(pruned %d old revisions)\n", revisionsPruned)
	}
	for _, sev := range []string{"critical", "planning", "content", "index"} {
		items := severityGroups[sev]
		if len(items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s (%d)\n", strings.ToUpper(sev), len(items))
		for _, f := range items {
			fmt.Fprintf(&b, "  [%s] %s  %s\n", f.Category, f.ID, f.Title)
			fmt.Fprintf(&b, "    %s\n", f.Detail)
			if f.Fix != "" {
				fmt.Fprintf(&b, "    fix: %s\n", f.Fix)
			}
		}
	}
	return b.String()
}
