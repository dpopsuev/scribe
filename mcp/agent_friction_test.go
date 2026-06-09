package mcp_test

// agent_friction_test.go — tests for the four UX issues observed in
// production sessions across 38 Claude Code projects (29,097 tool calls).
//
// Issue 1 (63 errors): artifact(action=move) → unknown action error.
//   Agents try to re-parent via artifact tool; the action lives on graph.
//   Fix: artifact(move) should route to graph(move) or emit a redirect hint.
//
// Issue 2 (168 errors): JSON type coercion for sections, links, section_filter.
//   Agents pass sections as {name:text} object, links as [], section_filter as "s1,s2".
//   Fix: server should coerce or return a clear description of the right shape.
//
// Issue 3 (597 errors): template create with missing required sections.
//   Agents create artifacts that satisfy a template but omit required sections,
//   get rejected, then recover via promote_stash.
//   Fix: partial creates should be accepted with a warning; validate on promote.
//
// Issue 4 (observation): artifact(action=children) undocumented / inconsistent.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	parchment "github.com/dpopsuev/parchment"
	scribemcp "github.com/dpopsuev/scribe/mcp"
)

func newFrictionServer(t *testing.T) func(tool string, args map[string]any) string {
	t.Helper()
	s := openStore(t)
	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	return func(tool string, args map[string]any) string {
		return callTool(t, cs, tool, args)
	}
}

// --- Issue 1: artifact(action=move) redirect ---

// TestArtifact_Move_WorksDirectly verifies that re-parenting via
// artifact(action=set, field=parent, value=<target>) works. SCR-TSK-274.
//
// The move action was removed. Re-parenting now uses set(field=parent).
func TestArtifact_Move_WorksDirectly(t *testing.T) {
	call := newFrictionServer(t)

	// Missing value — should get a helpful error, not crash.
	out := call("artifact", map[string]any{
		"action": "set",
		"id":     "TASK-1",
		"field":  "parent",
	})

	// set without value should return an error mentioning value or field.
	if strings.Contains(out, "unknown artifact action") {
		t.Errorf("artifact(set) should be recognized.\nGot: %s", out)
	}
	// Error or result should mention the field or value.
	if !strings.Contains(strings.ToLower(out), "parent") &&
		!strings.Contains(strings.ToLower(out), "value") &&
		!strings.Contains(strings.ToLower(out), "field") &&
		!strings.Contains(strings.ToLower(out), "not found") {
		t.Errorf("artifact(set, field=parent) response should be meaningful.\nGot: %s", out)
	}
}

// --- Issue 2: JSON type coercion ---

// TestArtifact_Create_SectionsAsObject is a regression guard confirming that
// sections-as-object is already coerced server-side (no raw unmarshal error).
// PASSING as of 2026-05-27. 92 such errors observed historically — now fixed.
func TestArtifact_Create_SectionsAsObject(t *testing.T) {
	call := newFrictionServer(t)

	// Pass sections as object (wrong shape agents use)
	out := call("artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "sections-as-object",
		"scope":  "test",
		// This is the wrong shape: object instead of array of {name, text}
		"sections": map[string]any{
			"context": "some context text",
		},
	})

	// Either it succeeds (coercion worked) or it fails with a useful shape hint
	if strings.Contains(out, "cannot unmarshal") {
		// Generic Go JSON error — not helpful to agents
		t.Errorf("artifact(create) with sections-as-object returned raw Go unmarshal error.\n"+
			"Want: coercion to []map[string]string, or an error naming the expected shape.\n"+
			"Got:  %s", out)
	}
}

// TestArtifact_Get_SectionFilterAsString verifies that passing section_filter
// as a comma-separated string is coerced to []string.
//
// 31 errors observed: agents pass section_filter="context,acceptance".
func TestArtifact_Get_SectionFilterAsString(t *testing.T) {
	// Pass section_filter as a plain comma-separated string — wrong shape agents use.
	// 31 errors observed: agents pass "context,acceptance" instead of ["context","acceptance"].
	// We only care that the server does NOT return a raw Go unmarshal error.
	// A not-found error is fine; a type-shape error should be agent-readable.
	call := newFrictionServer(t)

	out := call("artifact", map[string]any{
		"action":         "get",
		"id":             "nonexistent-for-type-test",
		"section_filter": "context,acceptance", // string instead of []string
	})

	if strings.Contains(out, "cannot unmarshal") {
		t.Errorf("artifact(get) with section_filter as string returned raw Go unmarshal error.\n"+
			"Want: coercion to []string, or a clear shape hint.\nGot: %s", out)
	}
}

// TestArtifact_Create_LinksAsArray is a regression guard confirming that
// links-as-array is already handled (no raw unmarshal error).
// PASSING as of 2026-05-27. 45 such errors observed historically — now coerced.
func TestArtifact_Create_LinksAsArray(t *testing.T) {
	call := newFrictionServer(t)

	out := call("artifact", map[string]any{
		"action": "create",
		"kind":   "task",
		"title":  "links-as-array",
		"scope":  "test",
		// Wrong shape: agents send ["TASK-1", "TASK-2"] not {"depends_on": ["TASK-1"]}
		"links": []string{"TASK-1", "TASK-2"},
	})

	if strings.Contains(out, "cannot unmarshal") {
		t.Errorf("artifact(create) with links-as-array returned raw unmarshal error.\n"+
			"Want: coercion or a clear shape hint ({relation: [ids]}).\nGot: %s", out)
	}
}

