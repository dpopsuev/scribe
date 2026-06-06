/**
 * api.js — Fetch wrappers for the Scribe graph API endpoints.
 *
 * All functions accept a `fetch` parameter so they can be tested
 * without network access (inject a mock fetch in tests).
 *
 * Endpoints:
 *   GET /api/graph/scopes       → scope super-nodes + cross-scope edges
 *   GET /api/graph/kinds        → kind-group nodes within a scope
 *   GET /api/graph              → artifact nodes within a scope
 *   GET /api/scopes             → list of scope names
 *   POST /api/artifacts         → create artifact
 *   PATCH /api/artifacts/{id}   → set a field
 *   POST /api/edges             → create edge
 *   DELETE /api/edges/{f}/{r}/{t} → delete edge
 */

/**
 * Fetch the scope-level graph (universe view).
 * Returns { nodes, links } for all scopes with cross-scope edges.
 */
export async function fetchScopeGraph(fetch, baseURL = '') {
  const res = await fetch(`${baseURL}/api/graph/scopes`);
  if (!res.ok) throw new Error(`/api/graph/scopes: ${res.status}`);
  return res.json();
}

/**
 * Fetch kind-group nodes within a scope (depth 1 expansion).
 */
export async function fetchKindGraph(fetch, scope, statuses, relations, baseURL = '') {
  const params = new URLSearchParams({ scope, status: statuses.join(',') });
  if (relations?.length) params.set('relations', relations.join(','));
  const res = await fetch(`${baseURL}/api/graph/kinds?${params}`);
  if (!res.ok) throw new Error(`/api/graph/kinds: ${res.status}`);
  return res.json();
}

/**
 * Fetch artifact nodes within a scope (depth 2 expansion).
 */
export async function fetchArtifactGraph(fetch, scope, statuses, relations, baseURL = '') {
  const params = new URLSearchParams({ scope, status: statuses.join(',') });
  if (relations?.length) params.set('relations', relations.join(','));
  const res = await fetch(`${baseURL}/api/graph?${params}`);
  if (!res.ok) throw new Error(`/api/graph: ${res.status}`);
  return res.json();
}

/**
 * Fetch the list of scope names.
 */
export async function fetchScopes(fetch, baseURL = '') {
  const res = await fetch(`${baseURL}/api/scopes`);
  if (!res.ok) throw new Error(`/api/scopes: ${res.status}`);
  return res.json();
}

/**
 * Set a single field on an artifact.
 * Returns the updated field info or throws on error.
 */
export async function patchArtifact(fetch, id, field, value, opts = {}, baseURL = '') {
  const res = await fetch(`${baseURL}/api/artifacts/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ field, value, ...opts }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`PATCH /api/artifacts/${id}: ${res.status} — ${text}`);
  }
  return res.json();
}

/**
 * Create an edge between two artifacts.
 */
export async function createEdge(fetch, from, relation, to, weight = 0, baseURL = '') {
  const res = await fetch(`${baseURL}/api/edges`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ from, relation, to, weight }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`POST /api/edges: ${res.status} — ${text}`);
  }
  return res.json();
}

/**
 * Delete an edge between two artifacts.
 */
export async function deleteEdge(fetch, from, relation, to, baseURL = '') {
  const res = await fetch(
    `${baseURL}/api/edges/${encodeURIComponent(from)}/${encodeURIComponent(relation)}/${encodeURIComponent(to)}`,
    { method: 'DELETE' }
  );
  if (!res.ok) throw new Error(`DELETE /api/edges: ${res.status}`);
}
