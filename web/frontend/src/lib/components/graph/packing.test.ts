import { describe, it, expect } from 'vitest';
import { computePacking, layoutChildren, parentSizeForChildren, PACKING_K, MIN_CHILD_SIZE } from './packing';

describe('computePacking', () => {
  it('childSize always less than output parentSize', () => {
    for (const ps of [0.5, 1.0, 3.0, 5.0, 10.0, 18.0]) {
      for (const n of [1, 3, 5, 10, 30, 50, 100]) {
        const pack = computePacking(ps, n);
        expect(pack.childSize).toBeLessThan(pack.parentSize);
      }
    }
  });

  it('parentSize >= original parentSize (only grows)', () => {
    const pack = computePacking(5, 10);
    expect(pack.parentSize).toBeGreaterThanOrEqual(5);
  });

  it('more children → smaller each (above MIN_CHILD_SIZE)', () => {
    // Use large parent so children aren't floored at MIN_CHILD_SIZE
    const p3 = computePacking(100, 3);
    const p10 = computePacking(100, 10);
    expect(p10.childSize).toBeLessThan(p3.childSize);
  });

  it('childSize >= MIN_CHILD_SIZE', () => {
    const pack = computePacking(0.5, 100);
    expect(pack.childSize).toBeGreaterThanOrEqual(MIN_CHILD_SIZE);
  });

  it('parent grows to accommodate MIN_CHILD_SIZE children', () => {
    // Parent=1 can't fit children of size 3 without growing
    const pack = computePacking(1, 5);
    expect(pack.parentSize).toBeGreaterThan(1);
    expect(pack.childSize).toBe(MIN_CHILD_SIZE);
  });
});

describe('layoutChildren — no overlaps', () => {
  function countOverlaps(positions: { x: number; y: number; size: number }[]): number {
    let count = 0;
    for (let i = 0; i < positions.length; i++) {
      for (let j = i + 1; j < positions.length; j++) {
        const dist = Math.hypot(positions[i].x - positions[j].x, positions[i].y - positions[j].y);
        if (dist < positions[i].size + positions[j].size) count++;
      }
    }
    return count;
  }

  for (const [ps, n] of [[18, 10], [18, 30], [12, 8], [8, 5], [5, 20]] as [number, number][]) {
    it(`parent=${ps} children=${n}`, () => {
      const pack = computePacking(ps, n);
      const layout = layoutChildren(pack.parentSize, n);
      expect(countOverlaps(layout)).toBe(0);
    });
  }
});

describe('containment — children inside parent ring', () => {
  for (const [ps, n] of [[18, 10], [18, 50], [5, 20], [3, 30]] as [number, number][]) {
    it(`parent=${ps} children=${n}`, () => {
      const pack = computePacking(ps, n);
      const layout = layoutChildren(pack.parentSize, n);
      for (const p of layout) {
        expect(Math.hypot(p.x, p.y) + p.size).toBeLessThanOrEqual(pack.parentSize * 1.01);
      }
    });
  }
});

describe('recursive nesting', () => {
  it('scope → kind-group → artifact: all levels valid', () => {
    // Simulate production flow: expandNode computes packing at each level
    const scopeSize = 18;

    // Level 1: scope expands into 12 kind-groups
    const l1Pack = computePacking(scopeSize, 12);
    const l1 = layoutChildren(l1Pack.parentSize, 12);
    for (const p of l1) {
      expect(Math.hypot(p.x, p.y) + p.size).toBeLessThanOrEqual(l1Pack.parentSize * 1.01);
      expect(p.size).toBeLessThan(l1Pack.parentSize);
    }

    // Level 2: a kind-group (size = l1 child) expands into 30 artifacts
    const kgSize = l1Pack.childSize;
    const l2Pack = computePacking(kgSize, 30);
    const l2 = layoutChildren(l2Pack.parentSize, 30);
    for (const p of l2) {
      expect(Math.hypot(p.x, p.y) + p.size).toBeLessThanOrEqual(l2Pack.parentSize * 1.01);
      expect(p.size).toBeLessThan(l2Pack.parentSize);
    }
  });

  it('child size strictly less than output parent at every depth', () => {
    let size = 100;
    for (const n of [10, 30, 50]) {
      const pack = computePacking(size, n);
      expect(pack.childSize).toBeLessThan(pack.parentSize);
      expect(pack.childSize).toBeGreaterThanOrEqual(MIN_CHILD_SIZE);
      size = pack.childSize;
    }
  });
});

describe('expandNode simulation — mirrors production code path', () => {
  function simulateExpandNode(parentSize: number, childCount: number) {
    const pack = computePacking(parentSize, childCount);
    const { childSize, orbitRadius, parentSize: newParentSize } = pack;
    const goldenAngle = 137.508 * Math.PI / 180;

    const children = [];
    for (let i = 0; i < childCount; i++) {
      const angle = i * goldenAngle;
      const r = orbitRadius * Math.sqrt((i + 0.5) / childCount);
      children.push({ x: r * Math.cos(angle), y: r * Math.sin(angle), size: childSize });
    }
    return { children, parentSize: newParentSize };
  }

  for (const [ps, n] of [[18, 10], [5, 20], [3, 50], [1.5, 30], [0.5, 5]] as [number, number][]) {
    it(`parent=${ps} n=${n}: children smaller than parent, inside ring, no overlap`, () => {
      const { children, parentSize } = simulateExpandNode(ps, n);

      for (const c of children) {
        expect(c.size).toBeLessThan(parentSize);
        expect(Math.hypot(c.x, c.y) + c.size).toBeLessThanOrEqual(parentSize * 1.01);
      }

      let overlaps = 0;
      for (let i = 0; i < children.length; i++)
        for (let j = i + 1; j < children.length; j++)
          if (Math.hypot(children[i].x - children[j].x, children[i].y - children[j].y) < children[i].size + children[j].size)
            overlaps++;
      expect(overlaps).toBe(0);
    });
  }
});

describe('parentSizeForChildren', () => {
  it('round-trips with layoutChildren', () => {
    const childSize = 5;
    const n = 10;
    const ps = parentSizeForChildren(childSize, n);
    const layout = layoutChildren(ps, n);
    expect(layout[0].size).toBeCloseTo(childSize, 1);
  });
});

describe('expandNode — mirrors +page.svelte expandNode', () => {
  for (const [ps, n] of [[18, 10], [50, 30], [100, 50]] as [number, number][]) {
    it(`parent=${ps} n=${n}: children smaller than parent, at least MIN_CHILD_SIZE`, () => {
      const pack = computePacking(ps, n);
      expect(pack.childSize).toBeLessThan(pack.parentSize);
      expect(pack.childSize).toBeGreaterThanOrEqual(MIN_CHILD_SIZE);
    });
  }

  it('small parent grows to fit MIN_CHILD_SIZE children', () => {
    const pack = computePacking(2, 5);
    expect(pack.parentSize).toBeGreaterThan(2);
    expect(pack.childSize).toBeGreaterThanOrEqual(MIN_CHILD_SIZE);
  });
});
