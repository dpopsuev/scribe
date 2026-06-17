import { test, expect } from '@playwright/test';

// Performance regression gate for the graph visualization.
// Simulates real user zoom/pan interactions, samples per-frame FPS
// via rAF timestamp delta, and asserts against the color scale:
//   Blue: 60+ fps | Green: 30-60 | Yellow: 15-30 | Red: <15
//
// Also checks the jank detector (setInterval-based) for main-thread
// blocks that rAF can't see (the $effect/startSimulation bug was 2-3s).
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
  if (!frames.length) { console.log(`\n${label}: no frames`); return { blue: 0, green: 0, yellow: 0, red: 0, total: 0 }; }
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

async function setupGraph(page: any) {
  await page.goto('/app/graph');
  await page.waitForFunction(
    () => (window as any).__GRAPH_PERF__?.fps > 0,
    { timeout: 15000 },
  );
  const box = await page.locator('canvas').first().boundingBox();
  if (!box) throw new Error('Canvas not found');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.evaluate(() => (window as any).__GRAPH_RESET_PERF__());
  await page.waitForTimeout(300);
  return box;
}

test.describe('perf regression gate', () => {

  test('zoom in/out: zero jank (>500ms blocks)', async ({ page }) => {
    await setupGraph(page);

    // Zoom in
    for (let i = 0; i < 30; i++) {
      await page.mouse.wheel(0, -120);
      await page.waitForTimeout(16);
    }
    await page.waitForTimeout(300);

    // Zoom out
    for (let i = 0; i < 30; i++) {
      await page.mouse.wheel(0, 120);
      await page.waitForTimeout(16);
    }
    await page.waitForTimeout(600);

    const jank: Array<{ blocked: number }> = await page.evaluate(() =>
      (window as any).__GRAPH_JANK__?.() || []
    );
    const severe = jank.filter(j => j.blocked > 500);
    if (severe.length) {
      console.log('SEVERE JANK DETECTED:');
      for (const j of severe) console.log(`  ${j.blocked}ms block`);
    }
    expect(severe).toHaveLength(0);
  });

  test('zoom in → idle → zoom out: FPS stays green+', async ({ page }) => {
    await setupGraph(page);

    for (let i = 0; i < 30; i++) {
      await page.mouse.wheel(0, -120);
      await page.waitForTimeout(16);
    }
    await page.waitForTimeout(600);
    const zoomInFrames: number[] = await page.evaluate(() => [...((window as any).__GRAPH_FRAME_HIST__ || [])]);

    await page.evaluate(() => (window as any).__GRAPH_RESET_PERF__());
    for (let i = 0; i < 30; i++) {
      await page.mouse.wheel(0, 120);
      await page.waitForTimeout(16);
    }
    await page.waitForTimeout(600);
    const zoomOutFrames: number[] = await page.evaluate(() => [...((window as any).__GRAPH_FRAME_HIST__ || [])]);

    const dIn = reportDist('ZOOM IN', zoomInFrames);
    const dOut = reportDist('ZOOM OUT', zoomOutFrames);

    // Gate: <2% red, 80%+ green-or-better
    expect(dIn.red).toBeLessThan(Math.max(3, dIn.total * 0.02));
    expect(dOut.red).toBeLessThan(Math.max(3, dOut.total * 0.02));
    expect(dIn.blue + dIn.green).toBeGreaterThan(dIn.total * 0.8);
    expect(dOut.blue + dOut.green).toBeGreaterThan(dOut.total * 0.8);
  });

  test('rapid burst (125Hz): no sustained drops', async ({ page }) => {
    await setupGraph(page);

    for (let i = 0; i < 50; i++) {
      await page.mouse.wheel(0, i % 2 === 0 ? -200 : 200);
      await page.waitForTimeout(8);
    }
    await page.waitForTimeout(600);

    const frames: number[] = await page.evaluate(() => [...((window as any).__GRAPH_FRAME_HIST__ || [])]);
    const d = reportDist('RAPID BURST', frames);

    expect(d.red).toBeLessThan(Math.max(3, d.total * 0.02));
    expect(d.yellow).toBeLessThan(d.total * 0.2);
  });

  test('render budget: <2ms average frame time', async ({ page }) => {
    await setupGraph(page);

    for (let i = 0; i < 20; i++) {
      await page.mouse.wheel(0, -100);
      await page.waitForTimeout(16);
    }
    await page.waitForTimeout(600);

    const perf = await page.evaluate(() => (window as any).__GRAPH_PERF__);
    console.log(`\nRender budget: ${perf.total.toFixed(2)}ms (gl:${perf.webgl.toFixed(2)} pk:${perf.pick.toFixed(2)} lbl:${perf.labels.toFixed(2)})`);
    expect(perf.total).toBeLessThan(2);
  });

  test('debug API contract', async ({ page }) => {
    await setupGraph(page);

    const perf = await page.evaluate(() => (window as any).__GRAPH_PERF__);
    expect(perf).toHaveProperty('fps');
    expect(perf).toHaveProperty('total');
    expect(perf).toHaveProperty('webgl');
    expect(perf).toHaveProperty('pick');
    expect(perf).toHaveProperty('labels');

    const hist = await page.evaluate(() => (window as any).__GRAPH_FRAME_HIST__);
    expect(Array.isArray(hist)).toBe(true);

    const jank = await page.evaluate(() => (window as any).__GRAPH_JANK__());
    expect(Array.isArray(jank)).toBe(true);

    await page.evaluate(() => (window as any).__GRAPH_RESET_PERF__());
    const afterReset = await page.evaluate(() => (window as any).__GRAPH_FRAME_HIST__);
    // Reset clears the buffer but rAF may have added a few frames before we read
    expect(afterReset.length).toBeLessThan(30);
  });
});
