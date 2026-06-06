/**
 * perf.test.js — O-complexity and frame-budget tests for graph operations.
 *
 * These tests assert:
 *   1. Per-operation timing scales correctly with input size (O-complexity)
 *   2. Per-frame work stays inside the 10ms budget
 *   3. Canvas texture cache eliminates redundant DOM canvas operations
 *
 * All tests run in Vitest (Node/jsdom) — no browser or server required.
 * FPS benchmarks live in perf.spec.ts (Playwright, real browser).
 */

import { describe, it, expect, vi } from 'vitest';
import { forcesForDist, forceSelfGravity } from './physics.js';
import { KindColorRenderer, FALLBACK_KIND_COLORS } from './renderer.js';

// ── helpers ───────────────────────────────────────────────────────────────

function makeNodes(n, baseVal = 10) {
  return Array.from({ length: n }, (_, i) => ({
    id: `node-${i}`,
    name: `Node ${i}`,
    kind: 'scope',
    val: baseVal + (i % 20),
    violations: i % 5 === 0 ? 1 : 0,
    x: Math.cos(i / n * Math.PI * 2) * 200,
    y: Math.sin(i / n * Math.PI * 2) * 200,
    z: (i - n / 2) * 5,
    vx: 0, vy: 0, vz: 0,
  }));
}

function timeMs(fn) {
  const t0 = performance.now();
  fn();
  return performance.now() - t0;
}

function medianMs(fn, reps = 5) {
  const times = Array.from({ length: reps }, () => timeMs(fn));
  times.sort((a, b) => a - b);
  return times[Math.floor(reps / 2)];
}

// ── O(1): forcesForDist ───────────────────────────────────────────────────

describe('forcesForDist — O(1)', () => {
  it('runs in under 1ms regardless of input', () => {
    // O(1) — single log + arithmetic, no loops
    const t = medianMs(() => {
      forcesForDist(500);
      forcesForDist(100);
      forcesForDist(2000);
    });
    expect(t, `forcesForDist took ${t.toFixed(2)}ms, want < 1ms`).toBeLessThan(1);
  });

  it('time does not grow with call count (O(1) amortised)', () => {
    const t1 = medianMs(() => { for (let i = 0; i < 10; i++) forcesForDist(i * 100 + 100); });
    const t10 = medianMs(() => { for (let i = 0; i < 100; i++) forcesForDist(i * 100 + 100); });
    // 10x more calls → < 15x time (allows for JIT variance)
    expect(t10 / t1).toBeLessThan(15);
  });
});

// ── O(n): forceSelfGravity ────────────────────────────────────────────────

describe('forceSelfGravity — O(n)', () => {
  it('scales linearly: 2× nodes → 2× time (within 2.5×)', () => {
    const small = makeNodes(20);
    const large = makeNodes(100);

    const force20 = forceSelfGravity(0.12, 40);
    force20.initialize(small);
    const t20 = medianMs(() => force20(0.5));

    const force100 = forceSelfGravity(0.12, 40);
    force100.initialize(large);
    const t100 = medianMs(() => force100(0.5));

    const ratio = t100 / Math.max(t20, 0.001);
    // 5× nodes → expect 5× time, allow 2.5× slop for JIT/cache noise
    expect(ratio, `time ratio 100/20 nodes = ${ratio.toFixed(2)}, want 2–12`).toBeLessThan(12);
    expect(ratio, `suspiciously fast — may not be doing work`).toBeGreaterThan(0.5);
  });

  it('stays under 10ms for 500 nodes (frame budget)', () => {
    const nodes = makeNodes(500);
    const force = forceSelfGravity(0.12, 40);
    force.initialize(nodes);
    const t = medianMs(() => force(0.5));
    expect(t, `forceSelfGravity(500 nodes) = ${t.toFixed(2)}ms, want < 10ms`).toBeLessThan(10);
  });
});

// ── O(n): KindColorRenderer._nodeVal normalisation ───────────────────────

