//nolint:goconst // dashboard renders status strings inline; constants obscure the logic
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type dashboardInput struct {
	Scope  string `json:"scope,omitempty"`
	Format string `json:"format,omitempty"`
}

type campStats struct {
	title                              string
	scope, status                      string
	goalsActive, goalsDraft, goalsDone int
	tasksActive, tasksDone             int
	score                              float64
	content, delivery, verified        float64
}

var opDashboard = Op{
	Name:       "dashboard",
	Structured: runDashboardStructured,
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		r, err := runDashboardStructured(ctx, svc, raw)
		return r.Text, err
	},
}

func runDashboardStructured(ctx context.Context, svc *Service, raw json.RawMessage) (Result, error) {
	var in dashboardInput
	_ = json.Unmarshal(raw, &in)

	campaignLabels := []string{labelCampaign}
	if in.Scope != "" {
		campaignLabels = append(campaignLabels, parchment.LabelPrefixScope+in.Scope)
	}
	campaigns, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
		Labels: campaignLabels,
	})

	stats := make([]campStats, 0, len(campaigns))
	for _, c := range campaigns {
		cs := collectCampStats(ctx, svc, c)
		stats = append(stats, cs)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%-10s %-8s %-40s %4s %4s %4s %4s %4s %5s %5s %5s\n",
		"SCOPE", "STATUS", "CAMPAIGN", "G.AC", "G.DR", "G.DN", "T.AC", "T.DN", "CONT", "DELV", "VERF")
	fmt.Fprintf(&b, "%-10s %-8s %-40s %4s %4s %4s %4s %4s %5s %5s %5s\n",
		"-----", "------", "--------", "----", "----", "----", "----", "----", "----", "----", "----")
	rows := make([]map[string]any, 0, len(stats))
	for _, cs := range stats {
		title := cs.title
		const titleColWidth = 40
		if len(title) > titleColWidth {
			title = title[:titleColWidth-3] + "..."
		}
		fmt.Fprintf(&b, "%-10s %-8s %-40s %4d %4d %4d %4d %4d %4.0f%% %4.0f%% %4.0f%%\n",
			cs.scope, cs.status, title,
			cs.goalsActive, cs.goalsDraft, cs.goalsDone,
			cs.tasksActive, cs.tasksDone,
			cs.content*100, cs.delivery*100, cs.verified*100)
		rows = append(rows, map[string]any{
			"scope": cs.scope, "status": cs.status, "title": cs.title,
			"content_completeness": cs.content, "delivery_progress": cs.delivery, "verified_progress": cs.verified,
			"goals_active": cs.goalsActive, "goals_draft": cs.goalsDraft, "goals_done": cs.goalsDone,
			"tasks_active": cs.tasksActive, "tasks_done": cs.tasksDone,
		})
	}
	fmt.Fprintf(&b, "\n(%d campaigns)\n", len(stats))
	return Result{Text: b.String(), Data: map[string]any{"campaigns": rows, "count": len(rows)}}, nil
}

func collectCampStats(ctx context.Context, svc *Service, c *parchment.Artifact) campStats {
	m := ComputeProgress(ctx, svc, c)
	cs := campStats{
		title:    c.Title,
		scope:    c.Label(parchment.LabelPrefixScope),
		status:   parchment.StatusFromLabels(c.Labels),
		score:    m.DeliveryProgress,
		content:  m.ContentCompleteness,
		delivery: m.DeliveryProgress,
		verified: m.VerifiedProgress,
	}
	goalEdges, _ := svc.Proto.Neighbors(ctx, c.ID, parchment.RelParentOf, parchment.Outgoing)
	for _, e := range goalEdges {
		goal, _ := svc.Proto.GetArtifact(ctx, e.To)
		if goal == nil || goal.Label(parchment.LabelPrefixKind) != "effort.goal" {
			continue
		}
		tallyGoalStatus(svc, &cs, goal)
		tallyTaskStats(ctx, svc, &cs, goal)
	}
	return cs
}

func tallyGoalStatus(svc *Service, cs *campStats, goal *parchment.Artifact) {
	status := parchment.StatusFromLabels(goal.Labels)
	switch {
	case svc.Proto.IsTerminal(status):
		cs.goalsDone++
	case status == svc.Proto.ActiveStatus(goal.Label(parchment.LabelPrefixKind)):
		cs.goalsActive++
	default:
		cs.goalsDraft++
	}
}

func tallyTaskStats(ctx context.Context, svc *Service, cs *campStats, goal *parchment.Artifact) {
	taskEdges, _ := svc.Proto.Neighbors(ctx, goal.ID, parchment.RelParentOf, parchment.Outgoing)
	for _, te := range taskEdges {
		task, _ := svc.Proto.GetArtifact(ctx, te.To)
		if task == nil {
			continue
		}
		status := parchment.StatusFromLabels(task.Labels)
		if svc.Proto.IsTerminal(status) {
			cs.tasksDone++
		} else if status == svc.Proto.ActiveStatus(task.Label(parchment.LabelPrefixKind)) {
			cs.tasksActive++
		}
	}
}
