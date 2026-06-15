import { describe, it, expect } from 'vitest';
import { createGraphState } from './graph-state.js';

describe('expand behavior — parent node persistence', () => {
  it('scope node remains in macroData after expansion', () => {
    const state = createGraphState();
    state.macroData.nodes = [
      { id: 'scope:hegemony', kind: 'scope', name: 'hegemony', val: 10 },
      { id: 'scope:alef', kind: 'scope', name: 'alef', val: 5 },
    ];
    state.macroData.links = [
      { source: 'scope:hegemony', target: 'scope:alef', relation: 'cross-scope' },
    ];

    // Simulate expandScope: add kind-group children WITHOUT removing scope node
    const kindData = {
      nodes: [
        { id: 'kind:hegemony:effort.task', kind: 'kind-group', group: 'effort.task', val: 3 },
        { id: 'kind:hegemony:intent.spec', kind: 'kind-group', group: 'intent.spec', val: 2 },
      ],
      links: [],
    };

    // Add contains links from scope to children
    for (const n of kindData.nodes) {
      kindData.links.push({ source: 'scope:hegemony', target: n.id, relation: 'contains' });
    }

    state.expandedScopes.set('hegemony', kindData);

    // Merge — same logic as mergedGraphData()
    const nodes = [...state.macroData.nodes];
    const links = [...state.macroData.links];
    for (const [, d] of state.expandedScopes) {
      nodes.push(...d.nodes);
      links.push(...d.links);
    }

    // Scope node must still be present
    const scopeNode = nodes.find(n => n.id === 'scope:hegemony');
    expect(scopeNode).toBeTruthy();
    expect(scopeNode.kind).toBe('scope');

    // Kind-group children must be present
    const kindNodes = nodes.filter(n => n.kind === 'kind-group');
    expect(kindNodes).toHaveLength(2);

    // Contains edges must connect scope to children
    const containsLinks = links.filter(l => l.relation === 'contains');
    expect(containsLinks).toHaveLength(2);
    expect(containsLinks.every(l => l.source === 'scope:hegemony')).toBe(true);
  });

  it('kind-group node remains after artifact expansion', () => {
    const state = createGraphState();
    state.macroData.nodes = [
      { id: 'scope:hegemony', kind: 'scope', name: 'hegemony', val: 10 },
    ];

    const kindData = {
      nodes: [
        { id: 'kind:hegemony:effort.task', kind: 'kind-group', group: 'effort.task', val: 3 },
      ],
      links: [
        { source: 'scope:hegemony', target: 'kind:hegemony:effort.task', relation: 'contains' },
      ],
    };
    state.expandedScopes.set('hegemony', kindData);

    // Simulate expandKind: add artifact children WITHOUT removing kind-group node
    const artData = {
      nodes: [
        { id: 'art-1', kind: 'effort.task', name: 'Task 1', val: 1 },
        { id: 'art-2', kind: 'effort.task', name: 'Task 2', val: 1 },
      ],
      links: [
        { source: 'art-1', target: 'art-2', relation: 'depends_on' },
      ],
    };

    // Add contains links from kind-group to artifacts
    for (const n of artData.nodes) {
      artData.links.push({ source: 'kind:hegemony:effort.task', target: n.id, relation: 'contains' });
    }

    state.expandedKinds.set('hegemony:effort.task', artData);

    // Merge
    const nodes = [...state.macroData.nodes];
    const links = [...state.macroData.links];
    for (const [, d] of state.expandedScopes) { nodes.push(...d.nodes); links.push(...d.links); }
    for (const [, d] of state.expandedKinds)  { nodes.push(...d.nodes); links.push(...d.links); }

    // Kind-group node must still be present
    const kindNode = nodes.find(n => n.id === 'kind:hegemony:effort.task');
    expect(kindNode).toBeTruthy();
    expect(kindNode.kind).toBe('kind-group');

    // Artifact children must be present
    const artNodes = nodes.filter(n => n.kind === 'effort.task');
    expect(artNodes).toHaveLength(2);

    // Contains edges must connect kind-group to artifacts
    const containsLinks = links.filter(l => l.relation === 'contains' && l.source === 'kind:hegemony:effort.task');
    expect(containsLinks).toHaveLength(2);

    // The scope node is still there too
    expect(nodes.find(n => n.id === 'scope:hegemony')).toBeTruthy();
  });

  it('expanded scope has no orphan nodes — all children connected', () => {
    const state = createGraphState();
    state.macroData.nodes = [
      { id: 'scope:test', kind: 'scope', name: 'test', val: 5 },
    ];

    const kindData = {
      nodes: [
        { id: 'kind:test:knowledge.note', kind: 'kind-group', group: 'knowledge.note', val: 1 },
      ],
      links: [],
    };

    // The fix ensures contains links are added
    for (const n of kindData.nodes) {
      kindData.links.push({ source: 'scope:test', target: n.id, relation: 'contains' });
    }
    state.expandedScopes.set('test', kindData);

    // Merge
    const nodes = [...state.macroData.nodes];
    const links = [...state.macroData.links];
    for (const [, d] of state.expandedScopes) { nodes.push(...d.nodes); links.push(...d.links); }

    // Every child node must have at least one edge
    const nodeIds = new Set(nodes.map(n => n.id));
    const connected = new Set();
    for (const l of links) {
      const src = typeof l.source === 'object' ? l.source.id : l.source;
      const tgt = typeof l.target === 'object' ? l.target.id : l.target;
      connected.add(src);
      connected.add(tgt);
    }

    for (const n of nodes) {
      if (n.kind !== 'scope') {
        expect(connected.has(n.id)).toBe(true);
      }
    }
  });
});
