package web

import (
	"net/http"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	parchment "github.com/dpopsuev/parchment"
)

type schemaLayer struct {
	Kinds []string `json:"kinds"`
	Label string   `json:"label"`
}

type schemaNamespace struct {
	Layers []schemaLayer `json:"layers"`
}

type schemaHierarchy struct {
	Namespaces map[string]schemaNamespace `json:"namespaces"`
}

func buildHierarchy(proto *parchment.Protocol) schemaHierarchy {
	children, hasParent := parentOfDAG(proto)
	byNS := kindsByNamespace(proto.AllKinds())

	h := schemaHierarchy{Namespaces: make(map[string]schemaNamespace)}
	for ns, kinds := range byNS {
		layers := namespaceLayers(kinds, children, hasParent)
		if len(layers) > 0 {
			h.Namespaces[ns] = schemaNamespace{Layers: layers}
		}
	}
	return h
}

func parentOfDAG(proto *parchment.Protocol) (children map[string][]string, hasParent map[string]bool) {
	children = map[string][]string{}
	hasParent = map[string]bool{}
	for _, kind := range proto.AllKinds() {
		for _, rel := range proto.ValidRelationsFor(kind) {
			if rel.Relation != "parent_of" || rel.Target == "*" {
				continue
			}
			children[kind] = append(children[kind], rel.Target)
			hasParent[rel.Target] = true
		}
	}
	return
}

func kindsByNamespace(allKinds []string) map[string][]string {
	byNS := map[string][]string{}
	for _, kind := range allKinds {
		ns := kind
		if dot := strings.IndexByte(kind, '.'); dot > 0 {
			ns = kind[:dot]
		}
		byNS[ns] = append(byNS[ns], kind)
	}
	return byNS
}

func namespaceLayers(kinds []string, children map[string][]string, hasParent map[string]bool) []schemaLayer {
	var roots []string
	for _, k := range kinds {
		if !hasParent[k] {
			roots = append(roots, k)
		}
	}
	if len(roots) == 0 {
		roots = kinds
	}
	sort.Strings(roots)

	var layers []schemaLayer
	current := roots
	visited := map[string]bool{}
	for len(current) > 0 {
		sort.Strings(current)
		layers = append(layers, schemaLayer{Kinds: current, Label: labelForKinds(current)})
		for _, k := range current {
			visited[k] = true
		}
		current = nextLevel(current, children, visited)
	}

	layers = appendUnreachedKinds(layers, kinds)
	return layers
}

func nextLevel(current []string, children map[string][]string, visited map[string]bool) []string {
	var next []string
	seen := map[string]bool{}
	for _, k := range current {
		for _, ch := range children[k] {
			if !visited[ch] && !seen[ch] {
				next = append(next, ch)
				seen[ch] = true
			}
		}
	}
	return next
}

func appendUnreachedKinds(layers []schemaLayer, kinds []string) []schemaLayer {
	reachable := map[string]bool{}
	for _, layer := range layers {
		for _, k := range layer.Kinds {
			reachable[k] = true
		}
	}
	var extra []string
	for _, k := range kinds {
		if !reachable[k] {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		layers = append(layers, schemaLayer{Kinds: extra, Label: "Related"})
	}
	return layers
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

func labelForKinds(kinds []string) string {
	if len(kinds) == 1 {
		k := kinds[0]
		if dot := strings.LastIndexByte(k, '.'); dot >= 0 {
			return capitalize(k[dot+1:])
		}
		return capitalize(k)
	}
	var parts []string
	for _, k := range kinds {
		short := k
		if dot := strings.LastIndexByte(k, '.'); dot >= 0 {
			short = k[dot+1:]
		}
		parts = append(parts, short)
	}
	return strings.Join(parts, " · ")
}

func (s *Server) handleAPISchemaHierarchy(w http.ResponseWriter, _ *http.Request) {
	h := buildHierarchy(s.svc.Proto)
	writeJSON(w, h)
}
