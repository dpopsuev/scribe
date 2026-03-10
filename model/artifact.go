package model

import (
	"fmt"
	"strings"
	"time"
)

// Artifact is the universal record for all work graph nodes.
type Artifact struct {
	ID        string              `json:"id"`
	Kind      string              `json:"kind"`
	Scope     string              `json:"scope,omitempty"`
	Status    string              `json:"status"`
	Parent    string              `json:"parent,omitempty"`
	Title     string              `json:"title"`
	Goal      string              `json:"goal,omitempty"`
	DependsOn []string            `json:"depends_on,omitempty"`
	Labels    []string            `json:"labels,omitempty"`
	Priority  string              `json:"priority,omitempty"`
	Sprint    string              `json:"sprint,omitempty"`
	Sections  []Section           `json:"sections,omitempty"`
	Features  []Feature           `json:"features,omitempty"`
	Criteria  []Criterion         `json:"criteria,omitempty"`
	Links     map[string][]string `json:"links,omitempty"`
	Extra     map[string]any      `json:"extra,omitempty"`
	CreatedAt  time.Time           `json:"created_at"`
	UpdatedAt  time.Time           `json:"updated_at"`
	InsertedAt time.Time           `json:"inserted_at"`
}

// Section is a named free-text block within an artifact.
type Section struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// Feature is a Gherkin-style feature containing scenarios.
type Feature struct {
	Name      string     `json:"name"`
	Scenarios []Scenario `json:"scenarios,omitempty"`
}

// Scenario is a single test scenario within a feature.
type Scenario struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	Steps  []Step `json:"steps,omitempty"`
}

// Step is a single Gherkin step (Given/When/Then/And/But).
type Step struct {
	Keyword string `json:"keyword"`
	Text    string `json:"text"`
}

// Criterion is an acceptance criterion with optional verification method.
type Criterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	VerifiedBy  string `json:"verified_by,omitempty"`
}

// Edge represents a directed relationship between two artifacts.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

// Well-known edge relations.
const (
	RelParentOf  = "parent_of"
	RelDependsOn = "depends_on"
	RelJustifies = "justifies"
	RelImplements = "implements"
	RelDocuments = "documents"
	RelSatisfies = "satisfies"
)

// Filter constrains artifact list/query operations.
type Filter struct {
	Kind          string
	ExcludeKinds  []string // kinds to exclude from results (e.g. ["note"])
	ExcludeKind   string   // exclude artifacts of this kind (single-kind exclusion)
	ExcludeStatus string   // exclude artifacts with this status
	IDPrefix      string   // match artifacts whose ID starts with this prefix
	Scope         string
	Scopes        []string // multi-scope IN filter (takes precedence over Scope when non-empty)
	Status        string
	Parent        string
	Sprint        string
	Labels        []string
	CreatedAfter  string
	CreatedBefore string
	UpdatedAfter  string
	UpdatedBefore string
	InsertedAfter  string
	InsertedBefore string
}

// Matches reports whether art satisfies all non-zero filter fields.
func (f Filter) Matches(art *Artifact) bool {
	if f.IDPrefix != "" && !strings.HasPrefix(art.ID, f.IDPrefix) {
		return false
	}
	if f.ExcludeKind != "" && art.Kind == f.ExcludeKind {
		return false
	}
	if f.ExcludeStatus != "" && art.Status == f.ExcludeStatus {
		return false
	}
	if f.Kind != "" && art.Kind != f.Kind {
		return false
	}
	for _, ek := range f.ExcludeKinds {
		if art.Kind == ek {
			return false
		}
	}
	if len(f.Scopes) > 0 {
		found := false
		for _, s := range f.Scopes {
			if art.Scope == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	} else if f.Scope != "" && art.Scope != f.Scope {
		return false
	}
	if f.Status != "" && art.Status != f.Status {
		return false
	}
	if f.Parent != "" && art.Parent != f.Parent {
		return false
	}
	if f.Sprint != "" && art.Sprint != f.Sprint {
		return false
	}
	if len(f.Labels) > 0 {
		have := make(map[string]bool, len(art.Labels))
		for _, l := range art.Labels {
			have[l] = true
		}
		for _, want := range f.Labels {
			if !have[want] {
				return false
			}
		}
	}
	return true
}

// FormatID produces PREFIX-YYYY-SEQ with minimum 3-digit zero-padded sequence.
func FormatID(prefix string, seq int) string {
	return fmt.Sprintf("%s-%d-%03d", prefix, time.Now().Year(), seq)
}

// FormatScopedID produces PRJ-ART-N format with no zero-padding and no year.
func FormatScopedID(scopeKey, kindCode string, seq int) string {
	return fmt.Sprintf("%s-%s-%d", scopeKey, kindCode, seq)
}

// DefaultPrefix returns the canonical ID prefix for a given artifact kind.
// Delegates to the built-in schema (see schema.go).
func DefaultPrefix(kind string) string {
	return DefaultSchema().Prefix(kind)
}
