package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type synonymInput struct {
	Mode  string `json:"mode"`
	ID    string `json:"id,omitempty"`
	Alias string `json:"alias,omitempty"`
	Term  string `json:"term,omitempty"`
}

var opSynonym = Op{
	Name: "synonym",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in synonymInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}

		switch in.Mode {
		case "add":
			return runSynonymAdd(ctx, svc, &in)
		case "remove":
			return runSynonymRemove(ctx, svc, &in)
		case "list":
			return runSynonymList(ctx, svc, &in)
		case "resolve":
			return runSynonymResolve(ctx, svc, &in)
		default:
			return "", fmt.Errorf("unknown synonym mode %q; valid: add, remove, list, resolve", in.Mode) //nolint:err113 // user-facing input validation
		}
	},
}

func runSynonymAdd(ctx context.Context, svc *Service, in *synonymInput) (string, error) {
	if in.ID == "" || in.Alias == "" {
		return "", fmt.Errorf("synonym add requires id and alias") //nolint:err113 // user-facing input validation
	}
	art, err := svc.Proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return "", err
	}
	if err := svc.Proto.AddAlias(ctx, art.ID, in.Alias); err != nil {
		return "", err
	}
	return fmt.Sprintf("alias %q added for %s (%s)", in.Alias, art.ID, art.Title), nil
}

func runSynonymRemove(ctx context.Context, svc *Service, in *synonymInput) (string, error) {
	if in.ID == "" || in.Alias == "" {
		return "", fmt.Errorf("synonym remove requires id and alias") //nolint:err113 // user-facing input validation
	}
	art, err := svc.Proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return "", err
	}
	if err := svc.Proto.RemoveAlias(ctx, art.ID, in.Alias); err != nil {
		return "", err
	}
	return fmt.Sprintf("alias %q removed from %s", in.Alias, art.ID), nil
}

func runSynonymList(ctx context.Context, svc *Service, in *synonymInput) (string, error) {
	if in.ID == "" {
		return "", fmt.Errorf("synonym list requires id") //nolint:err113 // user-facing input validation
	}
	art, err := svc.Proto.GetArtifact(ctx, in.ID)
	if err != nil {
		return "", err
	}
	aliases, _ := svc.Proto.ListAliases(ctx, art.ID)
	var b strings.Builder
	fmt.Fprintf(&b, "aliases for %s (%s):\n", art.ID, art.Title)
	for _, a := range aliases {
		fmt.Fprintf(&b, "  %s\n", a)
	}
	if len(aliases) == 0 {
		fmt.Fprintf(&b, "  (none)\n")
	}
	return b.String(), nil
}

func runSynonymResolve(ctx context.Context, svc *Service, in *synonymInput) (string, error) {
	if in.Term == "" {
		return "", fmt.Errorf("synonym resolve requires term") //nolint:err113 // user-facing input validation
	}
	art, err := svc.Proto.GetArtifact(ctx, in.Term)
	if err != nil {
		return fmt.Sprintf("no artifact found for term %q", in.Term), nil
	}
	return fmt.Sprintf("%s  %s  [%s]", art.ID, art.Title, art.Label(parchment.LabelPrefixKind)), nil
}
