/**
 * graph.spec.ts — Cross-browser rendering tests for the Scribe graph UI.
 *
 * No live server required. Tests load test-harness.html from the static
 * file server (npx serve, started by playwright.config.ts webServer).
 * All backend calls are mocked with fixture data.
 *
 * Run:  npx playwright test
 * Run (firefox only): npx playwright test --project=firefox
 */

import { test, expect, Page } from '@playwright/test';
import scopeGraph from './fixtures/scope-graph.json' with { type: 'json' };

// ── mock backend ──────────────────────────────────────────────────────────

/**
 * Override window.fetch before any script runs so graph.js picks up the
 * fixture data without needing a live server. addInitScript runs before
 * page JS, so deps.fetch = window.fetch.bind(window) gets our mock.
 */
async function mockAPI(page: Page, fixture = scopeGraph) {
  const scopes = (fixture.nodes as any[]).map((n: any) => n.scope);

  await page.addInitScript(({ graph, scopes }: { graph: any; scopes: string[] }) => {
    const real = window.fetch.bind(window);
    (window as any).fetch = (url: string, opts?: any) => {
      if (url.includes('/api/graph/scopes')) {
        return Promise.resolve(new Response(JSON.stringify(graph),
          { status: 200, headers: { 'Content-Type': 'application/json' } }));
      }
      if (url.includes('/api/scopes')) {
        return Promise.resolve(new Response(JSON.stringify(scopes),
          { status: 200, headers: { 'Content-Type': 'application/json' } }));
      }
      // Other API calls (sidebar fragments etc.) return empty stubs.
      if (url.includes('/api/') || url.includes('/fragments/')) {
        return Promise.resolve(new Response('{}', { status: 200 }));
      }
      return real(url, opts);
    };
  }, { graph: fixture, scopes });
}

async function loadHarness(page: Page) {
  await mockAPI(page);

  // Surface JS errors and console output for diagnosis.
  const errors: string[] = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

  await page.goto('/test-harness.html');

  // Wait until ForceGraph3D has initialised and exposed _Graph.
  try {
    await page.waitForFunction(() => !!(window as any)._Graph, { timeout: 25_000 });
  } catch {
    throw new Error(`_Graph not set after 25s. JS errors:\n${errors.join('\n') || '(none)'}`);
  }

  // Let physics run for a bit.
  await page.waitForTimeout(3_000);
}

// ── pixel helpers ─────────────────────────────────────────────────────────

async function brightPixelPct(page: Page): Promise<number> {
  const buf = await page.screenshot({ type: 'png' });
  return page.evaluate(async (png: number[]) => {
    const blob = new Blob([new Uint8Array(png)], { type: 'image/png' });
    const img  = await createImageBitmap(blob);
    const cvs  = new OffscreenCanvas(img.width, img.height);
    const ctx  = cvs.getContext('2d')!;
    ctx.drawImage(img, 0, 0);
    const data = ctx.getImageData(0, 0, img.width, img.height).data;
    // Background #05050f ≈ luminance 0.005
    const bgLum = 5/255*0.2126 + 5/255*0.7152 + 15/255*0.0722;
    let bright = 0;
    for (let i = 0; i < data.length; i += 4) {
      const lum = data[i]/255*0.2126 + data[i+1]/255*0.7152 + data[i+2]/255*0.0722;
      if (lum > bgLum * 3) bright++;
    }
    return bright / (data.length / 4) * 100;
  }, Array.from(buf));
}

// ── tests ─────────────────────────────────────────────────────────────────

