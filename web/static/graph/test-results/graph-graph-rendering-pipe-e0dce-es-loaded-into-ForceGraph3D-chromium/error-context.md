# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: graph.spec.ts >> graph rendering pipeline >> fixture nodes loaded into ForceGraph3D
- Location: graph.spec.ts:92:3

# Error details

```
Error: expect(received).toBe(expected) // Object.is equality

Expected: 6
Received: 0
```

# Page snapshot

```yaml
- generic [ref=e4]:
  - generic: "Left-click: rotate, Mouse-wheel/middle-click: zoom, Right-click: pan"
```

# Test source

```ts
  1   | /**
  2   |  * graph.spec.ts — Cross-browser rendering tests for the Scribe graph UI.
  3   |  *
  4   |  * No live server required. Tests load test-harness.html from the static
  5   |  * file server (npx serve, started by playwright.config.ts webServer).
  6   |  * All backend calls are mocked with fixture data.
  7   |  *
  8   |  * Run:  npx playwright test
  9   |  * Run (firefox only): npx playwright test --project=firefox
  10  |  */
  11  | 
  12  | import { test, expect, Page } from '@playwright/test';
  13  | import scopeGraph from './fixtures/scope-graph.json' with { type: 'json' };
  14  | 
  15  | // ── mock backend ──────────────────────────────────────────────────────────
  16  | 
  17  | /**
  18  |  * Override window.fetch before any script runs so graph.js picks up the
  19  |  * fixture data without needing a live server. addInitScript runs before
  20  |  * page JS, so deps.fetch = window.fetch.bind(window) gets our mock.
  21  |  */
  22  | async function mockAPI(page: Page, fixture = scopeGraph) {
  23  |   const scopes = (fixture.nodes as any[]).map((n: any) => n.scope);
  24  | 
  25  |   await page.addInitScript(({ graph, scopes }: { graph: any; scopes: string[] }) => {
  26  |     const real = window.fetch.bind(window);
  27  |     (window as any).fetch = (url: string, opts?: any) => {
  28  |       if (url.includes('/api/graph/scopes')) {
  29  |         return Promise.resolve(new Response(JSON.stringify(graph),
  30  |           { status: 200, headers: { 'Content-Type': 'application/json' } }));
  31  |       }
  32  |       if (url.includes('/api/scopes')) {
  33  |         return Promise.resolve(new Response(JSON.stringify(scopes),
  34  |           { status: 200, headers: { 'Content-Type': 'application/json' } }));
  35  |       }
  36  |       // Other API calls (sidebar fragments etc.) return empty stubs.
  37  |       if (url.includes('/api/') || url.includes('/fragments/')) {
  38  |         return Promise.resolve(new Response('{}', { status: 200 }));
  39  |       }
  40  |       return real(url, opts);
  41  |     };
  42  |   }, { graph: fixture, scopes });
  43  | }
  44  | 
  45  | async function loadHarness(page: Page) {
  46  |   await mockAPI(page);
  47  | 
  48  |   // Surface JS errors and console output for diagnosis.
  49  |   const errors: string[] = [];
  50  |   page.on('pageerror', e => errors.push(e.message));
  51  |   page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
  52  | 
  53  |   await page.goto('/test-harness.html');
  54  | 
  55  |   // Wait until ForceGraph3D has initialised and exposed _Graph.
  56  |   try {
  57  |     await page.waitForFunction(() => !!(window as any)._Graph, { timeout: 25_000 });
  58  |   } catch {
  59  |     throw new Error(`_Graph not set after 25s. JS errors:\n${errors.join('\n') || '(none)'}`);
  60  |   }
  61  | 
  62  |   // Let physics run for a bit.
  63  |   await page.waitForTimeout(3_000);
  64  | }
  65  | 
  66  | // ── pixel helpers ─────────────────────────────────────────────────────────
  67  | 
  68  | async function brightPixelPct(page: Page): Promise<number> {
  69  |   const buf = await page.screenshot({ type: 'png' });
  70  |   return page.evaluate(async (png: number[]) => {
  71  |     const blob = new Blob([new Uint8Array(png)], { type: 'image/png' });
  72  |     const img  = await createImageBitmap(blob);
  73  |     const cvs  = new OffscreenCanvas(img.width, img.height);
  74  |     const ctx  = cvs.getContext('2d')!;
  75  |     ctx.drawImage(img, 0, 0);
  76  |     const data = ctx.getImageData(0, 0, img.width, img.height).data;
  77  |     // Background #05050f ≈ luminance 0.005
  78  |     const bgLum = 5/255*0.2126 + 5/255*0.7152 + 15/255*0.0722;
  79  |     let bright = 0;
  80  |     for (let i = 0; i < data.length; i += 4) {
  81  |       const lum = data[i]/255*0.2126 + data[i+1]/255*0.7152 + data[i+2]/255*0.0722;
  82  |       if (lum > bgLum * 3) bright++;
  83  |     }
  84  |     return bright / (data.length / 4) * 100;
  85  |   }, Array.from(buf));
  86  | }
  87  | 
  88  | // ── tests ─────────────────────────────────────────────────────────────────
  89  | 
  90  | test.describe('graph rendering pipeline', () => {
  91  | 
  92  |   test('fixture nodes loaded into ForceGraph3D', async ({ page }) => {
  93  |     await loadHarness(page);
  94  | 
  95  |     const count = await page.evaluate(() =>
  96  |       (window as any)._Graph.graphData().nodes.length);
  97  | 
> 98  |     expect(count).toBe(scopeGraph.nodes.length);
      |                   ^ Error: expect(received).toBe(expected) // Object.is equality
  99  |   });
  100 | 
  101 |   test('nodes are positioned on a sphere — not collapsed to a line', async ({ page }) => {
  102 |     // Nodes live on a 3D sphere of radius UNIVERSE_RADIUS (180).
  103 |     // If fz is set on every node, they collapse to a single Z-line (broken).
  104 |     // If nodes are spread in all 3 dimensions, the sphere layout is correct.
  105 |     await loadHarness(page);
  106 | 
  107 |     const result = await page.evaluate(() => {
  108 |       const nodes = (window as any)._Graph.graphData().nodes as any[];
  109 |       const dists = nodes.map((n: any) =>
  110 |         Math.hypot(n.x || 0, n.y || 0, n.z || 0));
  111 |       const zValues = nodes.map((n: any) => Math.round(n.z || 0));
  112 |       const uniqueZ = new Set(zValues).size;
  113 |       return {
  114 |         count:    nodes.length,
  115 |         minDist:  Math.round(Math.min(...dists)),
  116 |         maxDist:  Math.round(Math.max(...dists)),
  117 |         uniqueZ,                                // 1 = line, >1 = spread in 3D
  118 |       };
  119 |     });
  120 | 
  121 |     // Nodes must not all share the same Z (that's the broken line layout).
  122 |     expect(result.uniqueZ, 'nodes collapsed to a single Z — sphere layout broken').toBeGreaterThan(1);
  123 |   });
  124 | 
  125 |   test('one sphere mesh per node in scene', async ({ page }) => {
  126 |     await loadHarness(page);
  127 | 
  128 |     const { nodes, meshes } = await page.evaluate(() => {
  129 |       const g = (window as any)._Graph;
  130 |       let meshes = 0;
  131 |       g.scene().traverse((o: any) => {
  132 |         if (o.isMesh && o.visible && o.geometry?.type === 'SphereGeometry') meshes++;
  133 |       });
  134 |       return { nodes: g.graphData().nodes.length, meshes };
  135 |     });
  136 | 
  137 |     expect(meshes, `${meshes} meshes for ${nodes} nodes`).toBe(nodes);
  138 |   });
  139 | 
  140 |   test('camera aimed at node cluster — nodes in frustum', async ({ page }) => {
  141 |     await loadHarness(page);
  142 | 
  143 |     const { inFrustum, total } = await page.evaluate(() => {
  144 |       const g    = (window as any)._Graph;
  145 |       const cam  = g.camera();
  146 |       const ctrl = g.controls();
  147 |       const lx = ctrl.target.x - cam.position.x;
  148 |       const ly = ctrl.target.y - cam.position.y;
  149 |       const lz = ctrl.target.z - cam.position.z;
  150 |       const ll = Math.sqrt(lx*lx + ly*ly + lz*lz) || 1;
  151 |       const cosFov = Math.cos(cam.fov / 2 * Math.PI / 180);
  152 |       let inFrustum = 0;
  153 |       (g.graphData().nodes as any[]).forEach((n: any) => {
  154 |         const dx = (n.x||0) - cam.position.x;
  155 |         const dy = (n.y||0) - cam.position.y;
  156 |         const dz = (n.z||0) - cam.position.z;
  157 |         const dl = Math.sqrt(dx*dx + dy*dy + dz*dz) || 1;
  158 |         if ((dx*lx/ll + dy*ly/ll + dz*lz/ll) / dl > cosFov) inFrustum++;
  159 |       });
  160 |       return { inFrustum, total: (g.graphData().nodes as any[]).length };
  161 |     });
  162 | 
  163 |     expect(inFrustum, 'nodes in frustum').toBeGreaterThanOrEqual(Math.floor(total / 2));
  164 |   });
  165 | 
  166 |   test('canvas has non-background pixels — nodes visually present', async ({ page, browserName }) => {
  167 |     // THE cross-browser visual test.
  168 |     // RED in Firefox when transparent sort makes nodes invisible (near 0%).
  169 |     // GREEN in both browsers when rendering is correct.
  170 |     //
  171 |     // Threshold is low (0.05%) because the fixture has only 3 nodes.
  172 |     // The key signal: any rendering > background means nodes are drawing.
  173 |     // A separate CI job should compare chrome vs firefox pct — divergence
  174 |     // flags a browser-specific regression.
  175 |     await loadHarness(page);
  176 |     await page.waitForTimeout(4_000); // extra settle for physics + render
  177 | 
  178 |     const pct = await brightPixelPct(page);
  179 |     console.log(`  [${browserName}] bright pixels: ${pct.toFixed(3)}%`);
  180 | 
  181 |     // Completely blank canvas = 0.000%. Any node or link rendering > 0.05%.
  182 |     expect(pct, `${browserName}: canvas appears completely blank — nothing rendered`).toBeGreaterThan(0.05);
  183 |   });
  184 | 
  185 |   test.skip('node materials are opaque — removed fixNodeMaterials; ForceGraph3D owns material lifecycle', async ({ page }) => {
  186 |     // ForceGraph3D hardcodes transparent=true on all node materials.
  187 |     // With transparent=true + renderOrder=10 on links, Firefox's back-to-front
  188 |     // sort makes nodes invisible. Fix: set transparent=false after creation.
  189 |     //
  190 |     // GREEN: fixNodeTransparency ran and patched the shared material.
  191 |     // RED:   fixNodeTransparency missing or ran before meshes were created.
  192 |     await loadHarness(page);
  193 | 
  194 |     const result = await page.evaluate(() => {
  195 |       const g = (window as any)._Graph;
  196 |       const bad: string[] = [];
  197 |       g.scene().traverse((o: any) => {
  198 |         if (o.isMesh && o.visible && o.geometry?.type === 'SphereGeometry') {
```