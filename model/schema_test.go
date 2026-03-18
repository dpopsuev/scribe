package model

import (
	"testing"
)

func TestMergeDefaults_CustomKindPreserved(t *testing.T) {
	custom := &Schema{
		Kinds: map[string]KindDef{
			"epic": {Prefix: "EPIC", Code: "EPC"},
		},
	}
	custom.MergeDefaults(DefaultSchema())

	if kd, ok := custom.Kinds["epic"]; !ok {
		t.Fatal("custom kind 'epic' lost after merge")
	} else if kd.Prefix != "EPIC" {
		t.Errorf("custom kind prefix changed: got %q", kd.Prefix)
	}
}

func TestMergeDefaults_DefaultKindsFilled(t *testing.T) {
	custom := &Schema{
		Kinds: map[string]KindDef{
			"epic": {Prefix: "EPIC"},
		},
	}
	custom.MergeDefaults(DefaultSchema())

	for _, name := range []string{"task", "spec", "bug", "goal", "campaign", "template"} {
		if _, ok := custom.Kinds[name]; !ok {
			t.Errorf("default kind %q missing after merge", name)
		}
	}
}

func TestMergeDefaults_CustomKindOverridesDefault(t *testing.T) {
	custom := &Schema{
		Kinds: map[string]KindDef{
			"task": {Prefix: "MY_TASK", Code: "MTK"},
		},
	}
	custom.MergeDefaults(DefaultSchema())

	if custom.Kinds["task"].Prefix != "MY_TASK" {
		t.Errorf("custom task definition was overridden by default: got prefix %q", custom.Kinds["task"].Prefix)
	}
}

func TestMergeDefaults_StatusesFromDefault(t *testing.T) {
	custom := &Schema{
		Kinds: map[string]KindDef{
			"epic": {Prefix: "EPIC"},
		},
	}
	custom.MergeDefaults(DefaultSchema())

	if len(custom.Statuses) == 0 {
		t.Fatal("statuses not filled from defaults")
	}
	if len(custom.TerminalStatuses) == 0 {
		t.Fatal("terminal_statuses not filled from defaults")
	}
	if len(custom.Relations) == 0 {
		t.Fatal("relations not filled from defaults")
	}
	if len(custom.Priorities) == 0 {
		t.Fatal("priorities not filled from defaults")
	}
	if custom.DefaultPriority == "" {
		t.Fatal("default_priority not filled from defaults")
	}
	if custom.Guards == (Guards{}) {
		t.Fatal("guards not filled from defaults")
	}
}

func TestMergeDefaults_UserStatusesPreserved(t *testing.T) {
	custom := &Schema{
		Statuses: []string{"open", "closed"},
	}
	custom.MergeDefaults(DefaultSchema())

	if len(custom.Statuses) != 2 || custom.Statuses[0] != "open" {
		t.Errorf("user statuses were overridden: got %v", custom.Statuses)
	}
}

func TestMissingCompletionGates_AllFilled(t *testing.T) {
	s := &Schema{
		Kinds: map[string]KindDef{
			"task": {CompletionGates: []string{"test_matrix", "acceptance"}},
		},
	}
	art := &Artifact{
		Kind: "task",
		Sections: []Section{
			{Name: "test_matrix", Text: "file:Symbol"},
			{Name: "acceptance", Text: "criteria"},
		},
	}
	if missing := s.MissingCompletionGates(art); len(missing) != 0 {
		t.Errorf("expected no missing gates, got %v", missing)
	}
}

func TestMissingCompletionGates_SomeMissing(t *testing.T) {
	s := &Schema{
		Kinds: map[string]KindDef{
			"task": {CompletionGates: []string{"test_matrix", "acceptance"}},
		},
	}
	art := &Artifact{
		Kind: "task",
		Sections: []Section{
			{Name: "test_matrix", Text: "file:Symbol"},
		},
	}
	missing := s.MissingCompletionGates(art)
	if len(missing) != 1 || missing[0] != "acceptance" {
		t.Errorf("expected [acceptance], got %v", missing)
	}
}

func TestMissingCompletionGates_EmptySectionBlocks(t *testing.T) {
	s := &Schema{
		Kinds: map[string]KindDef{
			"task": {CompletionGates: []string{"test_matrix"}},
		},
	}
	art := &Artifact{
		Kind: "task",
		Sections: []Section{
			{Name: "test_matrix", Text: "   "},
		},
	}
	missing := s.MissingCompletionGates(art)
	if len(missing) != 1 {
		t.Errorf("empty/whitespace section should count as missing, got %v", missing)
	}
}

func TestMissingCompletionGates_NoGatesDefined(t *testing.T) {
	s := &Schema{
		Kinds: map[string]KindDef{
			"task": {},
		},
	}
	art := &Artifact{Kind: "task"}
	if missing := s.MissingCompletionGates(art); len(missing) != 0 {
		t.Errorf("no gates defined should return nil, got %v", missing)
	}
}

func TestMergeDefaults_NilSchema(t *testing.T) {
	custom := &Schema{}
	custom.MergeDefaults(DefaultSchema())

	defaults := DefaultSchema()
	if len(custom.Kinds) != len(defaults.Kinds) {
		t.Errorf("expected %d kinds, got %d", len(defaults.Kinds), len(custom.Kinds))
	}
}
