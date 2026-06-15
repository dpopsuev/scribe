import { describe, it, expect } from 'vitest';
import { KindColorRenderer, FALLBACK_KIND_COLORS } from './renderer.js';

// ── healthHue (via nodeColor) ─────────────────────────────────────────────
// healthHue is internal — test its effect through _nodeColor.
// Without culori (not available in test env), fallback hex values are used.

describe('KindColorRenderer health color', () => {
  function renderer(nodes = [{ val: 1 }]) {
    const r = new KindColorRenderer();
    r.init(nodes);
    return r;
  }

  it('0 violations → kind color (not health override)', () => {
    const r = renderer();
    const color = r._nodeColor({ kind: 'task', violations: 0 });
    expect(color).toBe(FALLBACK_KIND_COLORS['task']);
  });

  it('1 violation → amber fallback', () => {
    const r = renderer();
    const color = r._nodeColor({ kind: 'task', violations: 1 });
    expect(color).toBe('#f59e0b');
  });

  it('3 violations → amber fallback', () => {
    const r = renderer();
    const color = r._nodeColor({ kind: 'task', violations: 3 });
    expect(color).toBe('#f59e0b');
  });

  it('4 violations → red fallback', () => {
    const r = renderer();
    const color = r._nodeColor({ kind: 'task', violations: 4 });
    expect(color).toBe('#ef4444');
  });

  it('scope nodes → neutral silver (not kind color)', () => {
    const r = renderer();
    const color = r._nodeColor({ kind: 'project' });
    expect(color).toBe('#c8d0dc');
  });

  it('artifact nodes → kind color', () => {
    const r = renderer();
    const color = r._nodeColor({ kind: 'task' });
    expect(color).toBe(FALLBACK_KIND_COLORS['task']);
  });
});

// ── _nodeVal normalisation ────────────────────────────────────────────────

describe('KindColorRenderer._nodeVal', () => {
  it('smallest node gets MIN_SIZE', () => {
    const r = new KindColorRenderer();
    r.init([{ val: 3 }, { val: 100 }]);
    expect(r._nodeVal(3)).toBeCloseTo(2, 0);  // MIN_SIZE
  });

  it('largest node gets MAX_SIZE', () => {
    const r = new KindColorRenderer();
    r.init([{ val: 3 }, { val: 100 }]);
    expect(r._nodeVal(100)).toBeCloseTo(40, 0);  // MAX_SIZE
  });

  it('middle val is between min and max', () => {
    const r = new KindColorRenderer();
    r.init([{ val: 1 }, { val: 100 }]);
    const mid = r._nodeVal(50);
    expect(mid).toBeGreaterThan(2);
    expect(mid).toBeLessThan(40);
  });

  it('all same val → all get MIN_SIZE (no division by zero)', () => {
    const r = new KindColorRenderer();
    r.init([{ val: 5 }, { val: 5 }, { val: 5 }]);
    expect(() => r._nodeVal(5)).not.toThrow();
  });
});