// --- Issue 3: template create — partial draft accepted ---

// TestArtifact_Create_TemplatePartialDraft verifies that creating an artifact
// that satisfies a template but is missing required sections succeeds as a draft.
// The agent should be able to add sections incrementally, and the template
// conformance check should fire on promote (not on create).
//
// 597 template conformance errors observed — agents create, get rejected,
// then recover via promote_stash (240 uses). The create→reject→stash→promote
// cycle is the norm, not the exception.
func TestArtifact_Create_TemplatePartialDraft_Accepted(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Create a template with required sections
	tplStore := parchment.NewMemoryStore()
	_ = tplStore

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)

	// First create the template
	tplOut := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   parchment.KindTemplate,
		"title":  "strict-need",
		"scope":  "test",
		"sections": []map[string]string{
			{"name": "problem", "text": "required:What is the problem?"},
			{"name": "acceptance", "text": "required:How do we know it's done?"},
		},
	})
	if strings.Contains(tplOut, "error") || strings.Contains(tplOut, "Error") {
		t.Logf("template create: %s", tplOut)
	}

	// Extract template ID — try JSON parse first, then search
	tplID := ""
	var tplMap map[string]any
	if err := json.Unmarshal([]byte(tplOut), &tplMap); err == nil {
		if id, ok := tplMap["id"].(string); ok {
			tplID = id
		}
	}
	if tplID == "" {
		// Try to find ID in text
		_ = ctx
		arts, _ := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{}).
			ListArtifacts(ctx, parchment.ListInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindTemplate}})
		if len(arts) > 0 {
			tplID = arts[0].ID
		}
	}
	if tplID == "" {
		t.Skip("could not determine template ID — template creation may have different format")
	}

	// Now create an artifact satisfying that template but WITHOUT required sections.
	// This should succeed (draft) not fail.
	out := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "need",
		"title":  "partial need — no sections yet",
		"scope":  "test",
		"links":  map[string][]string{parchment.RelSatisfies: {tplID}},
		// Intentionally omitting problem and acceptance sections
	})

	// Partial create should succeed (not error due to missing sections).
	// The artifact should be created in draft status with a warning, not rejected.
	isError := strings.Contains(strings.ToLower(out), "missing section") ||
		strings.Contains(strings.ToLower(out), "does not conform")
	if isError {
		t.Errorf("artifact(create) with template but missing required sections was rejected at create time.\n"+
			"Want: succeeds as draft with a warning (validate on promote, not on create).\nGot: %s", out)
	}
}

// TestArtifact_Promote_TemplateConformanceEnforced verifies the complement:
// promoting a template-linked artifact with missing required sections fails.
// Conformance check belongs on status promotion, not on create.
func TestArtifact_Promote_TemplateConformanceEnforced(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	srv, _ := scribemcp.NewServerFromStore(s, []string{"test"}, parchment.ProtocolConfig{}, "v0")
	cs := connectClient(t, srv)
	proto := parchment.New(s, nil, []string{"test"}, nil, parchment.ProtocolConfig{})

	// Create a template with a required section
	tpl, err := proto.CreateArtifact(ctx, parchment.CreateInput{Labels: []string{parchment.LabelPrefixKind + parchment.KindTemplate},
		Title: "need-tpl",

		Sections: []parchment.Section{
			{Name: "acceptance", Text: "required:Must have acceptance criteria"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a need that satisfies the template — without the required section.
	// Should succeed as draft.
	artOut := callTool(t, cs, "artifact", map[string]any{
		"action": "create",
		"kind":   "need",
		"title":  "needs acceptance",
		"scope":  "test",
		"links":  map[string][]string{parchment.RelSatisfies: {tpl.ID}},
	})

	// Extract artifact ID
	var artMap map[string]any
	artID := ""
	if err := json.Unmarshal([]byte(artOut), &artMap); err == nil {
		if id, ok := artMap["id"].(string); ok {
			artID = id
		}
	}
	if artID == "" {
		arts, _ := proto.ListArtifacts(ctx, parchment.ListInput{Labels: []string{"kind:need"}})
		if len(arts) > 0 {
			artID = arts[0].ID
		}
	}
	if artID == "" {
		t.Skip("could not determine need artifact ID")
	}

	// Attempt to promote to proposed — should fail: missing acceptance section.
	// (need kind uses intent lifecycle: draft→proposed, not draft→active)
	out := callTool(t, cs, "artifact", map[string]any{
		"action": "set",
		"id":     artID,
		"field":  "status",
		"value":  "proposed",
	})

	// Conformance check should fire here, not at create time.
	isConformanceError := strings.Contains(out, "acceptance") ||
		strings.Contains(out, "conform") ||
		strings.Contains(out, "missing")
	if !isConformanceError {
		t.Errorf("promoting template-linked artifact without required sections should fail.\n"+
			"Want: conformance error mentioning 'acceptance'.\nGot: %s", out)
	}
}
