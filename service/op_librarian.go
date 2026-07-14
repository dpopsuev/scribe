package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opLibrarian)
}

const (
	edgeSourceLibrarian   = "librarian"
	librarianModeMerge    = "merge"
	librarianModeSplit    = "split"
	librarianModeLink     = "link"
	librarianModeUnlink   = "unlink"
	librarianModeStale    = "stale"
	librarianDefaultStale = "code.stale"
	librarianTitleMax     = 72
	labelRoleSplit        = "role:split"
)

type librarianInput struct {
	Mode   string `json:"mode"` // merge | split | link | unlink | stale
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	ID     string `json:"id,omitempty"`
	Rel    string `json:"relation,omitempty"`
	Text   string `json:"text,omitempty"` // split: body for new node
	Status string `json:"status,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

var opLibrarian = Op{
	Name: edgeSourceLibrarian,
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in librarianInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(in.Mode)) {
		case librarianModeMerge:
			return librarianMerge(ctx, svc, in)
		case librarianModeSplit:
			return librarianSplit(ctx, svc, in)
		case librarianModeLink:
			return librarianLink(ctx, svc, in)
		case librarianModeUnlink:
			return librarianUnlink(ctx, svc, in)
		case librarianModeStale:
			return librarianStale(ctx, svc, in)
		default:
			return "", fmt.Errorf("librarian mode must be merge|split|link|unlink|stale") //nolint:err113 // agent-facing
		}
	},
}

func librarianMerge(ctx context.Context, svc *Service, in librarianInput) (string, error) {
	if in.From == "" || in.To == "" {
		return "", fmt.Errorf("merge requires from= (loser) and to= (keeper)") //nolint:err113 // agent-facing
	}
	if in.From == in.To {
		return "", fmt.Errorf("merge from and to must differ") //nolint:err113 // agent-facing
	}
	store := svc.Proto.Store()
	if _, err := svc.Proto.GetArtifact(ctx, in.From); err != nil {
		return "", err
	}
	if _, err := svc.Proto.GetArtifact(ctx, in.To); err != nil {
		return "", err
	}

	inEdges, _ := store.Neighbors(ctx, in.From, "", parchment.Incoming)
	outEdges, _ := store.Neighbors(ctx, in.From, "", parchment.Outgoing)
	rewired := 0
	for _, e := range inEdges {
		if e.From == in.To {
			continue
		}
		if err := store.AddEdgeSource(ctx, e.From, e.Relation, in.To, edgeSourceLibrarian); err == nil {
			rewired++
		}
	}
	for _, e := range outEdges {
		if e.To == in.To {
			continue
		}
		if err := store.AddEdgeSource(ctx, in.To, e.Relation, e.To, edgeSourceLibrarian); err == nil {
			rewired++
		}
	}
	_ = store.AddEdgeSource(ctx, in.To, parchment.RelSupersedes, in.From, edgeSourceLibrarian)
	if _, err := svc.Proto.SetField(ctx, []string{in.From}, "status", "archived", parchment.SetFieldOptions{Force: true}); err != nil {
		return "", fmt.Errorf("rewired %d edges but archive %s: %w", rewired, in.From, err)
	}
	return fmt.Sprintf("merged %s into %s (rewired %d edges, supersedes+archived)", in.From, in.To, rewired), nil
}

func librarianSplit(ctx context.Context, svc *Service, in librarianInput) (string, error) {
	if in.ID == "" || strings.TrimSpace(in.Text) == "" {
		return "", fmt.Errorf("split requires id= and text= (body for new node)") //nolint:err113 // agent-facing
	}
	parent, err := svc.Proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return "", err
	}
	scope := parent.Label(parchment.LabelPrefixScope)
	labels := []string{kindLabelKnowledge, labelRoleSplit}
	if scope != "" {
		labels = append(labels, parchment.LabelPrefixScope+scope)
	}
	child, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
		Title:    truncateTitle(in.Text, librarianTitleMax),
		Labels:   labels,
		Sections: []parchment.Section{{Name: sectionKeyBody, Text: in.Text}},
	})
	if err != nil {
		return "", err
	}
	_ = svc.Proto.Store().AddEdgeSource(ctx, in.ID, parchment.RelRelatesTo, child.ID, edgeSourceLibrarian)
	return fmt.Sprintf("split %s → %s", in.ID, child.ID), nil
}

func librarianLink(ctx context.Context, svc *Service, in librarianInput) (string, error) {
	if in.From == "" || in.To == "" || in.Rel == "" {
		return "", fmt.Errorf("link requires from=, to=, relation=") //nolint:err113 // agent-facing
	}
	if err := svc.Proto.Store().AddEdgeSource(ctx, in.From, in.Rel, in.To, edgeSourceLibrarian); err != nil {
		return "", err
	}
	return fmt.Sprintf("linked %s -[%s]-> %s (source=librarian)", in.From, in.Rel, in.To), nil
}

func librarianUnlink(ctx context.Context, svc *Service, in librarianInput) (string, error) {
	if in.From == "" || in.To == "" || in.Rel == "" {
		return "", fmt.Errorf("unlink requires from=, to=, relation=") //nolint:err113 // agent-facing
	}
	if err := svc.Proto.Store().RemoveEdgeSource(ctx, in.From, in.Rel, in.To, edgeSourceLibrarian); err != nil {
		if err2 := svc.Proto.Store().RemoveEdge(ctx, parchment.Edge{From: in.From, Relation: in.Rel, To: in.To}); err2 != nil {
			return "", fmt.Errorf("unlink: %w", err)
		}
	}
	return fmt.Sprintf("unlinked %s -[%s]-> %s", in.From, in.Rel, in.To), nil
}

func librarianStale(ctx context.Context, svc *Service, in librarianInput) (string, error) {
	id := in.ID
	if id == "" {
		id = in.From
	}
	if id == "" {
		return "", fmt.Errorf("stale requires id=") //nolint:err113 // agent-facing
	}
	status := in.Status
	if status == "" {
		status = librarianDefaultStale
	}
	if _, err := svc.Proto.SetField(ctx, []string{id}, "status", status, parchment.SetFieldOptions{Force: true}); err != nil {
		return "", err
	}
	return fmt.Sprintf("marked %s status=%s (source=librarian)", id, status), nil
}
