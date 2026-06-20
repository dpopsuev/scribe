import { describe, it, expect } from 'vitest';
import { fuzzyMatch, filterNodes, filterEdges } from './search';
import type { GraphNode } from './GraphCanvas.svelte';

function node(id: string, label: string, kind: string): GraphNode {
  return { id, label, x: 0, y: 0, size: 5, color: '#fff', kind };
}

describe('fuzzyMatch', () => {
  it('matches exact string', () => {
    expect(fuzzyMatch('scribe', 'scribe')).toBe(true);
  });

  it('matches subsequence', () => {
    expect(fuzzyMatch('scribe', 'srb')).toBe(true);
  });

  it('matches case-insensitive', () => {
    expect(fuzzyMatch('Scribe', 'scribe')).toBe(true);
    expect(fuzzyMatch('scribe', 'SCRIBE')).toBe(true);
  });

  it('rejects non-subsequence', () => {
    expect(fuzzyMatch('scribe', 'xyz')).toBe(false);
  });

  it('matches empty query to anything', () => {
    expect(fuzzyMatch('scribe', '')).toBe(true);
    expect(fuzzyMatch('', '')).toBe(true);
  });

  it('rejects non-empty query against empty text', () => {
    expect(fuzzyMatch('', 'a')).toBe(false);
  });

  it('matches single character', () => {
    expect(fuzzyMatch('scribe', 's')).toBe(true);
    expect(fuzzyMatch('scribe', 'e')).toBe(true);
  });

  it('matches prefix', () => {
    expect(fuzzyMatch('scribe', 'scr')).toBe(true);
  });

  it('matches suffix', () => {
    expect(fuzzyMatch('scribe', 'ibe')).toBe(true);
  });
});

describe('filterNodes', () => {
  const nodes = [
    node('project:scribe', 'scribe', 'project'),
    node('project:locus', 'locus', 'project'),
    node('project:parchment', 'parchment', 'project'),
    node('kind:scribe:effort.task', 'effort.task', 'kind-group'),
    node('scribe/service:protocol', 'Protocol', 'code.struct'),
  ];

  it('returns all nodes for empty query', () => {
    expect(filterNodes(nodes, '')).toHaveLength(5);
    expect(filterNodes(nodes, '  ')).toHaveLength(5);
  });

  it('filters by label', () => {
    const result = filterNodes(nodes, 'scribe');
    expect(result.map(n => n.id)).toContain('project:scribe');
  });

  it('filters by id', () => {
    const result = filterNodes(nodes, 'service:protocol');
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('scribe/service:protocol');
  });

  it('filters by kind', () => {
    const result = filterNodes(nodes, 'code.struct');
    expect(result).toHaveLength(1);
    expect(result[0].label).toBe('Protocol');
  });

  it('fuzzy matches across label', () => {
    const result = filterNodes(nodes, 'pmt');
    expect(result.map(n => n.id)).toContain('project:parchment');
  });

  it('returns empty for no match', () => {
    expect(filterNodes(nodes, 'zzzzz')).toHaveLength(0);
  });
});

describe('filterNodes performance', () => {
  it('filters 10K nodes under 10ms', () => {
    const big: GraphNode[] = [];
    for (let i = 0; i < 10_000; i++) {
      big.push(node(`id-${i}`, `node-${i}-label`, i % 2 === 0 ? 'code.struct' : 'effort.task'));
    }
    const start = performance.now();
    const result = filterNodes(big, 'node-500');
    const elapsed = performance.now() - start;
    expect(result.length).toBeGreaterThan(0);
    expect(elapsed).toBeLessThan(10);
  });
});

describe('filterEdges', () => {
  const a = node('a', 'A', 'x');
  const b = node('b', 'B', 'x');
  const c = node('c', 'C', 'x');

  const edges = [
    { source: 'a', target: 'b', color: '#fff' },
    { source: 'b', target: 'c', color: '#fff' },
    { source: 'a', target: 'c', color: '#fff' },
  ];

  it('keeps edges where both endpoints match', () => {
    const result = filterEdges(edges, [a, b]);
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ source: 'a', target: 'b', color: '#fff' });
  });

  it('returns all edges when all nodes match', () => {
    expect(filterEdges(edges, [a, b, c])).toHaveLength(3);
  });

  it('returns no edges when no nodes match', () => {
    expect(filterEdges(edges, [])).toHaveLength(0);
  });

  it('drops edges with one dangling endpoint', () => {
    const result = filterEdges(edges, [a]);
    expect(result).toHaveLength(0);
  });
});
