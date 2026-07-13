//nolint:goconst // action field schema keys are intentional literals
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

type schemaInput struct {
	ID    string `json:"id,omitempty"`
	Kind  string `json:"kind,omitempty"`
	Name  string `json:"name,omitempty"`  // action name for field contracts
	Query string `json:"query,omitempty"` // alias for name
}

// actionFieldSchemas documents action-specific MCP field contracts (progressive disclosure).
var actionFieldSchemas = map[string]map[string]any{
	"create": {
		"required": []string{"action", "title"},
		"optional": []string{"kind", "scope", "goal", "parent", "status", "priority", "labels", "depends_on", "sections", "links", "extra", "artifacts", "dry_run", "mode", "mutation_id", "clone_from"},
		"notes":    "Batch: artifacts[{...}] with $N parent refs. mode=plan|apply or dry_run=true for preview.",
	},
	"set": {
		"required": []string{"action", "field", "value"},
		"optional": []string{"id", "ids", "scope", "kind", "status", "force", "bypass_guards", "cascade", "dry_run", "rename_id"},
		"notes":    "Provide id/ids or a filter (scope/kind/status).",
	},
	"get": {
		"required": []string{"action"},
		"optional": []string{"id", "ids", "format", "include_edges", "section_filter", "depth", "relation", "direction", "against", "name"},
		"notes":    "ids[] is get_many. format=context|summary|tree|briefing|impact.",
	},
	"link": {
		"required": []string{"action", "relation"},
		"optional": []string{"id", "targets", "target", "old_target", "edges", "mode", "weight"},
		"notes":    "governed_by rewrites to decision -justifies-> subject.",
	},
	"delete": {
		"required": []string{"action"},
		"optional": []string{"id", "ids", "kind", "scope", "status", "query", "labels", "dry_run", "force"},
	},
	"lint": {
		"required": []string{"action"},
		"optional": []string{"id", "ids", "scope"},
		"notes":    "ids[] lints each root and its parent_of descendants (lint_many).",
	},
	"query": {
		"required": []string{"action"},
		"optional": []string{"query", "kind", "scope", "status", "mode", "ranked", "sort", "limit", "cursor", "labels"},
	},
	"update": {
		"required": []string{"action", "id"},
		"optional": []string{"title", "goal", "status", "priority", "labels", "sections", "sections_delete", "extra", "patch"},
	},
	"schema": {
		"required": []string{"action"},
		"optional": []string{"kind", "id", "name"},
		"notes":    "name=<action> for field contracts; kind= for kind lifecycle/relations.",
	},
}

var opSchema = Op{
	Name:       "schema",
	Structured: runSchemaStructured,
	Run: func(ctx context.Context, svc *Service, raw json.RawMessage) (string, error) {
		r, err := runSchemaStructured(ctx, svc, raw)
		return r.Text, err
	},
}

func runSchemaStructured(ctx context.Context, svc *Service, raw json.RawMessage) (Result, error) {
	var in schemaInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return Result{}, err
	}

	targetAction := strings.TrimSpace(in.Name)
	if targetAction == "" {
		targetAction = strings.TrimSpace(in.Query)
	}
	if in.Kind == "" && in.ID == "" && targetAction != "" {
		fields, ok := actionFieldSchemas[targetAction]
		if !ok {
			var names []string
			for k := range actionFieldSchemas {
				names = append(names, k)
			}
			sort.Strings(names)
			return Result{}, fmt.Errorf("unknown action %q; known: %s", targetAction, strings.Join(names, ", ")) //nolint:err113 // agent-facing
		}
		payload := map[string]any{
			"action": targetAction,
			"fields": fields,
			"canonical_relation": map[string]any{
				"name":         RelGovernedBy,
				"stores_as":    parchment.RelJustifies,
				"direction":    "decision -justifies-> subject",
				"discover_via": "get format=context",
			},
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		return Result{Text: string(data), Data: payload}, nil
	}

	kind := in.Kind
	if kind == "" && in.ID != "" {
		art, err := svc.Proto.GetArtifact(ctx, in.ID)
		if err != nil {
			return Result{}, err
		}
		kind = art.Label(parchment.LabelPrefixKind)
	}
	if kind == "" {
		var names []string
		for k := range actionFieldSchemas {
			names = append(names, k)
		}
		sort.Strings(names)
		return Result{}, fmt.Errorf("kind or id required; or name=<action> for field contracts (%s)", strings.Join(names, ", ")) //nolint:err113 // agent-facing
	}

	rels := svc.Proto.ValidRelationsFor(kind)
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
	fmt.Fprintf(&b, "  %-20s %s\n", RelGovernedBy, "intent.decision (alias → stores as justifies)")
	b.WriteString("\nprogress metrics:\n")
	b.WriteString("  content_completeness  required sections filled\n")
	b.WriteString("  delivery_progress     lifecycle-weighted work leaves\n")
	b.WriteString("  verified_progress     terminal leaves with evidence\n")

	payload := map[string]any{
		"kind":               kind,
		"relations":          rels,
		"canonical_relation": RelGovernedBy,
		"must_sections":      must,
		"should_sections":    should,
	}
	return Result{Text: b.String(), Data: payload}, nil
}