test.describe('graph rendering pipeline', () => {

  test('fixture nodes loaded into ForceGraph3D', async ({ page }) => {
    await loadHarness(page);

    const count = await page.evaluate(() =>
      (window as any)._Graph.graphData().nodes.length);

    expect(count).toBe(scopeGraph.nodes.length);
  });

  test('nodes are positioned on a sphere — not collapsed to a line', async ({ page }) => {
    // Nodes live on a 3D sphere of radius UNIVERSE_RADIUS (180).
    // If fz is set on every node, they collapse to a single Z-line (broken).
    // If nodes are spread in all 3 dimensions, the sphere layout is correct.
    await loadHarness(page);

    const result = await page.evaluate(() => {
      const nodes = (window as any)._Graph.graphData().nodes as any[];
      const dists = nodes.map((n: any) =>
        Math.hypot(n.x || 0, n.y || 0, n.z || 0));
      const zValues = nodes.map((n: any) => Math.round(n.z || 0));
      const uniqueZ = new Set(zValues).size;
      return {
        count:    nodes.length,
        minDist:  Math.round(Math.min(...dists)),
        maxDist:  Math.round(Math.max(...dists)),
        uniqueZ,                                // 1 = line, >1 = spread in 3D
      };
    });

    // Nodes must not all share the same Z (that's the broken line layout).
    expect(result.uniqueZ, 'nodes collapsed to a single Z — sphere layout broken').toBeGreaterThan(1);
  });

  test('one sphere mesh per node in scene', async ({ page }) => {
    await loadHarness(page);

    const { nodes, meshes } = await page.evaluate(() => {
      const g = (window as any)._Graph;
      let meshes = 0;
      g.scene().traverse((o: any) => {
        if (o.isMesh && o.visible && o.geometry?.type === 'SphereGeometry') meshes++;
      });
      return { nodes: g.graphData().nodes.length, meshes };
    });

    expect(meshes, `${meshes} meshes for ${nodes} nodes`).toBe(nodes);
  });

  test('camera aimed at node cluster — nodes in frustum', async ({ page }) => {
    await loadHarness(page);

    const { inFrustum, total } = await page.evaluate(() => {
      const g    = (window as any)._Graph;
      const cam  = g.camera();
      const ctrl = g.controls();
      const lx = ctrl.target.x - cam.position.x;
      const ly = ctrl.target.y - cam.position.y;
      const lz = ctrl.target.z - cam.position.z;
      const ll = Math.sqrt(lx*lx + ly*ly + lz*lz) || 1;
      const cosFov = Math.cos(cam.fov / 2 * Math.PI / 180);
      let inFrustum = 0;
      (g.graphData().nodes as any[]).forEach((n: any) => {
        const dx = (n.x||0) - cam.position.x;
        const dy = (n.y||0) - cam.position.y;
        const dz = (n.z||0) - cam.position.z;
        const dl = Math.sqrt(dx*dx + dy*dy + dz*dz) || 1;
        if ((dx*lx/ll + dy*ly/ll + dz*lz/ll) / dl > cosFov) inFrustum++;
      });
      return { inFrustum, total: (g.graphData().nodes as any[]).length };
    });

    expect(inFrustum, 'nodes in frustum').toBeGreaterThanOrEqual(Math.floor(total / 2));
  });

  test('canvas has non-background pixels — nodes visually present', async ({ page, browserName }) => {
    // THE cross-browser visual test.
    // RED in Firefox when transparent sort makes nodes invisible (near 0%).
    // GREEN in both browsers when rendering is correct.
    //
    // Threshold is low (0.05%) because the fixture has only 3 nodes.
    // The key signal: any rendering > background means nodes are drawing.
    // A separate CI job should compare chrome vs firefox pct — divergence
    // flags a browser-specific regression.
    await loadHarness(page);
    await page.waitForTimeout(4_000); // extra settle for physics + render

    const pct = await brightPixelPct(page);
    console.log(`  [${browserName}] bright pixels: ${pct.toFixed(3)}%`);

    // Completely blank canvas = 0.000%. Any node or link rendering > 0.05%.
    expect(pct, `${browserName}: canvas appears completely blank — nothing rendered`).toBeGreaterThan(0.05);
  });

  test.skip('node materials are opaque — removed fixNodeMaterials; ForceGraph3D owns material lifecycle', async ({ page }) => {
    // ForceGraph3D hardcodes transparent=true on all node materials.
    // With transparent=true + renderOrder=10 on links, Firefox's back-to-front
    // sort makes nodes invisible. Fix: set transparent=false after creation.
    //
    // GREEN: fixNodeTransparency ran and patched the shared material.
    // RED:   fixNodeTransparency missing or ran before meshes were created.
    await loadHarness(page);

    const result = await page.evaluate(() => {
      const g = (window as any)._Graph;
      const bad: string[] = [];
      g.scene().traverse((o: any) => {
        if (o.isMesh && o.visible && o.geometry?.type === 'SphereGeometry') {
          if (o.material.transparent !== false) bad.push(`${o.uuid}: transparent=${o.material.transparent}`);
          if (o.material.depthWrite  !== true)  bad.push(`${o.uuid}: depthWrite=${o.material.depthWrite}`);
        }
      });
      return { bad };
    });

    expect(result.bad, `node materials still transparent:\n${result.bad.join('\n')}`).toHaveLength(0);
  });

  test('sphere meshes have non-zero triangle count — catches NaN nodeResolution', async ({ page }) => {
    // Regression: nodeResolution(fn) → Math.floor(fn)=NaN → SphereGeometry
    // with NaN segments → 0 triangles → mesh exists but produces no pixels.
    // This test catches it without screenshots: directly reads index/vertex counts.
    await loadHarness(page);

    const result = await page.evaluate(() => {
      const g = (window as any)._Graph;
      const bad: string[] = [];
      g.scene().traverse((o: any) => {
        if (!o.isMesh || o.geometry?.type !== 'SphereGeometry') return;
        const idx = o.geometry.index;
        const pos = o.geometry.attributes?.position;
        // segments = NaN → 0 triangles → idx.count = 0
        if (!idx || idx.count === 0)
          bad.push(`zero indices: uuid=${o.uuid}`);
        if (!pos || pos.count === 0)
          bad.push(`zero vertices: uuid=${o.uuid}`);
        // Also check segment count directly
        const w = o.geometry.parameters?.widthSegments;
        if (!Number.isFinite(w) || w < 3)
          bad.push(`invalid widthSegments=${w}: uuid=${o.uuid}`);
      });
      return bad;
    });

    expect(result, `invisible sphere meshes detected:\n${result.join('\n')}`).toHaveLength(0);
  });

  test('WebGL no errors', async ({ page }) => {
    await loadHarness(page);
    const err = await page.evaluate(() =>
      (window as any)._Graph.renderer().getContext().getError());
    expect(err, 'GL error code (0=none)').toBe(0);
  });
});
