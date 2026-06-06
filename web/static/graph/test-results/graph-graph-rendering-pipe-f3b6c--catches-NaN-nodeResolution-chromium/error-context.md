# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: graph.spec.ts >> graph rendering pipeline >> sphere meshes have non-zero triangle count — catches NaN nodeResolution
- Location: graph.spec.ts:209:3

# Error details

```
Error: invisible sphere meshes detected:
zero indices: uuid=c48bf4be-cfa3-448a-a073-d97b574666bd
zero vertices: uuid=c48bf4be-cfa3-448a-a073-d97b574666bd
invalid widthSegments=n => n.kind === "scope" ? 24 : 8: uuid=c48bf4be-cfa3-448a-a073-d97b574666bd
zero indices: uuid=fe83162a-3eb4-48ef-b43a-b82af57194a6
zero vertices: uuid=fe83162a-3eb4-48ef-b43a-b82af57194a6
invalid widthSegments=n => n.kind === "scope" ? 24 : 8: uuid=fe83162a-3eb4-48ef-b43a-b82af57194a6
zero indices: uuid=e1e84f43-326e-48a7-8b14-b31a2961246d
zero vertices: uuid=e1e84f43-326e-48a7-8b14-b31a2961246d
invalid widthSegments=n => n.kind === "scope" ? 24 : 8: uuid=e1e84f43-326e-48a7-8b14-b31a2961246d
zero indices: uuid=a51411e2-f2f1-4ffe-a2db-204ea5326fe0
zero vertices: uuid=a51411e2-f2f1-4ffe-a2db-204ea5326fe0
invalid widthSegments=n => n.kind === "scope" ? 24 : 8: uuid=a51411e2-f2f1-4ffe-a2db-204ea5326fe0
zero indices: uuid=197f5f74-3c5a-440e-b9e8-ab7ff4114677
zero vertices: uuid=197f5f74-3c5a-440e-b9e8-ab7ff4114677
invalid widthSegments=n => n.kind === "scope" ? 24 : 8: uuid=197f5f74-3c5a-440e-b9e8-ab7ff4114677
zero indices: uuid=e87af941-ee1e-473c-950f-a0768c929c01
zero vertices: uuid=e87af941-ee1e-473c-950f-a0768c929c01
invalid widthSegments=n => n.kind === "scope" ? 24 : 8: uuid=e87af941-ee1e-473c-950f-a0768c929c01

expect(received).toHaveLength(expected)

Expected length: 0
Received length: 18
Received array:  ["zero indices: uuid=c48bf4be-cfa3-448a-a073-d97b574666bd", "zero vertices: uuid=c48bf4be-cfa3-448a-a073-d97b574666bd", "invalid widthSegments=n => n.kind === \"scope\" ? 24 : 8: uuid=c48bf4be-cfa3-448a-a073-d97b574666bd", "zero indices: uuid=fe83162a-3eb4-48ef-b43a-b82af57194a6", "zero vertices: uuid=fe83162a-3eb4-48ef-b43a-b82af57194a6", "invalid widthSegments=n => n.kind === \"scope\" ? 24 : 8: uuid=fe83162a-3eb4-48ef-b43a-b82af57194a6", "zero indices: uuid=e1e84f43-326e-48a7-8b14-b31a2961246d", "zero vertices: uuid=e1e84f43-326e-48a7-8b14-b31a2961246d", "invalid widthSegments=n => n.kind === \"scope\" ? 24 : 8: uuid=e1e84f43-326e-48a7-8b14-b31a2961246d", "zero indices: uuid=a51411e2-f2f1-4ffe-a2db-204ea5326fe0", …]
```

# Page snapshot

```yaml
- generic [ref=e4]:
  - generic: "Left-click: rotate, Mouse-wheel/middle-click: zoom, Right-click: pan"
```

# Test source

```ts
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
  199 |           if (o.material.transparent !== false) bad.push(`${o.uuid}: transparent=${o.material.transparent}`);
  200 |           if (o.material.depthWrite  !== true)  bad.push(`${o.uuid}: depthWrite=${o.material.depthWrite}`);
  201 |         }
  202 |       });
  203 |       return { bad };
  204 |     });
  205 | 
  206 |     expect(result.bad, `node materials still transparent:\n${result.bad.join('\n')}`).toHaveLength(0);
  207 |   });
  208 | 
  209 |   test('sphere meshes have non-zero triangle count — catches NaN nodeResolution', async ({ page }) => {
  210 |     // Regression: nodeResolution(fn) → Math.floor(fn)=NaN → SphereGeometry
  211 |     // with NaN segments → 0 triangles → mesh exists but produces no pixels.
  212 |     // This test catches it without screenshots: directly reads index/vertex counts.
  213 |     await loadHarness(page);
  214 | 
  215 |     const result = await page.evaluate(() => {
  216 |       const g = (window as any)._Graph;
  217 |       const bad: string[] = [];
  218 |       g.scene().traverse((o: any) => {
  219 |         if (!o.isMesh || o.geometry?.type !== 'SphereGeometry') return;
  220 |         const idx = o.geometry.index;
  221 |         const pos = o.geometry.attributes?.position;
  222 |         // segments = NaN → 0 triangles → idx.count = 0
  223 |         if (!idx || idx.count === 0)
  224 |           bad.push(`zero indices: uuid=${o.uuid}`);
  225 |         if (!pos || pos.count === 0)
  226 |           bad.push(`zero vertices: uuid=${o.uuid}`);
  227 |         // Also check segment count directly
  228 |         const w = o.geometry.parameters?.widthSegments;
  229 |         if (!Number.isFinite(w) || w < 3)
  230 |           bad.push(`invalid widthSegments=${w}: uuid=${o.uuid}`);
  231 |       });
  232 |       return bad;
  233 |     });
  234 | 
> 235 |     expect(result, `invisible sphere meshes detected:\n${result.join('\n')}`).toHaveLength(0);
      |                                                                               ^ Error: invisible sphere meshes detected:
  236 |   });
  237 | 
  238 |   test('WebGL no errors', async ({ page }) => {
  239 |     await loadHarness(page);
  240 |     const err = await page.evaluate(() =>
  241 |       (window as any)._Graph.renderer().getContext().getError());
  242 |     expect(err, 'GL error code (0=none)').toBe(0);
  243 |   });
  244 | });
  245 | 
```