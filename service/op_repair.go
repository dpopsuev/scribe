package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opFoldCampaign, opReparentChildren, opRepairLifecycle)
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

const kindEffort = "effort"

func lifecycleFix(proto *parchment.Protocol, status string) (string, bool) {
	if proto.IsTerminal(status) {
		return "work.complete", true
	}
	if parchment.IsDomainStatusLabel(status) && !strings.HasPrefix(status, "work.") {
		return labelStatusDraft, true
	}
	return "", false
}

type repairInput struct {
	ID    string `json:"id,omitempty"`
	Scope string `json:"scope,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

var opRepairLifecycle = Op{
	Name: "repair_lifecycle",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in repairInput
		_ = json.Unmarshal(raw, &in)

		var arts []*parchment.Artifact
		if in.ID != "" {
			art, err := svc.Proto.GetArtifact(ctx, in.ID)
			if err != nil {
				return "", err
			}
			arts = []*parchment.Artifact{art}
		} else {
			var labels []string
			if in.Scope != "" {
				labels = append(labels, parchment.LabelPrefixScope+in.Scope)
			}
			li := parchment.ListInput{Labels: labels, KindPrefix: kindEffort}
			if in.Kind != "" {
				li.Labels = append(li.Labels, parchment.LabelPrefixKind+in.Kind)
				li.KindPrefix = ""
			}
			arts, _ = svc.Proto.ListArtifacts(ctx, li)
		}

		repaired := 0
		var details []string
		for _, art := range arts {
			kind := art.Label(parchment.LabelPrefixKind)
			if !strings.HasPrefix(kind, kindEffort+".") {
				continue
			}
			status := parchment.StatusFromLabels(art.Labels)
			fix, needsFix := lifecycleFix(svc.Proto, status)
			if !needsFix || strings.HasPrefix(status, "work.") {
				continue
			}
			results, err := svc.Proto.SetField(ctx, []string{art.ID}, "status", fix, parchment.SetFieldOptions{Force: true})
			if err != nil || !results[0].OK {
				errMsg := ""
				if err != nil {
					errMsg = err.Error()
				} else {
					errMsg = results[0].Error
				}
				details = append(details, fmt.Sprintf("  %s: %s → %s FAILED: %s", art.ID, status, fix, errMsg))
				continue
			}
			details = append(details, fmt.Sprintf("  %s: %s → %s", art.ID, status, fix))
			repaired++
		}

		if repaired == 0 && len(details) == 0 {
			return "repair_lifecycle: no malformed artifacts found", nil
		}

		return fmt.Sprintf("repair_lifecycle: %d repaired\n%s", repaired, strings.Join(details, "\n")), nil
	},
}
