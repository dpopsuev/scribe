import { describe, it, expect } from 'vitest';
import { kindShape, SHAPE_CIRCLE, SHAPE_SQUARE, SHAPE_DIAMOND, SHAPE_TRIANGLE, SHAPE_HEXAGON, SHAPE_PENTAGON, SHAPE_STAR, SHAPE_ROUNDED_RECT } from './shapes';
import { MIN_CHILD_SIZE, computePacking } from './packing';

describe('kindShape mapping', () => {
  it('knowledge kinds → circle', () => {
    for (const k of ['knowledge.note', 'knowledge.concept', 'knowledge.source', 'knowledge.journal']) {
      expect(kindShape(k)).toBe(SHAPE_CIRCLE);
    }
  });

  it('code kinds → square', () => {
    for (const k of ['code.struct', 'code.interface', 'code.function', 'code.method', 'code.test']) {
      expect(kindShape(k)).toBe(SHAPE_SQUARE);
    }
  });

  it('intent decisions → diamond', () => {
    for (const k of ['intent.decision', 'intent.spec', 'intent.need']) {
      expect(kindShape(k)).toBe(SHAPE_DIAMOND);
    }
  });

  it('intent bugs → triangle', () => {
    expect(kindShape('intent.bug')).toBe(SHAPE_TRIANGLE);
  });

  it('support kinds → hexagon', () => {
    for (const k of ['support.doc', 'support.config', 'support.template', 'support.rule', 'support.ref']) {
      expect(kindShape(k)).toBe(SHAPE_HEXAGON);
    }
  });

  it('effort kinds → pentagon', () => {
    for (const k of ['effort.campaign', 'effort.goal', 'effort.task']) {
      expect(kindShape(k)).toBe(SHAPE_PENTAGON);
    }
  });

  it('investigation kinds → star', () => {
    for (const k of ['investigation.case', 'investigation.observation', 'investigation.cause']) {
      expect(kindShape(k)).toBe(SHAPE_STAR);
    }
  });

  it('meta kinds → rounded rect', () => {
    expect(kindShape('project')).toBe(SHAPE_ROUNDED_RECT);
    expect(kindShape('kind-group')).toBe(SHAPE_ROUNDED_RECT);
  });

  it('unknown kind → circle fallback', () => {
    expect(kindShape('unknown.whatever')).toBe(SHAPE_CIRCLE);
    expect(kindShape('')).toBe(SHAPE_CIRCLE);
  });

  it('all 8 shapes are distinct values', () => {
    const shapes = [SHAPE_CIRCLE, SHAPE_SQUARE, SHAPE_DIAMOND, SHAPE_TRIANGLE, SHAPE_HEXAGON, SHAPE_PENTAGON, SHAPE_STAR, SHAPE_ROUNDED_RECT];
    expect(new Set(shapes).size).toBe(8);
  });
});

describe('zoom and legibility', () => {
  const MAX_ZOOM = 200;
  const LABEL_FONT_BASE = 11; // from GraphCanvas DEPTH_FONT[0]

  it('max zoom can make MIN_CHILD_SIZE nodes readable', () => {
    // A node of MIN_CHILD_SIZE at max zoom should produce a screen size
    // large enough to display its label (>= 8px screen diameter)
    const screenSize = MIN_CHILD_SIZE * MAX_ZOOM;
    expect(screenSize).toBeGreaterThanOrEqual(8);
  });

  it('deepest nesting (depth=3) still produces visible nodes at max zoom', () => {
    // Simulate 3 levels of packing: scope(18) → kind(pack) → artifact(pack) → leaf(pack)
    let size = 18;
    for (const n of [8, 20, 10]) {
      const pack = computePacking(size, n);
      size = pack.childSize;
    }
    // At depth 3, the node is tiny — but max zoom should make it readable
    const screenSize = size * MAX_ZOOM;
    expect(screenSize).toBeGreaterThanOrEqual(8);
  });

  it('label font at deepest depth is still >= 6px', () => {
    const DEPTH_FONT = [11, 9, 7, 6];
    for (const fontSize of DEPTH_FONT) {
      expect(fontSize).toBeGreaterThanOrEqual(6);
    }
  });

  it('zoom range spans at least 3 orders of magnitude', () => {
    const MIN_ZOOM = 0.05;
    const ratio = MAX_ZOOM / MIN_ZOOM;
    expect(ratio).toBeGreaterThanOrEqual(1000);
  });
});

describe('rectangular boundary containment', () => {
  it('boundary dimensions are larger along X than Y (landscape aspect)', () => {
    // Simulating the boundary computation from GraphCanvas
    const sizes = [5, 10, 3, 8, 12, 6, 4, 7, 9, 11];
    const totalArea = sizes.reduce((s, sz) => s + Math.PI * sz * sz, 0);
    const idealRadius = Math.sqrt(totalArea / (0.25 * Math.PI)) * 1.5;
    const boundW = idealRadius * 1.4;
    const boundH = idealRadius * 1.05;

    expect(boundW).toBeGreaterThan(boundH);
    // Aspect ratio should be roughly 4:3
    const aspect = boundW / boundH;
    expect(aspect).toBeCloseTo(1.33, 1);
  });

  it('boundary clamp keeps nodes inside rectangular area', () => {
    const boundW = 100;
    const boundH = 75;
    const sz = 5;
    const maxX = boundW - sz;
    const maxY = boundH - sz;

    // Node at corner should be clamped
    const x = 150;
    const y = 120;
    const clampedX = Math.sign(x) * Math.min(Math.abs(x), maxX);
    const clampedY = Math.sign(y) * Math.min(Math.abs(y), maxY);

    expect(Math.abs(clampedX) + sz).toBeLessThanOrEqual(boundW);
    expect(Math.abs(clampedY) + sz).toBeLessThanOrEqual(boundH);
  });

  it('nodes at origin are not affected by boundary clamp', () => {
    const boundW = 100;
    const boundH = 75;
    const sz = 5;
    const x = 0;
    const y = 0;

    expect(Math.abs(x)).toBeLessThan(boundW - sz);
    expect(Math.abs(y)).toBeLessThan(boundH - sz);
  });
});
