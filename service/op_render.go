package service

import (
	"fmt"
	"strings"

	parchment "github.com/dpopsuev/parchment"
)

// nodeLabelFn returns the bracket content for a tree node — Strategy pattern.
// renderTree uses statusLabel; renderBriefing uses kindStatusLabel.
type nodeLabelFn func(labels []string) string

func statusLabel(labels []string) string {
	return parchment.StatusFromLabels(labels)
}

func kindStatusLabel(labels []string) string {
	kind := labelVal(labels, parchment.LabelPrefixKind)
	status := parchment.StatusFromLabels(labels)
	if kind != "" {
		return kind + "|" + status
	}
	return status
}

// renderNode is the shared recursive tree renderer.
// The label strategy is injected so tree and briefing views differ only in bracket content.
func renderNode(node *parchment.TreeNode, prefix string, last, showScope bool, b *strings.Builder, labelFn nodeLabelFn) {
	connector := "├── "
	if last {
		connector = "└── "
	}
	if prefix == "" {
		connector = ""
	}
	edgeLabel := ""
	if node.Edge != "" {
		arrow := " -> "
		if node.Direction == "incoming" {
			arrow = " <- "
		}
		edgeLabel = node.Edge + arrow
	}
	scopeLabel := ""
	if showScope {
		if sc := labelVal(node.Labels, parchment.LabelPrefixScope); sc != "" {
			scopeLabel = fmt.Sprintf(" [%s]", sc)
		}
	}
	fmt.Fprintf(b, "%s%s%s%s%s [%s] %s\n",
		prefix, connector, edgeLabel, node.ID, scopeLabel, labelFn(node.Labels), node.Title)
	childPrefix := prefix
	if prefix != "" {
		if last {
			childPrefix += "    "
		} else {
			childPrefix += "│   "
		}
	}
	for i, ch := range node.Children {
		renderNode(ch, childPrefix, i == len(node.Children)-1, showScope, b, labelFn)
	}
}

func renderTree(node *parchment.TreeNode) string {
	var b strings.Builder
	renderNode(node, "", true, countDistinctScopes(node) > 1, &b, statusLabel)
	return b.String()
}

func renderBriefing(node *parchment.TreeNode) string {
	var b strings.Builder
	renderNode(node, "", true, countDistinctScopes(node) > 1, &b, kindStatusLabel)
	return b.String()
}

func countDistinctScopes(node *parchment.TreeNode) int {
	scopes := map[string]struct{}{}
	var walk func(n *parchment.TreeNode)
	walk = func(n *parchment.TreeNode) {
		if sc := labelVal(n.Labels, parchment.LabelPrefixScope); sc != "" {
			scopes[sc] = struct{}{}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(node)
	return len(scopes)
}
