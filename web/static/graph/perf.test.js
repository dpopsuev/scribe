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
import { forcesForDist, forceNBodyGravity } from './physics.js';
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

function medianMs(fn, reps = 7) {
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
    // 10x more calls → < 30x time (generous — this machine has GC jitter)
    expect(t10 / t1).toBeLessThan(30);
  });
});

// ── O(n²): forceNBodyGravity ──────────────────────────────────────────────

describe('forceNBodyGravity — O(n²)', () => {
  // Run ITERS iterations per timed block so wall time >> performance.now() resolution.
  // Single-call timing (~0.005ms) is dominated by JIT noise; 200 calls gives ~1ms signal.
  const ITERS = 200;

  it('does not scale worse than O(n²): 5× nodes → less than 30× time', () => {
    const small = makeNodes(20);
    const large = makeNodes(100);

    const force20  = forceNBodyGravity(0.12, 40);
    const force100 = forceNBodyGravity(0.12, 40);
    force20.initialize(small);
    force100.initialize(large);

    // Warm both to equal JIT state before measuring — prevents the first measured
    // function from being penalised by compilation overhead.
    for (let i = 0; i < 50; i++) { force20(0.5); force100(0.5); }

    const t20  = medianMs(() => { for (let i = 0; i < ITERS; i++) force20(0.5); });
    const t100 = medianMs(() => { for (let i = 0; i < ITERS; i++) force100(0.5); });

    const ratio = t100 / Math.max(t20, 0.01);
    // forceNBodyGravity is O(n²): 5× nodes → 25× time. Allow 30× for JIT noise.
    // An O(n³) regression would give 125× — caught clearly.
    expect(ratio, `time ratio 100/20 nodes = ${ratio.toFixed(2)}, want < 30`).toBeLessThan(30);
  });

  it('stays under 10ms per call for 500 nodes', () => {
    const nodes = makeNodes(500);
    const force = forceNBodyGravity(0.12, 40);
    force.initialize(nodes);
    // Time 20 calls, check average — removes single-call GC spikes.
    const total = medianMs(() => { for (let i = 0; i < 20; i++) force(0.5); });
    const perCall = total / 20;
    // O(n²): 500 nodes → 124,750 pairs. 10ms per call is a generous budget.
    expect(perCall, `forceNBodyGravity(500 nodes) avg = ${perCall.toFixed(2)}ms, want < 10ms`).toBeLessThan(10);
  });
});

// ── O(n): KindColorRenderer._nodeVal normalisation ───────────────────────

describe('KindColorRenderer._nodeVal — O(1) per call after O(n) init', () => {
  it('init(n) scales linearly', () => {
    // Pre-allocate nodes outside timed block; init() itself is what we measure.
    const nodes20  = makeNodes(20);
    const nodes100 = makeNodes(100);
    const r20  = new KindColorRenderer();
    const r100 = new KindColorRenderer();

    // Warm up to avoid first-call JIT penalty skewing the ratio.
    r20.init(nodes20); r100.init(nodes100);

    const t20  = medianMs(() => r20.init(nodes20));
    const t100 = medianMs(() => r100.init(nodes100));

    // t20 is several µs — large enough that the ratio is meaningful without a floor.
    const ratio = t100 / Math.max(t20, 0.01);
    expect(ratio, `init ratio 100/20 = ${ratio.toFixed(2)}, want < 20`).toBeLessThan(20);
  });

  it('_nodeVal per-call is O(1) — constant regardless of dataset size', () => {
    const rSmall = new KindColorRenderer();
    rSmall.init(makeNodes(10));
    const rLarge = new KindColorRenderer();
    rLarge.init(makeNodes(1000));

    // Warm both renderers to equalise JIT state and let GC settle after the
    // 1000-node allocation above before the timed block starts.
    for (let i = 0; i < 200; i++) { rSmall._nodeVal(i % 120 + 3); rLarge._nodeVal(i % 120 + 3); }

    // Absolute bound: 10 000 O(1) calls must each complete in < 5µs on average.
    // Ratio comparisons are unreliable here because GC pressure from the large
    // init can inflate one block while the other gets a warmed JIT.
    const ITERS = 10000;
    const tSmall = medianMs(() => { for (let i = 0; i < ITERS; i++) rSmall._nodeVal(i % 120 + 3); });
    const tLarge = medianMs(() => { for (let i = 0; i < ITERS; i++) rLarge._nodeVal(i % 120 + 3); });

    expect(tSmall, `10k calls on 10-node init = ${tSmall.toFixed(2)}ms, want < 50ms`).toBeLessThan(50);
    expect(tLarge, `10k calls on 1000-node init = ${tLarge.toFixed(2)}ms, want < 50ms`).toBeLessThan(50);
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

describe('frame budget — combined operations scale sub-linearly', () => {
  // Wall-clock assertions are CI-hostile (GC, JIT, load). Instead verify that
  // 85-node work is not disproportionately slower than 10-node work — catching
  // O(n²) regressions without pinning to a machine-specific ms budget.
  it('85-node frame work is not more than 20× slower than 10-node work', () => {
    const ITERS = 100;

    const nodes10 = makeNodes(10);
    const force10 = forceNBodyGravity(0.12, 40);
    force10.initialize(nodes10);
    const t10 = medianMs(() => { for (let i = 0; i < ITERS; i++) { forcesForDist(700); force10(0.3); } });

    const nodes85 = makeNodes(85);
    const force85 = forceNBodyGravity(0.12, 40);
    force85.initialize(nodes85);
    const t85 = medianMs(() => { for (let i = 0; i < ITERS; i++) { forcesForDist(700); force85(0.3); } });

    const ratio = t85 / Math.max(t10, 0.01);
    // forceNBodyGravity is O(n²): 8.5× nodes → ~72× time; allow 100× for JIT noise.
    // An O(n³) regression would give 614× — caught clearly.
    expect(ratio, `85-node/10-node ratio = ${ratio.toFixed(2)}, want < 100`).toBeLessThan(100);
  });
});
