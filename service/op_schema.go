package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type schemaInput struct {
	ID   string `json:"id,omitempty"`
	Kind string `json:"kind,omitempty"`
}

var opSchema = Op{
	Name: "schema",
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		var in schemaInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", err
		}

		kind := in.Kind
		if kind == "" && in.ID != "" {
			art, err := svc.Proto.GetArtifact(ctx, in.ID)
			if err != nil {
				return "", err
			}
			kind = art.Label(parchment.LabelPrefixKind)
		}
		if kind == "" {
			return "", fmt.Errorf("kind or id required") //nolint:err113 // agent-facing
		}

		rels := svc.Proto.ValidRelationsFor(kind)
		if len(rels) == 0 {
			return fmt.Sprintf("no registered relations for kind %q", kind), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "schema for %s:\n\n", kind)

		defaultStatus, _, transitions := svc.Proto.KindLifecycle(kind)
		if len(transitions) > 0 {
			fmt.Fprintf(&b, "lifecycle:\n")
			fmt.Fprintf(&b, "  default: %s\n", defaultStatus)
			for _, t := range transitions {
				fmt.Fprintf(&b, "  %s\n", t)
			}
			b.WriteString("\n")
		}

		must := svc.Proto.MustSections(kind)
		should := svc.Proto.ShouldSections(kind)
		if len(must) > 0 || len(should) > 0 {
			fmt.Fprintf(&b, "sections:\n")
			if len(must) > 0 {
				fmt.Fprintf(&b, "  must:   %s\n", strings.Join(must, ", "))
			}
			if len(should) > 0 {
				fmt.Fprintf(&b, "  should: %s\n", strings.Join(should, ", "))
			}
			b.WriteString("\n")
		}

		fmt.Fprintf(&b, "relations:\n")
		fmt.Fprintf(&b, "  %-20s %s\n", "RELATION", "TARGET")
		fmt.Fprintf(&b, "  %-20s %s\n", "--------", "------")
		for _, r := range rels {
			fmt.Fprintf(&b, "  %-20s %s\n", r.Relation, r.Target)
		}
		return b.String(), nil
	},
}
