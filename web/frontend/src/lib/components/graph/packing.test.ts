import { describe, it, expect } from 'vitest';
import { computePacking, layoutChildren, layoutFromPack, parentSizeForChildren, PACKING_K, MIN_CHILD_SIZE } from './packing';

describe('computePacking', () => {
  it('childSize always less than output parentSize', () => {
    for (const ps of [1.0, 3.0, 5.0, 10.0, 18.0]) {
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

  it('parent size is preserved — children shrink to fit', () => {
    const pack = computePacking(1, 5);
    expect(pack.parentSize).toBe(1);
    expect(pack.childSize).toBeGreaterThanOrEqual(MIN_CHILD_SIZE);
    expect(pack.childSize).toBeLessThan(pack.parentSize);
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

  for (const [ps, n] of [[18, 10], [18, 30], [12, 8], [8, 5], [10, 20]] as [number, number][]) {
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
      const layout = layoutFromPack(pack, n);
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
    const l1 = layoutFromPack(l1Pack, 12);
    for (const p of l1) {
      expect(Math.hypot(p.x, p.y) + p.size).toBeLessThanOrEqual(l1Pack.parentSize * 1.01);
      expect(p.size).toBeLessThan(l1Pack.parentSize);
    }

    // Level 2: a kind-group (size = l1 child) expands into 30 artifacts
    const kgSize = l1Pack.childSize;
    const l2Pack = computePacking(kgSize, 30);
    const l2 = layoutFromPack(l2Pack, 30);
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
  for (const [ps, n] of [[18, 10], [12.5, 14], [5, 20], [3, 50], [1.5, 30]] as [number, number][]) {
    it(`parent=${ps} n=${n}: children inside parent, smaller than parent`, () => {
      const pack = computePacking(ps, n);
      const layout = layoutFromPack(pack, n);

      for (const c of layout) {
        expect(c.size).toBeLessThan(pack.parentSize);
        expect(Math.hypot(c.x, c.y) + c.size).toBeLessThanOrEqual(pack.parentSize * 1.01);
      }
    });
  }

  // Overlap check only for cases where children fit without the 2× cap
  for (const [ps, n] of [[18, 10], [50, 20], [100, 30]] as [number, number][]) {
    it(`parent=${ps} n=${n}: no geometric overlap`, () => {
      const pack = computePacking(ps, n);
      const layout = layoutFromPack(pack, n);

      let overlaps = 0;
      for (let i = 0; i < layout.length; i++)
        for (let j = i + 1; j < layout.length; j++)
          if (Math.hypot(layout[i].x - layout[j].x, layout[i].y - layout[j].y) < layout[i].size + layout[j].size)
            overlaps++;
      expect(overlaps).toBe(0);
    });
  }
});

describe('parentSizeForChildren', () => {
  it('computes parent large enough for children', () => {
    const childSize = 5;
    const n = 10;
    const ps = parentSizeForChildren(childSize, n);
    const pack = computePacking(ps, n);
    // With the 2× cap, the computed child may differ — but parent must
    // be large enough for the requested child orbit
    expect(pack.parentSize).toBeGreaterThanOrEqual(ps * 0.9);
    expect(pack.childSize).toBeGreaterThanOrEqual(MIN_CHILD_SIZE);
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

  it('small parent stays same size — children shrink', () => {
    const pack = computePacking(2, 5);
    expect(pack.parentSize).toBe(2);
    expect(pack.childSize).toBeGreaterThanOrEqual(MIN_CHILD_SIZE);
    expect(pack.childSize).toBeLessThan(pack.parentSize);
  });
});

describe('parent containment invariants', () => {
  for (const [ps, n] of [
    [18, 6],     // small scope
    [18, 14],    // hegemony scale
    [18, 200],   // Project-Alice scale
    [18, 500],   // extreme
    [5, 30],     // small parent
    [12.5, 14],  // exact hegemony repro
  ] as [number, number][]) {
    it(`parent=${ps} n=${n}: orbit + childSize inside parent ring`, () => {
      const pack = computePacking(ps, n);
      // Geometric containment: orbit + child fits inside parent
      expect(pack.orbitRadius + pack.childSize).toBeLessThanOrEqual(pack.parentSize * 0.86);
    });

    it(`parent=${ps} n=${n}: parent grows at most 2×`, () => {
      const pack = computePacking(ps, n);
      expect(pack.parentSize).toBeLessThanOrEqual(ps * 2.01);
      expect(pack.parentSize).toBeGreaterThanOrEqual(ps);
    });

    it(`parent=${ps} n=${n}: childSize < parentSize`, () => {
      const pack = computePacking(ps, n);
      expect(pack.childSize).toBeLessThan(pack.parentSize);
    });
  }
});
