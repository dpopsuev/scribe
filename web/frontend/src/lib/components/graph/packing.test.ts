import { describe, it, expect } from 'vitest';

function molecularChildSize(parentSize: number, childCount: number): number {
  const ringInner = parentSize * 0.85;
  const packingK = 1.3;
  return Math.max(0.5, ringInner / (1 + Math.sqrt(childCount) * packingK));
}

// Bottom-up packing: leaf size is fixed, parent grows to contain.
//
// LEAF_SIZE is the minimum visible node size — fixed constant.
// Parent size = LEAF_SIZE × (1 + sqrt(n) × PACKING_K) / 0.85
// Orbit radius = LEAF_SIZE × sqrt(n) × PACKING_K
//
// This guarantees:
// - Leaf nodes are always the same visible size
// - Parents are always larger than children
// - No overlaps (golden-angle sunflower with PACKING_K=1.3)
// - Children fit inside parent ring

export const LEAF_SIZE = 1.5;
export const PACKING_K = 1.3;

export function parentSizeForChildren(childSize: number, childCount: number): number {
  const orbitRadius = childSize * Math.sqrt(childCount) * PACKING_K;
  return (orbitRadius + childSize) / 0.85;
}

export function layoutChildren(parentSize: number, childCount: number) {
  const ringInner = parentSize * 0.85;
  const childSize = ringInner / (1 + Math.sqrt(childCount) * PACKING_K);
  const orbitRadius = childSize * Math.sqrt(childCount) * PACKING_K;
  const goldenAngle = 137.508 * Math.PI / 180;

  const positions: { x: number; y: number; size: number }[] = [];
  for (let i = 0; i < childCount; i++) {
    const angle = i * goldenAngle;
    const r = orbitRadius * Math.sqrt((i + 0.5) / childCount);
    positions.push({ x: r * Math.cos(angle), y: r * Math.sin(angle), size: childSize });
  }
  return positions;
}

describe('bottom-up sizing', () => {
  it('parent grows to fit children, not children shrink to fit parent', () => {
    const needed = parentSizeForChildren(LEAF_SIZE, 10);
    expect(needed).toBeGreaterThan(LEAF_SIZE * 3);
    const layout = layoutChildren(needed, 10);
    expect(layout[0].size).toBeCloseTo(LEAF_SIZE, 0);
  });

  it('more children → larger parent', () => {
    const p10 = parentSizeForChildren(LEAF_SIZE, 10);
    const p50 = parentSizeForChildren(LEAF_SIZE, 50);
    expect(p50).toBeGreaterThan(p10);
  });

  it('1 child → parent slightly larger than child', () => {
    const p = parentSizeForChildren(LEAF_SIZE, 1);
    expect(p).toBeGreaterThan(LEAF_SIZE);
    expect(p).toBeLessThan(LEAF_SIZE * 5);
  });

  it('leaf size is constant across all nesting depths', () => {
    // Scope → kind-groups (12) → artifacts (30)
    const kgSize = parentSizeForChildren(LEAF_SIZE, 30);
    const scopeSize = parentSizeForChildren(kgSize, 12);

    const level1 = layoutChildren(scopeSize, 12);
    expect(level1[0].size).toBeCloseTo(kgSize, 1);

    const level2 = layoutChildren(kgSize, 30);
    expect(level2[0].size).toBeCloseTo(LEAF_SIZE, 0);
  });
});

describe('containment: children fit inside parent ring', () => {
  for (const [childCount, label] of [[5, '5'], [10, '10'], [30, '30'], [50, '50'], [100, '100']] as [number, string][]) {
    it(`${label} children stay inside parent`, () => {
      const ps = parentSizeForChildren(LEAF_SIZE, childCount);
      const layout = layoutChildren(ps, childCount);
      for (const p of layout) {
        expect(Math.hypot(p.x, p.y) + p.size).toBeLessThanOrEqual(ps * 1.01);
      }
    });
  }

  it('deeply nested: scope → kind-group → artifact all fit', () => {
    const artSize = LEAF_SIZE;
    const kgSize = parentSizeForChildren(artSize, 30);
    const scopeSize = parentSizeForChildren(kgSize, 12);

    const level1 = layoutChildren(scopeSize, 12);
    for (const p of level1) {
      expect(Math.hypot(p.x, p.y) + p.size).toBeLessThanOrEqual(scopeSize * 1.01);
    }

    const level2 = layoutChildren(kgSize, 30);
    for (const p of level2) {
      expect(Math.hypot(p.x, p.y) + p.size).toBeLessThanOrEqual(kgSize * 1.01);
    }
    expect(level2[0].size).toBeGreaterThanOrEqual(LEAF_SIZE * 0.95);
  });
});

describe('no-overlap constraint', () => {
  function countOverlaps(positions: { x: number; y: number; size: number }[]): { count: number; worst: number } {
    let count = 0;
    let worst = 0;
    for (let i = 0; i < positions.length; i++) {
      for (let j = i + 1; j < positions.length; j++) {
        const dx = positions[i].x - positions[j].x;
        const dy = positions[i].y - positions[j].y;
        const dist = Math.hypot(dx, dy);
        const minDist = positions[i].size + positions[j].size;
        if (dist < minDist) {
          count++;
          worst = Math.max(worst, minDist - dist);
        }
      }
    }
    return { count, worst };
  }

  for (const [parentSize, childCount] of [[18, 10], [18, 15], [12, 8], [8, 5], [18, 30]]) {
    it(`parent=${parentSize} children=${childCount} — no overlaps`, () => {
      const positions = layoutChildren(parentSize, childCount);
      const { count, worst } = countOverlaps(positions);
      expect(count).toBe(0);
      if (count > 0) {
        console.log(`  ${count} overlaps, worst=${worst.toFixed(2)}, childSize=${positions[0]?.size.toFixed(2)}`);
      }
    });
  }

  for (const [parentSize, childCount] of [[18, 10], [18, 15], [18, 30]]) {
    it(`parent=${parentSize} children=${childCount} — all children inside parent`, () => {
      const positions = layoutChildren(parentSize, childCount);
      for (const p of positions) {
        const distFromCenter = Math.hypot(p.x, p.y);
        expect(distFromCenter + p.size).toBeLessThanOrEqual(parentSize * 1.05);
      }
    });
  }
});
