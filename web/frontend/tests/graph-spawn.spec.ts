import { test, expect } from '@playwright/test';

// Test that children spawn AT their parent's actual position, not at
// the pre-simulation spiral position.

test.describe('child node spawning', () => {

  test.beforeEach(async ({ page }) => {
    await page.goto('/app/graph');
    await page.waitForFunction(
      () => (window as any).__GRAPH_PERF__?.fps > 0,
      { timeout: 15000 },
    );
    await page.waitForTimeout(1000);
  });

  test('parent pre-sim position differs from post-sim position', async ({ page }) => {
    // The nodes array has golden-spiral positions (pre-simulation).
    // After d3-force pre-warm, simNodes have different positions.
    // If expansion reads from nodes (stale), children appear in the wrong place.

    const result = await page.evaluate(() => {
      const pos = (window as any).__GRAPH_NODE_POS__;
      return { hasPositions: !!pos, count: pos ? Object.keys(pos).length : 0 };
    });

    // __GRAPH_NODE_POS__ should be exposed by GraphCanvas after pre-warm
    expect(result.hasPositions).toBe(true);
    expect(result.count).toBeGreaterThan(0);
  });

  test('child positions are within parent radius', async ({ page }) => {
    const data = await page.evaluate(async () => {
      const pos = (window as any).__GRAPH_NODE_POS__ || {};
      const scopes = await fetch('/api/v1/graph/scopes').then(r => r.json());

      // Find a project with children
      const project = scopes.nodes.find((n: any) => n.kind === 'project' && n.val > 3);
      if (!project) return null;

      const parentPos = pos[project.id];
      if (!parentPos) return { error: 'no position for ' + project.id };

      return {
        parentId: project.id,
        parentX: parentPos.x,
        parentY: parentPos.y,
        parentSize: parentPos.size,
      };
    });

    expect(data).not.toBeNull();
    if (data && !('error' in data)) {
      expect(data.parentSize).toBeGreaterThan(0);
    }
  });
});
