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

var opDashboard = Op{
	Name: "dashboard",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in dashboardInput
		_ = json.Unmarshal(raw, &in)

		campaignLabels := []string{labelCampaign}
		if in.Scope != "" {
			campaignLabels = append(campaignLabels, parchment.LabelPrefixScope+in.Scope)
		}
		campaigns, _ := svc.Proto.ListArtifacts(ctx, parchment.ListInput{
			Labels: campaignLabels,
		})

		type campStats struct {
			title                              string
			scope, status                      string
			goalsActive, goalsDraft, goalsDone int
			tasksActive, tasksDone             int
			score                              float64
		}

		stats := make([]campStats, 0, len(campaigns))
		for _, c := range campaigns {
			cs := campStats{
				title:  c.Title,
				scope:  c.Label(parchment.LabelPrefixScope),
				status: parchment.StatusFromLabels(c.Labels),
				score:  svc.Proto.CompletionScore(ctx, c),
			}
			goalEdges, _ := svc.Proto.Store().Neighbors(ctx, c.ID, parchment.RelParentOf, parchment.Outgoing)
			for _, e := range goalEdges {
				goal, _ := svc.Proto.GetArtifact(ctx, e.To)
				if goal == nil || goal.Label(parchment.LabelPrefixKind) != "effort.goal" {
					continue
				}
				switch gs := parchment.StatusFromLabels(goal.Labels); gs {
				case "work.active":
					cs.goalsActive++
				case "work.draft":
					cs.goalsDraft++
				case "work.complete", "done", "complete":
					cs.goalsDone++
				}
				taskEdges, _ := svc.Proto.Store().Neighbors(ctx, goal.ID, parchment.RelParentOf, parchment.Outgoing)
				for _, te := range taskEdges {
					task, _ := svc.Proto.GetArtifact(ctx, te.To)
					if task == nil || task.Label(parchment.LabelPrefixKind) != "effort.task" {
						continue
					}
					switch ts := parchment.StatusFromLabels(task.Labels); ts {
					case "work.active":
						cs.tasksActive++
					case "work.complete", "done", "complete":
						cs.tasksDone++
					}
				}
			}
			stats = append(stats, cs)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "%-10s %-8s %-50s %4s %4s %4s %4s %4s %5s\n",
			"SCOPE", "STATUS", "CAMPAIGN", "G.AC", "G.DR", "G.DN", "T.AC", "T.DN", "SCORE")
		fmt.Fprintf(&b, "%-10s %-8s %-50s %4s %4s %4s %4s %4s %5s\n",
			"-----", "------", "--------", "----", "----", "----", "----", "----", "-----")
		for _, cs := range stats {
			title := cs.title
			if len(title) > 50 {
				title = title[:47] + "..."
			}
			fmt.Fprintf(&b, "%-10s %-8s %-50s %4d %4d %4d %4d %4d %4.0f%%\n",
				cs.scope, cs.status, title,
				cs.goalsActive, cs.goalsDraft, cs.goalsDone,
				cs.tasksActive, cs.tasksDone,
				cs.score*100)
		}
		fmt.Fprintf(&b, "\n(%d campaigns)\n", len(stats))
		return b.String(), nil
	},
}
