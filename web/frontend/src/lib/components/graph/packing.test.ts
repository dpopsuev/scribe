import { describe, it, expect } from 'vitest';

function molecularChildSize(parentSize: number, childCount: number): number {
  const ringInner = parentSize * 0.85;
  const packingK = 1.3;
  return Math.max(0.5, ringInner / (1 + Math.sqrt(childCount) * packingK));
}

// Golden-angle sunflower layout with two constraints:
// 1. Children don't overlap each other
// 2. Children fit inside the parent ring
//
// Sunflower packing: the outermost child is at r = R (orbit radius).
// For non-overlap, consecutive children at radius r need angular
// spacing > 2*childSize/r. The golden angle provides optimal spacing
// when R = childSize * sqrt(n) * k (where k ≈ 1.13 for golden angle).
//
// For containment: R + childSize <= parentSize (ring inner edge ≈ 0.88 * parentSize)
export function layoutChildren(parentSize: number, childCount: number) {
  const ringInner = parentSize * 0.85;
  // Sunflower packing constant: for golden-angle spirals, R ≈ childSize * sqrt(n) * 1.13
  // Solve for childSize: childSize * (1 + sqrt(n) * 1.13) <= ringInner
  const packingK = 1.3;
  const childSize = Math.max(0.5, ringInner / (1 + Math.sqrt(childCount) * packingK));
  const orbitRadius = childSize * Math.sqrt(childCount) * packingK;
  const goldenAngle = 137.508 * Math.PI / 180;

  const positions: { x: number; y: number; size: number }[] = [];
  for (let i = 0; i < childCount; i++) {
    const angle = i * goldenAngle;
    const r = orbitRadius * Math.sqrt((i + 0.5) / childCount);
    positions.push({ x: r * Math.cos(angle), y: r * Math.sin(angle), size: childSize });
  }
  return positions;
}

describe('molecular packing', () => {
  it('10 children in size-18 parent — each smaller than parent', () => {
    const cs = molecularChildSize(18, 10);
    expect(cs).toBeGreaterThan(1);
    expect(cs).toBeLessThan(18);
  });

  it('50 children → smaller than 10 children', () => {
    const cs10 = molecularChildSize(18, 10);
    const cs50 = molecularChildSize(18, 50);
    expect(cs50).toBeLessThan(cs10);
  });

  it('1 child → large but inside parent', () => {
    const cs = molecularChildSize(18, 1);
    expect(cs).toBeGreaterThan(5);
    expect(cs).toBeLessThan(18);
  });

  it('100 children → each is tiny but above minimum', () => {
    const cs = molecularChildSize(18, 100);
    expect(cs).toBeGreaterThanOrEqual(0.5);
    expect(cs).toBeLessThan(3);
  });

  it('small parent with many children clamps to minimum', () => {
    const cs = molecularChildSize(2, 50);
    expect(cs).toBe(0.5);
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
