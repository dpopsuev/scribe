package model

import "strings"

// KindDef describes a known artifact kind.
type KindDef struct {
	Prefix          string `json:"prefix" yaml:"prefix"`
	ExcludeFromList bool   `json:"exclude_from_list,omitempty" yaml:"exclude_from_list,omitempty"`
}

// Schema is the single source of truth for the Scribe data model.
type Schema struct {
	Kinds     map[string]KindDef `json:"kinds" yaml:"kinds"`
	Statuses  []string           `json:"statuses" yaml:"statuses"`
	Relations []string           `json:"relations" yaml:"relations"`
	Guards    Guards             `json:"guards" yaml:"guards"`
}

// Guards defines which business-rule guards are active.
type Guards struct {
	ArchivedReadonly                       bool `json:"archived_readonly" yaml:"archived_readonly"`
	CompletionRequiresChildrenComplete     bool `json:"completion_requires_children_complete" yaml:"completion_requires_children_complete"`
	AutoArchiveGoalOnJustifyComplete       bool `json:"auto_archive_goal_on_justify_complete" yaml:"auto_archive_goal_on_justify_complete"`
	DeleteRequiresArchived                 bool `json:"delete_requires_archived" yaml:"delete_requires_archived"`
	AutoCompleteParentOnChildrenTerminal   bool `json:"auto_complete_parent_on_children_terminal" yaml:"auto_complete_parent_on_children_terminal"`
	AutoActivateNextDraftSprint            bool `json:"auto_activate_next_draft_sprint" yaml:"auto_activate_next_draft_sprint"`
}

// Prefix returns the ID prefix for a kind. Known kinds use the schema;
// unknown kinds derive from the uppercased name (open world).
func (s *Schema) Prefix(kind string) string {
	if kd, ok := s.Kinds[kind]; ok {
		return kd.Prefix
	}
	if len(kind) >= 3 {
		return strings.ToUpper(kind[:3])
	}
	return strings.ToUpper(kind)
}

// ExcludedKinds returns the kinds that should be hidden from default list output.
func (s *Schema) ExcludedKinds() []string {
	var out []string
	for k, def := range s.Kinds {
		if def.ExcludeFromList {
			out = append(out, k)
		}
	}
	return out
}

// DefaultSchema returns the built-in schema.
func DefaultSchema() *Schema {
	return &Schema{
		Kinds: map[string]KindDef{
			"contract":      {Prefix: "CON"},
			"specification": {Prefix: "SPEC"},
			"rule":          {Prefix: "RULE"},
			"need":          {Prefix: "NEED"},
			"sprint":        {Prefix: "SPR"},
			"batch":         {Prefix: "BAT"},
			"architecture":  {Prefix: "ARCH"},
			"doc":           {Prefix: "DOC"},
			"binder":        {Prefix: "BND"},
			"goal":          {Prefix: "GOAL"},
			"epic":          {Prefix: "EPIC"},
			"story":         {Prefix: "STORY"},
			"task":          {Prefix: "TASK"},
			"subtask":       {Prefix: "SUB"},
			"bug":           {Prefix: "BUG"},
			"spike":         {Prefix: "SPIKE"},
			"note":          {Prefix: "NOTE", ExcludeFromList: true},
		},
		Statuses: []string{
			"draft", "active", "current", "open",
			"complete", "dismissed", "promoted",
			"retired", "archived",
		},
		Relations: []string{
			RelParentOf, RelDependsOn, RelJustifies,
			RelImplements, RelDocuments, RelSatisfies,
		},
		Guards: Guards{
			ArchivedReadonly:                     true,
			CompletionRequiresChildrenComplete:   true,
			AutoArchiveGoalOnJustifyComplete:     true,
			DeleteRequiresArchived:               true,
			AutoCompleteParentOnChildrenTerminal: true,
			AutoActivateNextDraftSprint:          true,
		},
	}
}
