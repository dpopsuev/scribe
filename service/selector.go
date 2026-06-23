package service

import (
	"encoding/json"
	"fmt"

	parchment "github.com/dpopsuev/parchment"
)

// Selector identifies a precise location within an artifact's content.
// Used by the Pointer→Kernel model to anchor derived content to its
// source location.
type Selector struct {
	Section string `json:"section,omitempty"` // target section name
	Line    int    `json:"line,omitempty"`    // 1-based line within section
	Offset  int    `json:"offset,omitempty"`  // 0-based character offset
	Length  int    `json:"length,omitempty"`  // selection length in chars
	Anchor  string `json:"anchor,omitempty"`  // named anchor (heading ID, fragment)
}

// IsZero returns true if no selector fields are set.
func (s Selector) IsZero() bool {
	return s.Section == "" && s.Line == 0 && s.Offset == 0 && s.Length == 0 && s.Anchor == ""
}

// String returns a human-readable representation of the selector.
func (s Selector) String() string {
	if s.Anchor != "" {
		return "#" + s.Anchor
	}
	if s.Section != "" && s.Line > 0 {
		return fmt.Sprintf("%s:%d", s.Section, s.Line)
	}
	if s.Section != "" {
		return s.Section
	}
	if s.Line > 0 {
		return fmt.Sprintf("L%d", s.Line)
	}
	return ""
}

// extraKeySelector is the Extra key where a selector is stored.
const extraKeySelector = "selector"

// SetSelector stores a Selector in the artifact's Extra map.
func SetSelector(art *parchment.Artifact, sel Selector) {
	if art.Extra == nil {
		art.Extra = make(map[string]any)
	}
	art.Extra[extraKeySelector] = sel
}

// GetSelector reads a Selector from the artifact's Extra map.
// Returns a zero Selector if none is stored.
func GetSelector(art *parchment.Artifact) Selector {
	if art.Extra == nil {
		return Selector{}
	}
	raw, ok := art.Extra[extraKeySelector]
	if !ok {
		return Selector{}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return Selector{}
	}
	var sel Selector
	_ = json.Unmarshal(b, &sel)
	return sel
}

// EdgeSelector stores selector metadata for an edge relationship.
// Since parchment edges don't have an Extra field yet, this is stored
// as a convention in the source artifact's Extra under "edge_selectors".
type EdgeSelector struct {
	TargetID string   `json:"target_id"`
	Relation string   `json:"relation"`
	Selector Selector `json:"selector"`
}

const extraKeyEdgeSelectors = "edge_selectors"

// SetEdgeSelector adds or updates a selector for a specific edge on the artifact.
func SetEdgeSelector(art *parchment.Artifact, es EdgeSelector) {
	if art.Extra == nil {
		art.Extra = make(map[string]any)
	}
	existing := GetEdgeSelectors(art)
	for i, e := range existing {
		if e.TargetID == es.TargetID && e.Relation == es.Relation {
			existing[i] = es
			art.Extra[extraKeyEdgeSelectors] = existing
			return
		}
	}
	art.Extra[extraKeyEdgeSelectors] = append(existing, es)
}

// GetEdgeSelectors returns all edge selectors stored on the artifact.
func GetEdgeSelectors(art *parchment.Artifact) []EdgeSelector {
	if art.Extra == nil {
		return nil
	}
	raw, ok := art.Extra[extraKeyEdgeSelectors]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var sels []EdgeSelector
	_ = json.Unmarshal(b, &sels)
	return sels
}
