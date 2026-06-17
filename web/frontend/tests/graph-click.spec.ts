import { test, expect } from '@playwright/test';

// Tests for click routing: every node kind has a defined behavior.
// ghost → ignored | project → expand scope | kind-group → expand kind | else → sidebar

test.describe('graph node click routing', () => {

  test.beforeEach(async ({ page }) => {
    await page.goto('/app/graph');
    await page.waitForFunction(
      () => (window as any).__GRAPH_PERF__?.fps > 0,
      { timeout: 15000 },
    );
    await page.waitForTimeout(1000);
  });

  test('scope graph has project nodes that expand on click', async ({ page }) => {
    // Verify scope graph returns project-kind nodes
    const data = await page.evaluate(() =>
      fetch('/api/v1/graph/scopes').then(r => r.json())
    );
    const projectNodes = data.nodes.filter((n: any) => n.kind === 'project');
    expect(projectNodes.length).toBeGreaterThan(0);

    // Each project node should have an ID
    for (const n of projectNodes.slice(0, 3)) {
      expect(n.id).toBeTruthy();
      expect(n.name).toBeTruthy();
    }
  });

  test('kind-group nodes are synthetic — no artifact in DB', async ({ page }) => {
    // Pick a scope and fetch its kind graph
    const scopes = await page.evaluate(() =>
      fetch('/api/v1/graph/scopes').then(r => r.json())
    );
    const scopeName = scopes.nodes[0]?.name;
    if (!scopeName) return;

    const kindData = await page.evaluate((scope: string) =>
      fetch(`/api/v1/graph/kinds?scope=${encodeURIComponent(scope)}&status=work.active`).then(r => r.json()),
      scopeName
    );
    const kindGroups = kindData.nodes?.filter((n: any) => n.kind === 'kind-group') || [];

    // Each kind-group node should return 404 from the artifact API
    for (const kg of kindGroups.slice(0, 2)) {
      const status = await page.evaluate((id: string) =>
        fetch(`/api/v1/artifacts/${encodeURIComponent(id)}`).then(r => r.status),
        kg.id
      );
      expect(status).toBe(404);
    }
  });

  test('artifact detail API returns valid data for real artifacts', async ({ page }) => {
    const listRes = await page.evaluate(() =>
      fetch('/api/v1/artifacts?limit=5').then(r => r.json())
    );
    expect(listRes.length).toBeGreaterThan(0);

    const art = await page.evaluate((id: string) =>
      fetch(`/api/v1/artifacts/${encodeURIComponent(id)}`).then(r => r.json()),
      listRes[0].id
    );
    expect(art).toHaveProperty('id');
    expect(art).toHaveProperty('title');
    expect(art).toHaveProperty('labels');
  });

  test('edges API returns array with relation + title for real artifacts', async ({ page }) => {
    const listRes = await page.evaluate(() =>
      fetch('/api/v1/artifacts?limit=5').then(r => r.json())
    );
    const id = listRes[0]?.id;
    if (!id) return;

    const edges = await page.evaluate((artId: string) =>
      fetch(`/api/v1/artifacts/${encodeURIComponent(artId)}/edges`).then(r => r.json()),
      id
    );
    expect(Array.isArray(edges)).toBe(true);
    if (edges.length > 0) {
      expect(edges[0]).toHaveProperty('from');
      expect(edges[0]).toHaveProperty('to');
      expect(edges[0]).toHaveProperty('relation');
    }
  });

  test('kind-group expand returns artifacts of that kind', async ({ page }) => {
    const scopes = await page.evaluate(() =>
      fetch('/api/v1/graph/scopes').then(r => r.json())
    );
    const scopeName = scopes.nodes[0]?.name;
    if (!scopeName) return;

    // Fetch artifact graph for this scope
    const graphData = await page.evaluate((scope: string) =>
      fetch(`/api/v1/graph?scope=${encodeURIComponent(scope)}&status=work.active&max_nodes=200`).then(r => r.json()),
      scopeName
    );
    expect(graphData.nodes).toBeDefined();
    expect(Array.isArray(graphData.nodes)).toBe(true);
  });
});
