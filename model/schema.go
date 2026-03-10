package model

import (
	"fmt"
	"sort"
	"strings"
)

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

// legacyPrefixes maps old kind names to their ID prefixes so existing
// artifacts (e.g. CON-2026-307) still resolve correctly on read.
var legacyPrefixes = map[string]string{
	"contract":      "CON",
	"specification": "SPEC",
	"rule":          "RULE",
	"need":          "NEED",
	"batch":         "BAT",
	"architecture":  "ARCH",
	"doc":           "DOC",
	"binder":        "BND",
	"epic":          "EPIC",
	"story":         "STORY",
	"subtask":       "SUB",
	"spike":         "SPIKE",
	"note":          "NOTE",
	"feature":       "FEA",
	"decision":      "DEC",
}

// KindAbsorption maps legacy kind names to their canonical replacements.
var KindAbsorption = map[string]string{
	"contract":      "task",
	"story":         "task",
	"feature":       "task",
	"need":          "task",
	"batch":         "task",
	"subtask":       "task",
	"doc":           "task",
	"binder":        "task",
	"spike":         "task",
	"note":          "task",
	"epic":          "goal",
	"specification": "spec",
	"architecture":  "spec",
	"decision":      "spec",
}

// Prefix returns the ID prefix for a kind. Canonical kinds use the schema,
// legacy kinds use the legacy prefix map, unknown kinds derive from the
// uppercased name.
func (s *Schema) Prefix(kind string) string {
	if kd, ok := s.Kinds[kind]; ok {
		return kd.Prefix
	}
	if p, ok := legacyPrefixes[kind]; ok {
		return p
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

// ValidateKind checks whether kind is in the allowed vocabulary.
// If vocab is nil or empty, all kinds are accepted (backward-compatible).
func ValidateKind(kind string, vocab []string) error {
	if len(vocab) == 0 {
		return nil
	}
	for _, v := range vocab {
		if v == kind {
			return nil
		}
	}
	sorted := make([]string, len(vocab))
	copy(sorted, vocab)
	sort.Strings(sorted)
	return fmt.Errorf("unknown kind %q — registered kinds: %s. To register a new kind: scribe vocab add %s",
		kind, strings.Join(sorted, ", "), kind)
}

// DefaultSchema returns the built-in schema with the canonical 5-kind vocabulary.
func DefaultSchema() *Schema {
	return &Schema{
		Kinds: map[string]KindDef{
			"goal":   {Prefix: "GOAL"},
			"sprint": {Prefix: "SPR"},
			"task":   {Prefix: "TASK"},
			"spec":   {Prefix: "SPEC"},
			"bug":    {Prefix: "BUG"},
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
