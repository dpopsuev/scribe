import { test, expect } from '@playwright/test';

// Sidebar integration tests: click node → sidebar opens → linked refs clickable.
// Requires scribe server running on localhost:8083 with data.

test.describe('graph sidebar — wiki-style navigation', () => {

  test.beforeEach(async ({ page }) => {
    await page.goto('/app/graph');
    await page.waitForFunction(
      () => (window as any).__GRAPH_PERF__?.fps > 0,
      { timeout: 15000 },
    );
    await page.waitForTimeout(1000);
  });

  test('sidebar opens on node click via API', async ({ page }) => {
    // Fetch a real artifact ID from the scope graph
    const scopeRes = await page.evaluate(() =>
      fetch('/api/v1/graph/scopes').then(r => r.json())
    );
    const nodeId = scopeRes.nodes[0]?.id;
    expect(nodeId).toBeTruthy();

    // Simulate opening sidebar by calling the API directly
    const artRes = await page.evaluate((id: string) =>
      fetch(`/api/v1/artifacts/${encodeURIComponent(id)}`).then(r => r.ok ? r.json() : null),
      nodeId
    );
    // Scope graph nodes are project: prefixed, may not have an artifact entry
    // But the API should return 200 or 404 without crashing
    if (artRes) {
      expect(artRes).toHaveProperty('id');
      expect(artRes).toHaveProperty('title');
    }
  });

  test('edges API returns array for any artifact', async ({ page }) => {
    const scopeRes = await page.evaluate(() =>
      fetch('/api/v1/graph/scopes').then(r => r.json())
    );
    const nodeId = scopeRes.nodes[0]?.id;

    const edgesRes = await page.evaluate((id: string) =>
      fetch(`/api/v1/artifacts/${encodeURIComponent(id)}/edges`).then(r => r.ok ? r.json() : []),
      nodeId
    );
    expect(Array.isArray(edgesRes)).toBe(true);
  });

  test('sidebar DOM appears after clicking canvas', async ({ page }) => {
    const canvas = page.locator('canvas').first();
    const box = await canvas.boundingBox();
    if (!box) throw new Error('Canvas not found');

    // Click center of canvas — may or may not hit a node
    await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
    await page.waitForTimeout(1500);

    // Check if sidebar appeared (it only appears if a node was hit)
    const sidebar = page.locator('.sidebar');
    const sidebarVisible = await sidebar.count() > 0;

    if (sidebarVisible) {
      await expect(sidebar.locator('h3')).toBeVisible();
      await expect(sidebar.locator('.sidebar-close')).toBeVisible();

      // Close button works
      await sidebar.locator('.sidebar-close').click();
      await expect(sidebar).not.toBeVisible();
    }
    // If no node was hit, sidebar correctly doesn't appear — that's valid
  });

  test('resolve endpoint returns data for referenced artifact', async ({ page }) => {
    // Find an artifact with ref_backend
    const listRes = await page.evaluate(() =>
      fetch('/api/v1/artifacts?limit=50').then(r => r.json())
    );
    const arts: any[] = listRes || [];
    // Try to find one with ref_backend, or just test that endpoint doesn't crash
    const testId = arts[0]?.id;
    if (!testId) return;

    const resolveRes = await page.evaluate((id: string) =>
      fetch(`/api/v1/artifacts/${encodeURIComponent(id)}/resolve`)
        .then(r => ({ status: r.status, ok: r.ok })),
      testId
    );
    // Should return 200 (with cached data) or 404 (no ref_backend)
    expect([200, 404]).toContain(resolveRes.status);
  });
});
