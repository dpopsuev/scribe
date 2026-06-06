import { describe, it, expect, vi } from 'vitest';
import {
  fetchScopeGraph, fetchKindGraph, fetchArtifactGraph,
  fetchScopes, patchArtifact, createEdge, deleteEdge,
} from './api.js';

// ── Mock fetch factory ────────────────────────────────────────────────────────

function mockFetch(body, status = 200) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
    text: () => Promise.resolve(JSON.stringify(body)),
  });
}

// ── fetchScopeGraph ───────────────────────────────────────────────────────────

describe('fetchScopeGraph', () => {
  it('calls /api/graph/scopes', async () => {
    const fetch = mockFetch({ nodes: [], links: [] });
    await fetchScopeGraph(fetch);
    expect(fetch).toHaveBeenCalledWith('/api/graph/scopes');
  });

  it('returns parsed JSON', async () => {
    const data = { nodes: [{ id: 'scope:alpha' }], links: [] };
    const result = await fetchScopeGraph(mockFetch(data));
    expect(result).toEqual(data);
  });

  it('throws on non-OK response', async () => {
    await expect(fetchScopeGraph(mockFetch({}, 500))).rejects.toThrow('500');
  });

  it('respects baseURL', async () => {
    const fetch = mockFetch({});
    await fetchScopeGraph(fetch, 'http://localhost:8080');
    expect(fetch).toHaveBeenCalledWith('http://localhost:8080/api/graph/scopes');
  });
});

// ── fetchKindGraph ────────────────────────────────────────────────────────────

describe('fetchKindGraph', () => {
  it('builds correct URL with scope and statuses', async () => {
    const fetch = mockFetch({ nodes: [], links: [] });
    await fetchKindGraph(fetch, 'scribe', ['active', 'draft'], []);
    const url = fetch.mock.calls[0][0];
    expect(url).toContain('/api/graph/kinds');
    expect(url).toContain('scope=scribe');
    expect(url).toContain('status=active%2Cdraft');
  });

  it('includes relations when provided', async () => {
    const fetch = mockFetch({ nodes: [], links: [] });
    await fetchKindGraph(fetch, 'scribe', ['active'], ['parent_of', 'depends_on']);
    expect(fetch.mock.calls[0][0]).toContain('relations=parent_of');
  });

  it('omits relations param when empty', async () => {
    const fetch = mockFetch({ nodes: [], links: [] });
    await fetchKindGraph(fetch, 'scribe', ['active'], []);
    expect(fetch.mock.calls[0][0]).not.toContain('relations');
  });
});

// ── fetchArtifactGraph ────────────────────────────────────────────────────────

describe('fetchArtifactGraph', () => {
  it('calls /api/graph with scope param', async () => {
    const fetch = mockFetch({ nodes: [], links: [] });
    await fetchArtifactGraph(fetch, 'parchment', ['active'], []);
    expect(fetch.mock.calls[0][0]).toContain('/api/graph');
    expect(fetch.mock.calls[0][0]).toContain('scope=parchment');
  });
});

// ── fetchScopes ───────────────────────────────────────────────────────────────

describe('fetchScopes', () => {
  it('returns scope list', async () => {
    const scopes = ['alpha', 'beta', 'gamma'];
    const result = await fetchScopes(mockFetch(scopes));
    expect(result).toEqual(scopes);
  });

  it('calls /api/scopes', async () => {
    const fetch = mockFetch([]);
    await fetchScopes(fetch);
    expect(fetch).toHaveBeenCalledWith('/api/scopes');
  });
});

// ── patchArtifact ─────────────────────────────────────────────────────────────

describe('patchArtifact', () => {
  it('sends PATCH with correct body', async () => {
    const fetch = mockFetch({ id: 'T-1', field: 'status', value: 'active' });
    await patchArtifact(fetch, 'T-1', 'status', 'active');
    expect(fetch).toHaveBeenCalledWith('/api/artifacts/T-1', expect.objectContaining({
      method: 'PATCH',
      body: expect.stringContaining('"status"'),
    }));
  });

  it('includes rename_id in body when passed as option', async () => {
    const fetch = mockFetch({ id: 'SCR-SPC-1', new_id: 'ALE-SPC-1' });
    await patchArtifact(fetch, 'SCR-SPC-1', 'scope', 'alef', { rename_id: true });
    const body = JSON.parse(fetch.mock.calls[0][1].body);
    expect(body.rename_id).toBe(true);
    expect(body.field).toBe('scope');
    expect(body.value).toBe('alef');
  });

  it('throws on error response', async () => {
    await expect(
      patchArtifact(mockFetch('not found', 404), 'X', 'status', 'active')
    ).rejects.toThrow('404');
  });

  it('encodes special characters in ID', async () => {
    const fetch = mockFetch({});
    await patchArtifact(fetch, 'A B/C', 'title', 'x');
    expect(fetch.mock.calls[0][0]).toContain('A%20B%2FC');
  });
});

// ── createEdge ────────────────────────────────────────────────────────────────

describe('createEdge', () => {
  it('sends POST with edge data', async () => {
    const fetch = mockFetch({});
    await createEdge(fetch, 'T-1', 'implements', 'SPEC-1');
    const body = JSON.parse(fetch.mock.calls[0][1].body);
    expect(body).toEqual({ from: 'T-1', relation: 'implements', to: 'SPEC-1', weight: 0 });
  });

  it('uses provided weight', async () => {
    const fetch = mockFetch({});
    await createEdge(fetch, 'A', 'depends_on', 'B', 0.8);
    const body = JSON.parse(fetch.mock.calls[0][1].body);
    expect(body.weight).toBe(0.8);
  });
});

// ── deleteEdge ────────────────────────────────────────────────────────────────

describe('deleteEdge', () => {
  it('sends DELETE to correct URL', async () => {
    const fetch = mockFetch(null, 204);
    await deleteEdge(fetch, 'T-1', 'implements', 'SPEC-1');
    expect(fetch.mock.calls[0][0]).toBe('/api/edges/T-1/implements/SPEC-1');
    expect(fetch.mock.calls[0][1].method).toBe('DELETE');
  });

  it('encodes special characters in from/relation/to', async () => {
    const fetch = mockFetch(null, 204);
    await deleteEdge(fetch, 'A B', 'parent_of', 'C/D');
    expect(fetch.mock.calls[0][0]).toContain('A%20B');
    expect(fetch.mock.calls[0][0]).toContain('C%2FD');
  });
});
