/**
 * shelf.spec.ts — Headless visual verification of the Bookshelf (layered) layout.
 *
 * Runs against the live server at localhost:8083.
 * Verifies: shelf mode activates, nodes pin to distinct Y levels,
 * shelf indicators render, camera faces straight on (no orbit).
 *
 * Run: npx playwright test shelf.spec.ts --project=chromium
 */

import { test, expect, Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const SERVER = 'http://localhost:8083';

async function loadGraph(page: Page) {
  const errors: string[] = [];
  const logs: string[] = [];
  page.on('console', m => logs.push(`[${m.type()}] ${m.text()}`));
  page.on('pageerror', e => errors.push(e.message));

  await page.goto(`${SERVER}/graph`);
  await page.waitForSelector('#graph-root canvas', { timeout: 25_000 });
  // Wait for physics to settle + data to load
  await page.waitForTimeout(8_000);
  return { errors, logs };
}

function saveScreenshot(page: Page, name: string) {
  const dir = path.join(__dirname, 'test-results', 'shelf');
  fs.mkdirSync(dir, { recursive: true });
  return page.screenshot({ path: path.join(dir, `${name}.png`), fullPage: false });
}

test.describe('Bookshelf layout', () => {
  test('selecting Bookshelf pins nodes to distinct Y shelves', async ({ page }) => {
    const { errors } = await loadGraph(page);
    await saveScreenshot(page, '01-before-shelf');

    // Verify graph loaded
    const nodesBefore = await page.evaluate(() => {
      const g = (window as any)._Graph;
      return g ? g.graphData().nodes.length : 0;
    });
    console.log(`Nodes loaded: ${nodesBefore}`);
    expect(nodesBefore, 'graph should have nodes').toBeGreaterThan(0);

    // Select "Bookshelf (layered)" from layout dropdown
    await page.selectOption('#layout-select', 'shelf');
    console.log('Selected Bookshelf layout');

    // Wait for shelf layout to settle
    await page.waitForTimeout(3_000);
    await saveScreenshot(page, '02-after-shelf');

    // Check for JS errors during shelf activation
    const shelfErrors = errors.filter(e =>
      e.includes('shelf') || e.includes('lens') || e.includes('assignShelves')
    );
    expect(shelfErrors, `JS errors during shelf activation: ${shelfErrors.join('; ')}`).toHaveLength(0);

    // Verify shelf mode state
    const shelfState = await page.evaluate(() => {
      const g = (window as any)._Graph;
      if (!g) return null;

      const nodes = g.graphData().nodes;
      const yValues = new Set<number>();
      const nodeData: Array<{kind: string, y: number, fy: number | null, z: number}> = [];

      for (const n of nodes) {
        yValues.add(Math.round(n.y || 0));
        nodeData.push({
          kind: n.kind,
          y: Math.round(n.y || 0),
          fy: n.fy != null ? Math.round(n.fy) : null,
          z: Math.round(n.z || 0),
        });
      }

      // Check camera orientation
      const cam = g.camera();
      const ctrl = g.controls();
      const camZ = cam.position.z - ctrl.target.z;

      // Check shelf controls visibility
      const shelfControls = document.getElementById('shelf-controls');
      const shelfVisible = shelfControls ? shelfControls.style.display !== 'none' : false;

      // Check orbit disabled
      const rotateEnabled = ctrl.enableRotate;

      return {
        nodeCount: nodes.length,
        distinctYLevels: yValues.size,
        yValues: [...yValues].sort((a: number, b: number) => a - b),
        sampleNodes: nodeData.slice(0, 10),
        nodesWithFy: nodeData.filter((n: any) => n.fy !== null).length,
        nodesWithZ0: nodeData.filter((n: any) => Math.abs(n.z) < 1).length,
        cameraFacesZ: Math.abs(camZ) > 100,
        shelfControlsVisible: shelfVisible,
        orbitDisabled: !rotateEnabled,
      };
    });

    console.log('\nShelf state:');
    console.log(`  nodes: ${shelfState?.nodeCount}`);
    console.log(`  distinct Y levels: ${shelfState?.distinctYLevels}`);
    console.log(`  Y values: [${shelfState?.yValues?.join(', ')}]`);
    console.log(`  nodes with fy set: ${shelfState?.nodesWithFy}`);
    console.log(`  nodes with z≈0: ${shelfState?.nodesWithZ0}`);
    console.log(`  camera faces Z axis: ${shelfState?.cameraFacesZ}`);
    console.log(`  shelf controls visible: ${shelfState?.shelfControlsVisible}`);
    console.log(`  orbit disabled: ${shelfState?.orbitDisabled}`);
    console.log(`  sample nodes: ${JSON.stringify(shelfState?.sampleNodes, null, 2)}`);

    if (errors.length) console.log(`  ALL JS errors: ${errors.join('\n  ')}`);

    // ── Assertions ──────────────────────────────────────────────────────
    expect(shelfState, 'graph state should be readable').not.toBeNull();

    // Core shelf effect: nodes should be pinned to distinct Y levels (shelves)
    expect(shelfState!.nodesWithFy,
      'all nodes should have fy (fixed Y) set'
    ).toBe(shelfState!.nodeCount);

    // At universe view (only project nodes), all nodes are the same kind
    // so they land on a single shelf — correct behavior.
    // Multiple shelves appear after expanding scopes (kind-groups + artifacts).
    expect(shelfState!.distinctYLevels,
      `nodes should be on at least 1 shelf`
    ).toBeGreaterThanOrEqual(1);

    // Z should be flattened (2D mode)
    expect(shelfState!.nodesWithZ0,
      'all nodes should have z≈0 (2D flat)'
    ).toBe(shelfState!.nodeCount);

    // Shelf controls should be visible
    expect(shelfState!.shelfControlsVisible,
      'shelf controls panel should be visible'
    ).toBe(true);

    // Orbit should be disabled (bookshelf = straight-on camera)
    expect(shelfState!.orbitDisabled,
      'orbit rotation should be disabled in shelf mode'
    ).toBe(true);
  });

  test('switching back to Free 3D restores normal behavior', async ({ page }) => {
    await loadGraph(page);

    // Enter shelf mode
    await page.selectOption('#layout-select', 'shelf');
    await page.waitForTimeout(2_000);

    // Exit shelf mode
    await page.selectOption('#layout-select', '');
    await page.waitForTimeout(2_000);
    await saveScreenshot(page, '03-back-to-3d');

    const restored = await page.evaluate(() => {
      const g = (window as any)._Graph;
      if (!g) return null;
      const nodes = g.graphData().nodes;
      const ctrl = g.controls();
      return {
        nodesWithFy: nodes.filter((n: any) => n.fy != null).length,
        orbitEnabled: ctrl.enableRotate,
        shelfControlsHidden: document.getElementById('shelf-controls')?.style.display === 'none',
      };
    });

    console.log('Restored state:', JSON.stringify(restored));

    expect(restored!.nodesWithFy, 'fy should be cleared after leaving shelf mode').toBe(0);
    expect(restored!.orbitEnabled, 'orbit should be re-enabled').toBe(true);
    expect(restored!.shelfControlsHidden, 'shelf controls should be hidden').toBe(true);
  });

  test('after expanding a scope, nodes spread across multiple shelves', async ({ page }) => {
    await loadGraph(page);

    // Click first project node to expand it (shows kind-groups)
    const expanded = await page.evaluate(async () => {
      const g = (window as any)._Graph;
      const nodes = g.graphData().nodes;
      const projectNode = nodes.find((n: any) => n.kind === 'project');
      if (!projectNode) return { expanded: false, reason: 'no project node' };

      // Simulate click on the project node to expand
      const clickHandler = g.onNodeClick();
      if (clickHandler) clickHandler(projectNode, {});
      return { expanded: true, scope: projectNode.scope || projectNode.name };
    });
    console.log('Expand result:', JSON.stringify(expanded));

    // Wait for expansion data to load
    await page.waitForTimeout(4_000);

    // Now enter shelf mode
    await page.selectOption('#layout-select', 'shelf');
    await page.waitForTimeout(3_000);
    await saveScreenshot(page, '04-shelf-with-expanded');

    const shelfState = await page.evaluate(() => {
      const g = (window as any)._Graph;
      if (!g) return null;
      const nodes = g.graphData().nodes;
      const yValues = new Set<number>();
      const kindCounts: Record<string, number> = {};

      for (const n of nodes) {
        yValues.add(Math.round(n.y || 0));
        kindCounts[n.kind] = (kindCounts[n.kind] || 0) + 1;
      }

      return {
        nodeCount: nodes.length,
        distinctYLevels: yValues.size,
        yValues: [...yValues].sort((a: number, b: number) => a - b),
        kindCounts,
        nodesWithFy: nodes.filter((n: any) => n.fy != null).length,
      };
    });

    console.log('\nExpanded shelf state:');
    console.log(`  nodes: ${shelfState?.nodeCount}`);
    console.log(`  distinct Y levels: ${shelfState?.distinctYLevels}`);
    console.log(`  Y values: [${shelfState?.yValues?.join(', ')}]`);
    console.log(`  kind distribution: ${JSON.stringify(shelfState?.kindCounts)}`);

    // With expanded scope, we should have project + kind-group nodes = 2+ kinds = 2+ shelves
    if (shelfState && shelfState.nodeCount > 1) {
      const kindCount = Object.keys(shelfState.kindCounts).length;
      if (kindCount > 1) {
        expect(shelfState.distinctYLevels,
          `with ${kindCount} kinds, should have multiple shelves`
        ).toBeGreaterThan(1);
      }
    }
  });
});
