package service

import (
	"context"
	"encoding/json"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opFoldCampaign, opReparentChildren)
}

type foldInput struct {
	From string `json:"from"`
	To   string `json:"to"`
}

var opFoldCampaign = Op{
	Name: "fold_campaign",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in foldInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.From == "" || in.To == "" {
			return "", fmt.Errorf("from and to campaign IDs required") //nolint:err113 // agent-facing
		}
		src, err := svc.Proto.GetArtifact(ctx, in.From)
		if err != nil {
			return "", fmt.Errorf("source campaign %q not found", in.From) //nolint:err113 // agent-facing
		}
		if _, err := svc.Proto.GetArtifact(ctx, in.To); err != nil {
			return "", fmt.Errorf("target campaign %q not found", in.To) //nolint:err113 // agent-facing
		}

		children, _ := svc.Proto.Neighbors(ctx, src.ID, parchment.RelParentOf, parchment.Outgoing)
		moved := 0
		for _, e := range children {
			child, _ := svc.Proto.GetArtifact(ctx, e.To)
			if child == nil {
				continue
			}
			status := parchment.StatusFromLabels(child.Labels)
			if svc.Proto.IsTerminal(status) {
				continue
			}
			_ = svc.Proto.RemoveEdge(ctx, e)
			_ = svc.Proto.AddEdge(ctx, parchment.Edge{From: in.To, To: e.To, Relation: parchment.RelParentOf})
			moved++
		}

		_, _ = svc.Proto.SetField(ctx, []string{in.From}, "status", "status:archived", parchment.SetFieldOptions{Force: true})

		return fmt.Sprintf("fold_campaign: moved %d non-terminal children from %s to %s, archived source", moved, in.From, in.To), nil
	},
}

type reparentInput struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status,omitempty"`
}

var opReparentChildren = Op{
	Name: "reparent_children",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in reparentInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.From == "" || in.To == "" {
			return "", fmt.Errorf("from and to IDs required") //nolint:err113 // agent-facing
		}
		if _, err := svc.Proto.GetArtifact(ctx, in.From); err != nil {
			return "", fmt.Errorf("source %q not found", in.From) //nolint:err113 // agent-facing
		}
		if _, err := svc.Proto.GetArtifact(ctx, in.To); err != nil {
			return "", fmt.Errorf("target %q not found", in.To) //nolint:err113 // agent-facing
		}

		children, _ := svc.Proto.Neighbors(ctx, in.From, parchment.RelParentOf, parchment.Outgoing)
		moved := 0
		for _, e := range children {
			child, _ := svc.Proto.GetArtifact(ctx, e.To)
			if child == nil {
				continue
			}
			status := parchment.StatusFromLabels(child.Labels)
			if in.Status != "" && status != in.Status {
				continue
			}
			if svc.Proto.IsTerminal(status) {
				continue
			}
			_ = svc.Proto.RemoveEdge(ctx, e)
			_ = svc.Proto.AddEdge(ctx, parchment.Edge{From: in.To, To: e.To, Relation: parchment.RelParentOf})
			moved++
		}

		return fmt.Sprintf("reparent_children: moved %d children from %s to %s", moved, in.From, in.To), nil
	},
}
