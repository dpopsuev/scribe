package model

import (
	"fmt"
	"sort"
	"strings"
)

// KindDef describes a known artifact kind.
type KindDef struct {
	Prefix           string   `json:"prefix" yaml:"prefix"`
	Code             string   `json:"code,omitempty" yaml:"code,omitempty"`
	ExcludeFromList  bool     `json:"exclude_from_list,omitempty" yaml:"exclude_from_list,omitempty"`
	Protected        bool     `json:"protected,omitempty" yaml:"protected,omitempty"`
	ScopeOptional    bool     `json:"scope_optional,omitempty" yaml:"scope_optional,omitempty"`
	DefaultStatus    string   `json:"default_status,omitempty" yaml:"default_status,omitempty"`
	AutoActivateNext bool     `json:"auto_activate_next,omitempty" yaml:"auto_activate_next,omitempty"`
	ExpectedSections []string `json:"expected_sections,omitempty" yaml:"expected_sections,omitempty"`
}

// Schema is the single source of truth for the Scribe data model.
type Schema struct {
	Kinds            map[string]KindDef `json:"kinds" yaml:"kinds"`
	Statuses         []string           `json:"statuses" yaml:"statuses"`
	TerminalStatuses []string           `json:"terminal_statuses,omitempty" yaml:"terminal_statuses,omitempty"`
	ReadonlyStatuses []string           `json:"readonly_statuses,omitempty" yaml:"readonly_statuses,omitempty"`
	Relations        []string           `json:"relations" yaml:"relations"`
	Guards           Guards             `json:"guards" yaml:"guards"`
}

// Guards defines which business-rule guards are active.
type Guards struct {
	ArchivedReadonly                       bool `json:"archived_readonly" yaml:"archived_readonly"`
	CompletionRequiresChildrenComplete     bool `json:"completion_requires_children_complete" yaml:"completion_requires_children_complete"`
	AutoArchiveGoalOnJustifyComplete       bool `json:"auto_archive_goal_on_justify_complete" yaml:"auto_archive_goal_on_justify_complete"`
	DeleteRequiresArchived                 bool `json:"delete_requires_archived" yaml:"delete_requires_archived"`
	AutoCompleteParentOnChildrenTerminal   bool `json:"auto_complete_parent_on_children_terminal" yaml:"auto_complete_parent_on_children_terminal"`
	AutoActivateNextDraftSprint            bool `json:"auto_activate_next_draft_sprint" yaml:"auto_activate_next_draft_sprint"`
	ActivationRequiresExpectedSections     bool `json:"activation_requires_expected_sections,omitempty" yaml:"activation_requires_expected_sections,omitempty"`
}

// legacyPrefixes maps old kind names to their ID prefixes so existing
// artifacts (e.g. CON-2026-307) still resolve correctly on read.
var legacyPrefixes = map[string]string{
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

// KindCode returns the 3-letter scoped-ID code for a kind.
// Falls back to the first 3 uppercase letters of the kind name.
func (s *Schema) KindCode(kind string) string {
	if kd, ok := s.Kinds[kind]; ok && kd.Code != "" {
		return kd.Code
	}
	upper := strings.ToUpper(kind)
	if len(upper) >= 3 {
		return upper[:3]
	}
	return upper
}

// IsProtected returns true if the kind is protected from vacuum deletion.
func (s *Schema) IsProtected(kind string) bool {
	if kd, ok := s.Kinds[kind]; ok {
		return kd.Protected
	}
	return false
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

// IsTerminal reports whether status is a terminal (done/closed) state.
func (s *Schema) IsTerminal(status string) bool {
	for _, ts := range s.TerminalStatuses {
		if ts == status {
			return true
		}
	}
	return false
}

// IsReadonly reports whether status prohibits mutation.
func (s *Schema) IsReadonly(status string) bool {
	for _, rs := range s.ReadonlyStatuses {
		if rs == status {
			return true
		}
	}
	return false
}

// IsScopeOptional reports whether the kind can be created without a scope.
func (s *Schema) IsScopeOptional(kind string) bool {
	if kd, ok := s.Kinds[kind]; ok {
		return kd.ScopeOptional
	}
	return false
}

// DefaultStatus returns the default status for a kind, falling back to "draft".
func (s *Schema) DefaultStatus(kind string) string {
	if kd, ok := s.Kinds[kind]; ok && kd.DefaultStatus != "" {
		return kd.DefaultStatus
	}
	return "draft"
}

// GetExpectedSections returns the expected section names for a kind.
func (s *Schema) GetExpectedSections(kind string) []string {
	if kd, ok := s.Kinds[kind]; ok {
		return kd.ExpectedSections
	}
	return nil
}

// MissingSections returns expected section names not present in have.
func (s *Schema) MissingSections(kind string, have []Section) []string {
	expected := s.GetExpectedSections(kind)
	if len(expected) == 0 {
		return nil
	}
	present := make(map[string]bool, len(have))
	for _, sec := range have {
		present[sec.Name] = true
	}
	var missing []string
	for _, name := range expected {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// HasAutoActivateNext reports whether the kind should trigger next-draft activation on completion.
func (s *Schema) HasAutoActivateNext(kind string) bool {
	if kd, ok := s.Kinds[kind]; ok {
		return kd.AutoActivateNext
	}
	return false
}

// ValidRelation reports whether rel is a registered relation (or the wildcard "*").
func (s *Schema) ValidRelation(rel string) bool {
	if rel == "*" {
		return true
	}
	for _, r := range s.Relations {
		if r == rel {
			return true
		}
	}
	return false
}

// DefaultSchema returns the built-in schema with the canonical kind vocabulary.
func DefaultSchema() *Schema {
	return &Schema{
		Kinds: map[string]KindDef{
			"goal":   {Prefix: "GOAL", Code: "GOL"},
			"sprint": {Prefix: "SPR", Code: "SPR", AutoActivateNext: true},
			"task": {Prefix: "TASK", Code: "TSK",
				ExpectedSections: []string{"context", "checklist", "acceptance"}},
			"spec": {Prefix: "SPEC", Code: "SPC",
				ExpectedSections: []string{"problem", "decision", "acceptance"}},
			"bug": {Prefix: "BUG", Code: "BUG",
				ExpectedSections: []string{"observed", "reproduction"}},
			"decision": {Prefix: "ADR", Code: "ADR"},
			"campaign": {Prefix: "CMP", Code: "CMP", ScopeOptional: true,
				ExpectedSections: []string{"mission", "goals", "success_criteria"}},
		},
		Statuses: []string{
			"draft", "active", "current", "open",
			"complete", "dismissed", "promoted",
			"retired", "archived",
		},
		TerminalStatuses: []string{"complete", "cancelled", "dismissed", "retired", "archived"},
		ReadonlyStatuses: []string{"archived"},
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
