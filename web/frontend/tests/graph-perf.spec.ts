import { test, expect } from '@playwright/test';

// Full browser perf test: simulates zoom on the live graph page and
// reads frame timing from the built-in performance HUD (window.__GRAPH_PERF__).
// Requires the scribe server running on localhost:8083.
//
// Run: npx playwright test tests/graph-perf.spec.ts
// With xvfb (for CI with GPU): xvfb-run npx playwright test tests/graph-perf.spec.ts

test.describe('graph zoom performance', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/app/graph');
    // Wait for graph to render and perf data to populate
    await page.waitForFunction(() => (window as any).__GRAPH_PERF__?.fps > 0, {
      timeout: 10000,
    });
  });

  test('baseline FPS is above 30', async ({ page }) => {
    const perf = await page.evaluate(() => (window as any).__GRAPH_PERF__);
    expect(perf.fps).toBeGreaterThanOrEqual(30);
    console.log(`Baseline: ${perf.fps} fps, ${perf.total.toFixed(1)}ms/frame`);
    console.log(`  WebGL: ${perf.webgl.toFixed(2)}ms, Pick: ${perf.pick.toFixed(2)}ms, Labels: ${perf.labels.toFixed(2)}ms`);
  });

  test('zoom does not drop below 24 fps', async ({ page }) => {
    const canvas = page.locator('canvas').first();
    const box = await canvas.boundingBox();
    if (!box) throw new Error('Canvas not found');

    const cx = box.x + box.width / 2;
    const cy = box.y + box.height / 2;

    // Rapid zoom: 30 wheel events over 500ms (simulates fast scroll)
    for (let i = 0; i < 30; i++) {
      await page.mouse.wheel(0, -120);
      await page.waitForTimeout(16); // ~60fps pacing
    }

    // Let perf HUD catch up (updates at 2Hz)
    await page.waitForTimeout(600);

    const perf = await page.evaluate(() => (window as any).__GRAPH_PERF__);
    console.log(`Under zoom: ${perf.fps} fps, ${perf.total.toFixed(1)}ms/frame`);
    console.log(`  WebGL: ${perf.webgl.toFixed(2)}ms, Pick: ${perf.pick.toFixed(2)}ms, Labels: ${perf.labels.toFixed(2)}ms`);

    expect(perf.fps).toBeGreaterThanOrEqual(24);
    expect(perf.total).toBeLessThan(42); // 42ms = 24fps budget
  });

  test('zoom out + zoom in round trip', async ({ page }) => {
    const results: Array<{ direction: string; fps: number; total: number; labels: number }> = [];

    for (const [dir, delta] of [['out', 120], ['in', -120]] as const) {
      for (let i = 0; i < 20; i++) {
        await page.mouse.wheel(0, delta);
        await page.waitForTimeout(16);
      }
      await page.waitForTimeout(600);
      const perf = await page.evaluate(() => (window as any).__GRAPH_PERF__);
      results.push({ direction: dir, fps: perf.fps, total: perf.total, labels: perf.labels });
    }

    for (const r of results) {
      console.log(`Zoom ${r.direction}: ${r.fps} fps, ${r.total.toFixed(1)}ms (labels: ${r.labels.toFixed(2)}ms)`);
      expect(r.fps).toBeGreaterThanOrEqual(24);
    }
  });

  test('perf data is exposed on window', async ({ page }) => {
    const perf = await page.evaluate(() => (window as any).__GRAPH_PERF__);
    expect(perf).toBeDefined();
    expect(perf).toHaveProperty('fps');
    expect(perf).toHaveProperty('total');
    expect(perf).toHaveProperty('webgl');
    expect(perf).toHaveProperty('pick');
    expect(perf).toHaveProperty('labels');
  });
});
