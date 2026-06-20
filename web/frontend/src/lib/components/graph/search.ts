import type { GraphNode } from './GraphCanvas.svelte';

export interface GraphEdge {
  source: string;
  target: string;
}

export function fuzzyMatch(text: string, query: string): boolean {
  const lower = text.toLowerCase();
  const q = query.toLowerCase();
  let qi = 0;
  for (let i = 0; i < lower.length && qi < q.length; i++) {
    if (lower[i] === q[qi]) qi++;
  }
  return qi === q.length;
}

export function filterNodes(nodes: GraphNode[], query: string): GraphNode[] {
  if (!query.trim()) return nodes;
  return nodes.filter(n =>
    fuzzyMatch(n.label || '', query) ||
    fuzzyMatch(n.id || '', query) ||
    fuzzyMatch(n.kind || '', query)
  );
}

export function filterEdges<E extends GraphEdge>(edges: E[], matchedNodes: GraphNode[]): E[] {
  const ids = new Set(matchedNodes.map(n => n.id));
  return edges.filter(e => ids.has(e.source) && ids.has(e.target));
}
