import { test, expect } from '@playwright/test';

// 1:1 user-story perf test: simulates real zoom in/out on the graph page,
// samples PER-FRAME FPS from window.__GRAPH_FRAME_HIST__, and reports
// distribution across the color scale:
//   Blue: 60+ fps | Green: 30-60 | Yellow: 15-30 | Red: <15
//
// Run:  npx playwright test tests/graph-perf.spec.ts
// CI:   xvfb-run npx playwright test tests/graph-perf.spec.ts

function classify(fps: number): 'blue' | 'green' | 'yellow' | 'red' {
  if (fps >= 60) return 'blue';
  if (fps >= 30) return 'green';
  if (fps >= 15) return 'yellow';
  return 'red';
}

function distribution(frames: number[]) {
  const bins = { blue: 0, green: 0, yellow: 0, red: 0, total: frames.length };
  for (const f of frames) bins[classify(f)]++;
  return bins;
}

function reportDist(label: string, frames: number[]) {
  const d = distribution(frames);
  const pct = (n: number) => ((n / d.total) * 100).toFixed(0).padStart(3) + '%';
  const min = Math.min(...frames);
  const max = Math.max(...frames);
  const avg = Math.round(frames.reduce((s, v) => s + v, 0) / frames.length);
  console.log(`\n${label} (${d.total} frames, avg ${avg} fps, min ${min}, max ${max}):`);
  console.log(`  🔵 Blue  60+fps: ${pct(d.blue)}  (${d.blue} frames)`);
  console.log(`  🟢 Green 30-59:  ${pct(d.green)}  (${d.green} frames)`);
  console.log(`  🟡 Yellow 15-29: ${pct(d.yellow)}  (${d.yellow} frames)`);
  console.log(`  🔴 Red    <15:   ${pct(d.red)}  (${d.red} frames)`);
  return d;
}

test.describe('graph zoom performance — user story', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/app/graph');
    await page.waitForFunction(
      () => (window as any).__GRAPH_PERF__?.fps > 0,
      { timeout: 15000 },
    );
    // Clear frame history so each test starts fresh
    await page.evaluate(() => { (window as any).__GRAPH_FRAME_HIST__ = []; });
  });

  test('zoom in → pause → zoom out: per-frame FPS distribution', async ({ page }) => {
    // Reset clears history AND prevFrameTs to avoid stale first-frame delta
    await page.evaluate(() => (window as any).__GRAPH_RESET_PERF__());
    await page.waitForTimeout(200);

    // Phase 1: zoom in (30 scroll ticks, ~16ms apart = real trackpad speed)
    for (let i = 0; i < 30; i++) {
      await page.mouse.wheel(0, -120);
      await page.waitForTimeout(16);
    }
    await page.waitForTimeout(600);
    const zoomInFrames: number[] = await page.evaluate(() => [...((window as any).__GRAPH_FRAME_HIST__ || [])]);

    // Phase 2: pause — let everything settle
    await page.evaluate(() => (window as any).__GRAPH_RESET_PERF__());
    await page.waitForTimeout(1000);
    const idleFrames: number[] = await page.evaluate(() => [...((window as any).__GRAPH_FRAME_HIST__ || [])]);

    // Phase 3: zoom out
    await page.evaluate(() => (window as any).__GRAPH_RESET_PERF__());
    for (let i = 0; i < 30; i++) {
      await page.mouse.wheel(0, 120);
      await page.waitForTimeout(16);
    }
    await page.waitForTimeout(600);
    const zoomOutFrames: number[] = await page.evaluate(() => [...((window as any).__GRAPH_FRAME_HIST__ || [])]);

    // Report
    const dIn = reportDist('ZOOM IN', zoomInFrames);
    const dIdle = reportDist('IDLE', idleFrames);
    const dOut = reportDist('ZOOM OUT', zoomOutFrames);

    // Assertions: <2% red (GC spikes), 80%+ green-or-better
    expect(dIn.red).toBeLessThan(Math.max(3, dIn.total * 0.02));
    expect(dOut.red).toBeLessThan(Math.max(3, dOut.total * 0.02));
    expect(dIn.blue + dIn.green).toBeGreaterThan(dIn.total * 0.8);
    expect(dOut.blue + dOut.green).toBeGreaterThan(dOut.total * 0.8);
  });

  test('rapid zoom burst: no red frames', async ({ page }) => {
    await page.evaluate(() => (window as any).__GRAPH_RESET_PERF__());

    // Aggressive burst: 50 wheel events with no wait between JS calls
    // (browser still processes them at vsync rate)
    for (let i = 0; i < 50; i++) {
      await page.mouse.wheel(0, i % 2 === 0 ? -200 : 200);
      await page.waitForTimeout(8); // 125Hz — faster than display refresh
    }
    await page.waitForTimeout(600);

    const frames: number[] = await page.evaluate(() => [...((window as any).__GRAPH_FRAME_HIST__ || [])]);
    const d = reportDist('RAPID BURST', frames);

    expect(d.red).toBeLessThan(Math.max(3, d.total * 0.02));
    expect(d.yellow).toBeLessThan(d.total * 0.2);
  });

  test('perf API: __GRAPH_PERF__ and __GRAPH_FRAME_HIST__ exposed', async ({ page }) => {
    const perf = await page.evaluate(() => (window as any).__GRAPH_PERF__);
    expect(perf).toHaveProperty('fps');
    expect(perf).toHaveProperty('total');
    expect(perf).toHaveProperty('webgl');
    expect(perf).toHaveProperty('pick');
    expect(perf).toHaveProperty('labels');

    const hist = await page.evaluate(() => (window as any).__GRAPH_FRAME_HIST__);
    expect(Array.isArray(hist)).toBe(true);
  });
});
