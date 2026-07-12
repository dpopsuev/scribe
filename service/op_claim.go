package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	parchment "github.com/dpopsuev/parchment"
)

func init() {
	Registry = append(Registry, opClaim, opRelease, opHandoff)
}

const (
	defaultClaimTTL = time.Hour
	sectionEvidence = "evidence"
)

type claimInput struct {
	ID          string   `json:"id,omitempty"`
	Agent       string   `json:"agent,omitempty"`
	Session     string   `json:"session,omitempty"`
	TTLSeconds  int      `json:"ttl_seconds,omitempty"`
	Force       bool     `json:"force,omitempty"`
	FromSession string   `json:"from_session,omitempty"`
	ToSession   string   `json:"to_session,omitempty"`
	ArtifactID  string   `json:"artifact_id,omitempty"`
	Evidence    []string `json:"evidence,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	ToAgent     string   `json:"to_agent,omitempty"`
}

var opClaim = Op{
	Name: "claim",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in claimInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" || in.Agent == "" {
			return "", fmt.Errorf("claim requires id= and agent=") //nolint:err113 // agent-facing
		}
		ttl := defaultClaimTTL
		if in.TTLSeconds > 0 {
			ttl = time.Duration(in.TTLSeconds) * time.Second
		}
		session := in.Session
		if session == "" {
			session = svc.SessionID
		}
		if err := applyClaim(ctx, svc, in.ID, in.Agent, session, ttl, in.Force); err != nil {
			return "", err
		}
		return fmt.Sprintf("claimed %s by %s (ttl=%s)", in.ID, in.Agent, ttl), nil
	},
}

var opRelease = Op{
	Name: "release",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in claimInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		if in.ID == "" || in.Agent == "" {
			return "", fmt.Errorf("release requires id= and agent=") //nolint:err113 // agent-facing
		}
		if err := releaseClaim(ctx, svc, in.ID, in.Agent, in.Force); err != nil {
			return "", err
		}
		return fmt.Sprintf("released %s by %s", in.ID, in.Agent), nil
	},
}

var opHandoff = Op{
	Name: "handoff",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in claimInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}
		targetID := in.ArtifactID
		if targetID == "" {
			targetID = in.ID
		}
		if targetID == "" || in.FromSession == "" || in.ToSession == "" {
			return "", fmt.Errorf("handoff requires artifact_id= (or id=), from_session=, to_session=") //nolint:err113 // agent-facing
		}

		wsRaw, _ := runWorkingSet(ctx, svc, &listInput{Mode: modeWorkingSet, Scope: "", Limit: 10})
		summary := in.Summary
		if summary == "" {
			summary = "agent handoff"
		}
		sections := []parchment.Section{
			{Name: "from_session", Text: in.FromSession},
			{Name: "to_session", Text: in.ToSession},
			{Name: "summary", Text: summary},
			{Name: "working_set", Text: wsRaw},
		}
		if len(in.Evidence) > 0 {
			sections = append(sections, parchment.Section{Name: sectionEvidence, Text: strings.Join(in.Evidence, "\n")})
		}
		note, err := svc.Proto.CreateArtifact(ctx, parchment.CreateInput{
			Labels:   []string{kindLabelKnowledge, "note.fleeting", "area:handoff"},
			Title:    fmt.Sprintf("Handoff %s → %s", in.FromSession, in.ToSession),
			Sections: sections,
		})
		if err != nil {
			return "", err
		}
		_, _ = svc.Proto.LinkArtifacts(ctx, note.ID, "related", []string{targetID}, 0)
		for _, ev := range in.Evidence {
			_, _ = svc.Proto.LinkArtifacts(ctx, note.ID, "related", []string{ev}, 0)
		}

		if in.Agent != "" {
			_ = releaseClaim(ctx, svc, targetID, in.Agent, true)
		}
		if in.ToAgent != "" {
			_ = applyClaim(ctx, svc, targetID, in.ToAgent, in.ToSession, defaultClaimTTL, true)
		}

		return fmt.Sprintf("handoff note %s for %s", note.ID, targetID), nil
	},
}

func applyClaim(ctx context.Context, svc *Service, id, agent, session string, ttl time.Duration, force bool) error {
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if existing, ok := parchment.ClaimFromExtra(art.Extra); ok && parchment.ClaimActive(existing, now) && existing.Agent != agent && !force {
		return fmt.Errorf("artifact %s claimed by %s until %s", id, existing.Agent, existing.ExpiresAt.Format(time.RFC3339)) //nolint:err113 // agent-facing
	}
	claim := parchment.Claim{Agent: agent, Session: session, ExpiresAt: now.Add(ttl)}
	extra := parchment.ApplyClaim(art.Extra, claim)
	return svc.Proto.PatchArtifact(ctx, id, parchment.ArtifactPatch{SetExtra: extra})
}

func releaseClaim(ctx context.Context, svc *Service, id, agent string, force bool) error {
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return err
	}
	existing, ok := parchment.ClaimFromExtra(art.Extra)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	if parchment.ClaimActive(existing, now) && existing.Agent != agent && !force {
		return fmt.Errorf("artifact %s claimed by %s", id, existing.Agent) //nolint:err113 // agent-facing
	}
	art.Extra = parchment.ClearClaim(art.Extra)
	return svc.Proto.UpdateArtifact(ctx, art, art.UpdatedAt)
}

func checkClaimGuard(ctx context.Context, svc *Service, id, agent string, force, bypass bool) error {
	if force || bypass {
		return nil
	}
	art, err := svc.Proto.GetArtifact(ctx, id)
	if err != nil {
		return err
	}
	claim, ok := parchment.ClaimFromExtra(art.Extra)
	if !ok || !parchment.ClaimActive(claim, time.Now().UTC()) {
		return nil
	}
	if agent != "" && claim.Agent == agent {
		return nil
	}
	return fmt.Errorf("artifact %s is claimed by %s until %s (pass force=true to override)", id, claim.Agent, claim.ExpiresAt.Format(time.RFC3339)) //nolint:err113 // agent-facing
}