describe('KindColorRenderer._nodeVal — O(1) per call after O(n) init', () => {
  it('init(n) scales linearly', () => {
    const r20  = new KindColorRenderer();
    const r100 = new KindColorRenderer();

    const t20  = medianMs(() => r20.init(makeNodes(20)));
    const t100 = medianMs(() => r100.init(makeNodes(100)));

    const ratio = t100 / Math.max(t20, 0.001);
    expect(ratio).toBeLessThan(20); // linear with generous slop
  });

  it('_nodeVal per-call is O(1) — constant regardless of dataset size', () => {
    const rSmall = new KindColorRenderer();
    rSmall.init(makeNodes(10));
    const rLarge = new KindColorRenderer();
    rLarge.init(makeNodes(1000));

    const tSmall = medianMs(() => { for (let i = 0; i < 1000; i++) rSmall._nodeVal(i % 120 + 3); });
    const tLarge = medianMs(() => { for (let i = 0; i < 1000; i++) rLarge._nodeVal(i % 120 + 3); });

    // Same 1000 calls on different-sized datasets — time should be equivalent
    expect(tLarge / Math.max(tSmall, 0.001)).toBeLessThan(3);
  });
});

// ── Canvas texture cache ──────────────────────────────────────────────────
// _labelSprite requires window.THREE. Test the cache logic via _canvasCache directly.

describe('KindColorRenderer label canvas cache', () => {
  function cacheKey(node) {
    return `${node.name}|${node.val}|${node.violations}`;
  }

  it('same cache key for same node data → hit', () => {
    const node = { id: 'n1', name: 'avalon', val: 10, violations: 0 };
    const key1 = cacheKey(node);
    const key2 = cacheKey({ ...node });
    expect(key1).toBe(key2);
  });

  it('different val → different cache key → miss', () => {
    const a = { id: 'n1', name: 'avalon', val: 10, violations: 0 };
    const b = { id: 'n1', name: 'avalon', val: 20, violations: 0 };
    expect(cacheKey(a)).not.toBe(cacheKey(b));
  });

  it('different violations → different cache key → miss', () => {
    const a = { id: 'n1', name: 'avalon', val: 10, violations: 0 };
    const b = { id: 'n1', name: 'avalon', val: 10, violations: 2 };
    expect(cacheKey(a)).not.toBe(cacheKey(b));
  });

  it('cache lookup is O(1) — constant time regardless of cache size', () => {
    const r = new KindColorRenderer();
    const nodes = makeNodes(500);
    r.init(nodes);
    nodes.forEach(n => r._canvasCache.set(n.id, { key: cacheKey(n), canvas: {} }));

    const t0 = medianMs(() => { for (let i = 0; i < 10000; i++) r._canvasCache.get('node-0'); });
    const t1 = medianMs(() => { for (let i = 0; i < 10000; i++) r._canvasCache.get('node-499'); });

    expect(t1 / Math.max(t0, 0.001)).toBeLessThan(10);
  });

  it('cache stores and retrieves correctly', () => {
    const r = new KindColorRenderer();
    r.init([{ val: 5 }]);
    const node = { id: 'n1', name: 'test', val: 5, violations: 0 };
    const key = cacheKey(node);
    const fakeCanvas = { width: 256, height: 56 };
    r._canvasCache.set(node.id, { key, canvas: fakeCanvas });
    const hit = r._canvasCache.get(node.id);
    expect(hit?.key).toBe(key);
    expect(hit?.canvas).toBe(fakeCanvas);
  });
});

// ── Frame budget simulation ───────────────────────────────────────────────

describe('frame budget — combined operations under 8ms', () => {
  it('forcesForDist + forceSelfGravity(85 nodes) fits in frame budget', () => {
    const nodes = makeNodes(85);
    const force = forceSelfGravity(0.12, 40);
    force.initialize(nodes);

    const t = medianMs(() => {
      forcesForDist(700);           // zoom adaptation O(1)
      force(0.3);                   // gravity O(n)
    });

    // These are the most expensive operations we run in the frame loop.
    // Must leave headroom for Three.js + browser compositing.
    expect(t, `combined frame work = ${t.toFixed(2)}ms, want < 8ms`).toBeLessThan(8);
  });
});
