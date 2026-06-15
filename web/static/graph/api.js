/**
 * api.js — Fetch wrappers for the Scribe graph API endpoints.
 *
 * All functions accept a `fetch` parameter so they can be tested
 * without network access (inject a mock fetch in tests).
 *
 * ── API contract ──────────────────────────────────────────────────────────
 * Source of truth: web/graph.go (graphNode / graphLink / graphData structs).
 * Contract test:   web/graph_test.go TestAPIGraphScopes_ContractMatchesFixture
 * JS fixture:      web/static/graph/fixtures/scope-graph.json
 *
 * @typedef {Object} GraphNode
 * @property {string} id         - Unique node ID (e.g. "scope:origami")
 * @property {string} name       - Display name
 * @property {string} kind       - Node kind: "scope" | "kind-group" | artifact kind
 * @property {string} status     - Artifact status (empty for scope super-nodes)
 * @property {string} scope      - Owning scope name
 * @property {string} [group]    - Kind group label (kind-group nodes only)
 * @property {number} val        - Size proxy: artifact count / 20 (min 3)
 * @property {number} violations - Compliance violation count; 0 = compliant
 *
 * @typedef {Object} GraphLink
 * @property {string} source   - Source node ID
 * @property {string} target   - Target node ID
 * @property {string} relation - Edge type: "cross-scope" | "parent_of" | "depends_on" | …
 * @property {number} [weight] - Edge weight (0 = unweighted)
 *
 * @typedef {Object} GraphData
 * @property {GraphNode[]} nodes
 * @property {GraphLink[]} links
 *
 * Endpoints:
 *   GET /api/v1/graph/scopes       → scope super-nodes + cross-scope edges
 *   GET /api/v1/graph/kinds        → kind-group nodes within a scope
 *   GET /api/graph              → artifact nodes within a scope
 *   GET /api/scopes             → list of scope names
 *   POST /api/artifacts         → create artifact
 *   PATCH /api/v1/artifacts/{id}   → set a field
 *   POST /api/v1/edges             → create edge
 *   DELETE /api/v1/edges/{f}/{r}/{t} → delete edge
 */

/** @returns {Promise<GraphData>} */
export async function fetchScopeGraph(fetch, baseURL = '') {
  const res = await fetch(`${baseURL}/api/v1/graph/scopes`);
  if (!res.ok) throw new Error(`/api/v1/graph/scopes: ${res.status}`);
  return res.json();
}

/** @returns {Promise<GraphData>} */
export async function fetchKindGraph(fetch, scope, statuses, relations, baseURL = '') {
  const params = new URLSearchParams({ scope, status: statuses.join(',') });
  if (relations?.length) params.set('relations', relations.join(','));
  const res = await fetch(`${baseURL}/api/v1/graph/kinds?${params}`);
  if (!res.ok) throw new Error(`/api/v1/graph/kinds: ${res.status}`);
  return res.json();
}

/** @returns {Promise<GraphData>} */
export async function fetchArtifactGraph(fetch, scope, statuses, relations, maxNodes = 200, baseURL = '') {
  const params = new URLSearchParams({ scope, status: statuses.join(','), max_nodes: String(maxNodes) });
  if (relations?.length) params.set('relations', relations.join(','));
  const res = await fetch(`${baseURL}/api/v1/graph?${params}`);
  if (!res.ok) throw new Error(`/api/graph: ${res.status}`);
  return res.json();
}

/** @returns {Promise<string[]>} */
export async function fetchScopes(fetch, baseURL = '') {
  const res = await fetch(`${baseURL}/api/v1/scopes`);
  if (!res.ok) throw new Error(`/api/scopes: ${res.status}`);
  return res.json();
}

/**
 * Set a single field on an artifact.
 * Returns the updated field info or throws on error.
 */
export async function patchArtifact(fetch, id, field, value, opts = {}, baseURL = '') {
  const res = await fetch(`${baseURL}/api/v1/artifacts/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ field, value, ...opts }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`PATCH /api/v1/artifacts/${id}: ${res.status} — ${text}`);
  }
  return res.json();
}

/**
 * Create an edge between two artifacts.
 */
export async function createEdge(fetch, from, relation, to, weight = 0, baseURL = '') {
  const res = await fetch(`${baseURL}/api/v1/edges`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ from, relation, to, weight }),
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`POST /api/v1/edges: ${res.status} — ${text}`);
  }
  return res.json();
}

/**
 * Delete an edge between two artifacts.
 */
export async function deleteEdge(fetch, from, relation, to, baseURL = '') {
  const res = await fetch(
    `${baseURL}/api/v1/edges/${encodeURIComponent(from)}/${encodeURIComponent(relation)}/${encodeURIComponent(to)}`,
    { method: 'DELETE' }
  );
  if (!res.ok) throw new Error(`DELETE /api/v1/edges: ${res.status}`);
}
