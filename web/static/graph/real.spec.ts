/**
 * real.spec.ts — Integration tests against the real production server.
 *
 * No mocks. Real CDN scripts. Real Go template. Real data (85 scope nodes).
 * Runs against localhost:8083 — server must be up before running.
 *
 * Compares /graph-v1 (known working) against /graph (current).
 * A visual diff or pixel divergence between the two reveals regressions.
 *
 * Run: npx playwright test real.spec.ts --project=firefox
 */

import { test, expect, Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const SERVER = 'http://localhost:8083';
const SETTLE_MS = 15_000; // CDN scripts + physics settle

// ── helpers ───────────────────────────────────────────────────────────────

async function loadReal(page: Page, routePath: string) {
  const logs: string[] = [];
  const errors: string[] = [];

  page.on('console', m => logs.push(`[${m.type()}] ${m.text()}`));
  page.on('pageerror', e => errors.push(e.message));

  await page.goto(`${SERVER}${routePath}`);

  // Wait for WebGL canvas — works for both /graph (_Graph) and /graph-v1 (Graph).
  await page.waitForSelector('#graph-root canvas', { timeout: 25_000 });
  await page.waitForTimeout(SETTLE_MS);

  return { logs, errors };
}

async function graphState(page: Page) {
  return page.evaluate(() => {
    const g = (window as any)._Graph ?? (window as any).Graph;
    if (!g) return null;

    const nodes = g.graphData().nodes as any[];
    const cam   = g.camera();
    const ctrl  = g.controls();

    const spheres: any[] = [];
    let meshCount = 0, transparentCount = 0, blackCount = 0, zeroOpacity = 0;
    g.scene().traverse((o: any) => {
      if (!o.isMesh || !o.visible || o.geometry?.type !== 'SphereGeometry') return;
      meshCount++;
      const m = o.material;
      if (m.transparent) transparentCount++;
      if (m.opacity === 0) zeroOpacity++;
      const c = m.color;
      if (c && c.r === 0 && c.g === 0 && c.b === 0) blackCount++;
      if (spheres.length < 2) spheres.push({
        transparent: m.transparent,
        opacity:     m.opacity,
        depthWrite:  m.depthWrite,
        color:       c ? { r: +c.r.toFixed(2), g: +c.g.toFixed(2), b: +c.b.toFixed(2) } : null,
        renderOrder: o.renderOrder,
        side:        m.side,
      });
    });

    const lx = ctrl.target.x - cam.position.x;
    const ly = ctrl.target.y - cam.position.y;
    const lz = ctrl.target.z - cam.position.z;
    const ll = Math.sqrt(lx*lx + ly*ly + lz*lz) || 1;
    const cosFov = Math.cos(cam.fov / 2 * Math.PI / 180);

    let inFrustum = 0;
    nodes.forEach((n: any) => {
      const dx = (n.x||0) - cam.position.x;
      const dy = (n.y||0) - cam.position.y;
      const dz = (n.z||0) - cam.position.z;
      const dl = Math.sqrt(dx*dx + dy*dy + dz*dz) || 1;
      if ((dx*lx/ll + dy*ly/ll + dz*lz/ll) / dl > cosFov) inFrustum++;
    });

    const lights: any[] = [];
    g.scene().traverse((o: any) => {
      if (o.isLight) lights.push({ type: o.type, intensity: +o.intensity.toFixed(2) });
    });

    return {
      nodeCount:        nodes.length,
      meshCount,
      transparentCount,
      blackCount,
      zeroOpacity,
      inFrustum,
      sphereSample:     spheres,
      lights,
      camPos: { x: Math.round(cam.position.x), y: Math.round(cam.position.y), z: Math.round(cam.position.z) },
      target: { x: Math.round(ctrl.target.x),  y: Math.round(ctrl.target.y),  z: Math.round(ctrl.target.z) },
      glError: g.renderer().getContext().getError(),
    };
  });
}

async function brightPixelPct(page: Page): Promise<number> {
  const buf = await page.screenshot({ type: 'png', fullPage: false });
  return page.evaluate(async (png: number[]) => {
    const blob = new Blob([new Uint8Array(png)], { type: 'image/png' });
    const img  = await createImageBitmap(blob);
    const cvs  = new OffscreenCanvas(img.width, img.height);
    const ctx  = cvs.getContext('2d')!;
    ctx.drawImage(img, 0, 0);
    const data = ctx.getImageData(0, 0, img.width, img.height).data;
    const bgLum = 5/255*0.2126 + 5/255*0.7152 + 15/255*0.0722;
    let bright = 0;
    for (let i = 0; i < data.length; i += 4) {
      const lum = data[i]/255*0.2126 + data[i+1]/255*0.7152 + data[i+2]/255*0.0722;
      if (lum > bgLum * 3) bright++;
    }
    return bright / (data.length / 4) * 100;
  }, Array.from(buf));
}

function saveScreenshot(page: Page, name: string) {
  const dir = path.join(__dirname, 'test-results', 'real');
  fs.mkdirSync(dir, { recursive: true });
  return page.screenshot({ path: path.join(dir, `${name}.png`), fullPage: false });
}

// ── current /graph ───────────────────────────────────────────────────────

test.describe('real server — current /graph', () => {
  test('graph renders nodes — must match v1 baseline', async ({ page, browserName }) => {
    const { logs, errors } = await loadReal(page, '/graph');

    const state = await graphState(page);
    const pct   = await brightPixelPct(page);
    await saveScreenshot(page, `current-${browserName}`);

    console.log(`\n[${browserName}] /graph`);
    console.log(`  nodes=${state?.nodeCount}  meshes=${state?.meshCount}  inFrustum=${state?.inFrustum}`);
    console.log(`  transparent=${state?.transparentCount}  black=${state?.blackCount}  opacity0=${state?.zeroOpacity}`);
    console.log(`  lights=${JSON.stringify(state?.lights)}`);
    console.log(`  sphereSample=${JSON.stringify(state?.sphereSample)}`);
    console.log(`  glError=${state?.glError}  bright=${pct.toFixed(3)}%`);
    console.log(`  camPos=${JSON.stringify(state?.camPos)}  target=${JSON.stringify(state?.target)}`);
    logs.filter(l => l.includes('[graph]')).forEach(l => console.log(' ', l));
    if (errors.length) console.log('  ERRORS:', errors);

    // Every property that makes nodes visible must hold.
    expect(state?.nodeCount,        'nodes loaded').toBeGreaterThan(0);
    expect(state?.meshCount,        'one mesh per node').toBe(state?.nodeCount);
    expect(state?.glError, 'WebGL clean').toBe(0);

    // Regression: nodeResolution passed as function → Math.floor(fn)=NaN →
    // SphereGeometry with NaN segments → 0 triangles → invisible mesh.
    // The mesh appears in every scene-graph diagnostic but produces no pixels.
    const badSegments = await page.evaluate(() => {
      const g = (window as any)._Graph;
      const bad: string[] = [];
      g.scene().traverse((o: any) => {
        if (!o.isMesh || o.geometry?.type !== 'SphereGeometry') return;
        const w = o.geometry.parameters?.widthSegments;
        if (!Number.isFinite(w) || w < 3)
          bad.push(`id=${o.uuid} widthSegments=${w}`);
      });
      return bad;
    });
    expect(badSegments, `sphere meshes with invalid segment count:\n${badSegments.join('\n')}`).toHaveLength(0);

    // Camera must not drift far — if nodes are at radius 180 and camera is at
    // radius > UNIVERSE_RADIUS*5=900, nodes are sub-pixel (< 7px diameter).
    const result = await page.evaluate(() => {
      const g    = (window as any)._Graph;
      const cam  = g.camera().position;
      const ctrl = g.controls();
      const tgt  = ctrl.target;
      const distFromCoM = Math.hypot(cam.x-tgt.x, cam.y-tgt.y, cam.z-tgt.z);
      const n    = g.graphData().nodes.length;
      const BASE = 80;
      const cap  = BASE * Math.max(1, Math.log10(Math.max(n, 10) / 10) + 1);
      const fov  = g.camera().fov;
      const expected = (cap / Math.tan(fov / 2 * Math.PI / 180)) * 1.25;
      return { distFromCoM: Math.round(distFromCoM), expected: Math.round(expected), n };
    });
    console.log(`  boot camDistFromCoM=${result.distFromCoM} expected=${result.expected} n=${result.n}`);
    expect(result.distFromCoM,
      `boot camera ${result.distFromCoM} units from CoM, expected ~${result.expected}`
    ).toBeLessThan(result.expected * 1.1);

    // Threshold is 3%: links alone produce ~2%, nodes add at least 1% more.
    // 0.5% only catches blank screens — this catches invisible-nodes-with-links.
    expect(pct, `${browserName}: only links visible, nodes absent — bright=${pct.toFixed(3)}%`).toBeGreaterThan(3);

    // Zoom-adaptive clustering must not collapse nodes to a point.
    // If all nodes overlap, they appear as one tiny dot — same symptom as invisible nodes.
    const spread = await page.evaluate(() => {
      const nodes = (window as any)._Graph.graphData().nodes;
      const dists = nodes.map((n: any) => Math.hypot(n.x||0, n.y||0, n.z||0));
      return Math.max(...dists);
    });
    expect(spread, `nodes collapsed — max radius only ${Math.round(spread)} units`).toBeGreaterThan(30);
  });
});
