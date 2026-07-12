package service

import (
	"context"
	"encoding/json"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

type workingSetItem struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Kind     string `json:"kind,omitempty"`
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
	Excerpt  string `json:"excerpt,omitempty"`
}

type workingSetRepair struct {
	SafeCount int    `json:"safe_count"`
	Hint      string `json:"hint,omitempty"`
}

type workingSetOutput struct {
	Session    map[string]any          `json:"session,omitempty"`
	Campaigns  []triageCampaignSummary `json:"campaigns"`
	Ready      []workingSetItem        `json:"ready"`
	Recent     []workingSetItem        `json:"recent"`
	HygieneTop []HygieneFinding        `json:"hygiene_top"`
	Repair     *workingSetRepair       `json:"repair,omitempty"`
}

func runWorkingSet(ctx context.Context, svc *Service, in *listInput) (string, error) {
	labels := append([]string(nil), in.Labels...)
	if in.Scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+in.Scope)
	}

	active, _ := summarizeCampaigns(ctx, svc, labels)
	out := workingSetOutput{
		Session:    buildWorkingSetSession(svc, in),
		Campaigns:  active,
		Ready:      collectWorkingSetReady(ctx, svc, active, in.Depth),
		Recent:     nil,
		HygieneTop: nil,
	}

	recentLimit := in.Limit
	if recentLimit <= 0 {
		recentLimit = 20
	}
	recentCutoff := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	recentArts, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels:       labels,
		UpdatedAfter: recentCutoff,
		Limit:        recentLimit,
	})
	out.Recent = appendRecent(nil, recentArts, in.ExcerptChars)
	out.HygieneTop, out.Repair = buildWorkingSetHygiene(ctx, svc, in)

	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func collectWorkingSetReady(ctx context.Context, svc *Service, active []triageCampaignSummary, depth int) []workingSetItem {
	readyLimit := depth
	if readyLimit <= 0 {
		readyLimit = 5
	}
	seenReady := map[string]bool{}
	var ready []workingSetItem
	for _, camp := range active {
		entries, err := svc.Proto.TopoSort(ctx, camp.ID)
		if err != nil && len(entries) == 0 {
			continue
		}
		entries = filterUnblocked(ctx, svc, entries, readyLimit)
		entries = filterLeaves(ctx, svc, entries)
		for _, e := range entries {
			if seenReady[e.ID] {
				continue
			}
			seenReady[e.ID] = true
			ready = append(ready, workingSetItem{
				ID:       e.ID,
				Title:    e.Title,
				Kind:     parchment.LabelValue(e.Labels, parchment.LabelPrefixKind),
				Status:   parchment.StatusFromLabels(e.Labels),
				Priority: e.Priority,
			})
		}
	}
	return ready
}

func appendRecent(dst []workingSetItem, arts []*parchment.Artifact, excerptChars int) []workingSetItem {
	for _, art := range arts {
		item := workingSetItem{
			ID:       art.ID,
			Title:    art.Title,
			Kind:     art.Label(parchment.LabelPrefixKind),
			Status:   parchment.StatusFromLabels(art.Labels),
			Priority: art.Label(parchment.LabelPrefixPriority),
		}
		if excerptChars > 0 {
			item.Excerpt = excerptArtifact(art, excerptChars)
		}
		dst = append(dst, item)
	}
	return dst
}

func buildWorkingSetHygiene(ctx context.Context, svc *Service, in *listInput) ([]HygieneFinding, *workingSetRepair) {
	findings := collectFindings(ctx, svc, in.Scope, in.IncludeCode)
	const hygieneCap = 10
	safeCount := 0
	var top []HygieneFinding
	for i := range findings {
		f := &findings[i]
		if f.SafeAutofix && f.SuggestedFix != nil {
			safeCount++
		}
		if !in.IncludeCode && f.Severity == severityIndex {
			continue
		}
		if f.Severity != severityCritical && f.Severity != severityPlanning {
			continue
		}
		top = append(top, *f)
		if len(top) >= hygieneCap {
			break
		}
	}
	scopeHint := in.Scope
	if scopeHint == "" {
		scopeHint = "<scope>"
	}
	return top, &workingSetRepair{
		SafeCount: safeCount,
		Hint:      "admin(action=auto_repair, scope=" + scopeHint + ") — dry_run=true first",
	}
}

func buildWorkingSetSession(svc *Service, in *listInput) map[string]any {
	session := map[string]any{}
	if svc.SessionID != "" {
		session["session_id"] = svc.SessionID
	}
	if len(svc.HomeScopes) > 0 {
		session["home_scopes"] = svc.HomeScopes
	}
	if len(svc.WorkspaceLabels) > 0 {
		session["workspace_labels"] = svc.WorkspaceLabels
	}
	if in.Scope != "" {
		session["scope"] = in.Scope
	}
	if in.Session != "" {
		session["session"] = in.Session
	}
	if len(session) == 0 {
		return nil
	}
	return session
}

func excerptArtifact(art *parchment.Artifact, n int) string {
	if art == nil || n <= 0 {
		return ""
	}
	for _, sec := range art.Sections {
		text := sec.Text
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > n {
			return string(runes[:n])
		}
		return text
	}
	return ""
}
