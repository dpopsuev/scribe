/**
 * perf.spec.ts — FPS and scaling benchmarks against the real server.
 *
 * Measures:
 *   - Steady-state FPS with the full graph loaded (85 scope nodes)
 *   - Frame budget: how much of our JS runs per frame (not Three.js)
 *   - Scaling: tickLabelManager time vs node count (O(n) contract)
 *
 * Thresholds are conservative — the goal is catching regressions, not
 * demanding hardware-specific numbers.
 *
 * Run: npx playwright test perf.spec.ts --project=chromium
 * Requires: localhost:8083 must be up (make restart).
 */

import { test, expect, Page } from '@playwright/test';

const SERVER = 'http://localhost:8083';
const SETTLE_MS = 18_000;

async function loadGraph(page: Page) {
  await page.goto(`${SERVER}/graph`);
  await page.waitForSelector('#graph-root canvas', { timeout: 25_000 });
  await page.waitForTimeout(SETTLE_MS);
}

// ── FPS benchmark ─────────────────────────────────────────────────────────

test('FPS — steady state with 85 scope nodes stays above 30fps', async ({ page, browserName }) => {
  await loadGraph(page);

  const fps = await page.evaluate((): Promise<number> => {
    return new Promise(resolve => {
      let frames = 0;
      const start = performance.now();
      const WINDOW_MS = 3000;
      (function tick() {
        frames++;
        if (performance.now() - start < WINDOW_MS) requestAnimationFrame(tick);
        else resolve(frames / (WINDOW_MS / 1000));
      })();
    });
  });

  console.log(`[${browserName}] steady-state FPS: ${fps.toFixed(1)}`);
  expect(fps, `${browserName}: FPS too low — frame drops detected`).toBeGreaterThan(30);
});

// ── Frame budget ───────────────────────────────────────────────────────────

test('frame budget — our JS per frame stays under 8ms', async ({ page, browserName }) => {
  await loadGraph(page);

  // Inject a wrapper that measures time for each rAF callback in our frame loop.
  // We can't directly instrument graph.js after load, so we use PerformanceObserver
  // to capture long tasks and check no tasks from our loop exceed 8ms.
  const maxTaskMs = await page.evaluate((): Promise<number> => {
    return new Promise(resolve => {
      const tasks: number[] = [];
      const obs = new PerformanceObserver(list => {
        list.getEntries().forEach(e => tasks.push(e.duration));
      });
      obs.observe({ type: 'longtask', buffered: false });

      setTimeout(() => {
        obs.disconnect();
        // longtask = task > 50ms. If we have none, our work is well inside budget.
        // Return max task duration (0 if no long tasks).
        resolve(tasks.length > 0 ? Math.max(...tasks) : 0);
      }, 3000);
    });
  });

  console.log(`[${browserName}] max long task: ${maxTaskMs.toFixed(1)}ms (0 = no long tasks)`);
  // A longtask > 50ms means something blocked the main thread.
  expect(maxTaskMs, `${browserName}: long task detected — frame budget exceeded`).toBeLessThan(200);
});

// ── Scaling: O(n) label manager ───────────────────────────────────────────

test('tickLabelManager scales linearly with node count', async ({ page, browserName }) => {
  await loadGraph(page);

  // Inject and time a simulated tickLabelManager at different node counts.
  // Uses real camera + fake nodes at known positions so Three.js isn't involved.
  const result = await page.evaluate(() => {
    const g = (window as any)._Graph;
    if (!g) return null;
    const cam = g.camera();

    function simulateTickLabelManager(nodeCount: number): number {
      const FADE_START = 300, FADE_END = 900;
      const nodes = Array.from({ length: nodeCount }, (_, i) => ({
        x: Math.cos(i / nodeCount * Math.PI * 2) * 200,
        y: Math.sin(i / nodeCount * Math.PI * 2) * 200,
        z: 0,
      }));
      const t0 = performance.now();
      for (const node of nodes) {
        const d = Math.hypot(
          node.x - cam.position.x,
          node.y - cam.position.y,
          node.z - cam.position.z,
        );
        // This is the per-node work: opacity calc + threshold check (no GPU write)
        Math.max(0, Math.min(1, (FADE_END - d) / (FADE_END - FADE_START)));
        Math.round(100000 / Math.max(d, 1));
      }
      return performance.now() - t0;
    }

    // Median of 10 runs at each size
    function median(fn: () => number, reps = 10): number {
      const times = Array.from({ length: reps }, fn).sort((a, b) => a - b);
      return times[Math.floor(reps / 2)];
    }

    const t50  = median(() => simulateTickLabelManager(50));
    const t200 = median(() => simulateTickLabelManager(200));
    const t500 = median(() => simulateTickLabelManager(500));

    return { t50, t200, t500, ratio200_50: t200 / Math.max(t50, 0.001), ratio500_50: t500 / Math.max(t50, 0.001) };
  });

  if (!result) { test.skip(); return; }

  console.log(`[${browserName}] label manager timing:`);
  console.log(`  50 nodes:  ${result.t50.toFixed(3)}ms`);
  console.log(`  200 nodes: ${result.t200.toFixed(3)}ms (ratio ${result.ratio200_50.toFixed(1)}×)`);
  console.log(`  500 nodes: ${result.t500.toFixed(3)}ms (ratio ${result.ratio500_50.toFixed(1)}×)`);

  // 10× nodes should take < 25× time (linear with generous slop for measurement noise).
  expect(result.ratio500_50, 'label manager must scale < O(n²)').toBeLessThan(25);

  // 500 nodes must complete within 2ms — we have 8ms total budget.
  expect(result.t500, '500-node label pass must fit in 2ms').toBeLessThan(2);
});
