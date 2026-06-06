/**
 * physics.js — Force simulation geometry for the Scribe graph universe.
 *
 * Pure functions: no DOM, no Three.js, no ForceGraph3D.
 * All functions take explicit arguments — no global state.
 */

// ── Fibonacci sphere ─────────────────────────────────────────────────────────

/**
 * Distributes n points evenly on the surface of a sphere of given radius.
 * Uses the golden angle (2π(1 - 1/φ) ≈ 2.399 rad) to avoid clustering.
 * Points spiral from top (index 0) to bottom (index n-1).
 *
 * Returns an array of { x, y, z } objects.
 */
export function fibonacciSphere(n, radius) {
  if (n <= 0) return [];
  const goldenAngle = Math.PI * (3 - Math.sqrt(5)); // ≈ 2.399 rad
  return Array.from({ length: n }, (_, i) => {
    const y = 1 - (i / Math.max(n - 1, 1)) * 2; // [1, -1] top → bottom
    const r = Math.sqrt(Math.max(0, 1 - y * y));
    const theta = goldenAngle * i;
    return {
      x: Math.cos(theta) * r * radius,
      y: y * radius,
      z: Math.sin(theta) * r * radius,
    };
  });
}

// ── Centre of mass ────────────────────────────────────────────────────────────

/**
 * Weighted centroid of a node array. Weight = node.val (sphere size).
 * Nodes with kind 'scope' or 'kind-group' are the structural parents;
 * passing them as `pool` ensures leaf nodes don't influence the camera target.
 *
 * @param {Array<{x?,y?,z?,val?}>} nodes
 * @returns {{ x: number, y: number, z: number }}
 */
export function weightedCentroid(nodes) {
  if (!nodes || !nodes.length) return { x: 0, y: 0, z: 0 };
  let cx = 0, cy = 0, cz = 0, totalW = 0;
  for (const n of nodes) {
    const w = n.val || 1;
    cx += (n.x || 0) * w;
    cy += (n.y || 0) * w;
    cz += (n.z || 0) * w;
    totalW += w;
  }
  if (totalW === 0) return { x: 0, y: 0, z: 0 };
  return { x: cx / totalW, y: cy / totalW, z: cz / totalW };
}

/**
 * Extracts the parent-level nodes from a full node list.
 * Parent nodes are those with kind 'scope' or 'kind-group'.
 * Falls back to all nodes if no parents exist.
 */
export function parentNodes(nodes) {
  const parents = (nodes || []).filter(
    n => n.kind === 'scope' || n.kind === 'kind-group'
  );
  return parents.length ? parents : (nodes || []);
}

/**
 * Computes the weighted centre of mass using only parent-level nodes.
 * This is the correct anchor point for the camera kite.
 */
export function centerOfMass(nodes) {
  return weightedCentroid(parentNodes(nodes));
}

// ── Radial sphere force ───────────────────────────────────────────────────────

/**
 * Returns a d3-force compatible force function that attracts nodes toward
 * the surface of a sphere of targetRadius. Replaces the 'center' force so
 * nodes float freely within the sphere rather than collapsing to the origin.
 *
 * strength: 0–1, default 0.08 (gentle — preserves the floaty feel).
 */
export function forceRadialSphere(targetRadius, strength = 0.08) {
  let nodes;
  function force(alpha) {
    for (const n of nodes) {
      const d = Math.sqrt((n.x||0)**2 + (n.y||0)**2 + (n.z||0)**2) || 1;
      const scale = (targetRadius - d) / d * alpha * strength;
      n.vx = (n.vx || 0) + (n.x || 0) * scale;
      n.vy = (n.vy || 0) + (n.y || 0) * scale;
      n.vz = (n.vz || 0) + (n.z || 0) * scale;
    }
  }
  force.initialize = ns => { nodes = ns; };
  return force;
}

// ── Equator-priority fibonacci mapping ───────────────────────────────────────

/**
 * Maps n sorted items (highest-weight first) to fibonacci sphere positions
 * such that high-weight items land near the equator (most visible region).
 *
 * Returns an array of { x, y, z } positions corresponding to sortedItems[i].
 */
export function equatorPriorityPositions(n, radius) {
  if (n <= 0) return [];
  const positions = fibonacciSphere(n, radius);
  // Build an interleaved index: 0→middle, 1→middle+1, 2→middle-1, …
  const order = new Array(n);
  let lo = Math.floor(n / 2), hi = lo + 1;
  for (let i = 0; i < n; i++) {
    order[i] = i % 2 === 0 ? lo-- : hi++;
  }
  // Map: item[i] gets position[order[i]]
  return order.map(idx => positions[Math.max(0, Math.min(idx, n - 1))]);
}

// ── Mini-sphere placement ─────────────────────────────────────────────────────

/**
 * Places nodes in a fibonacci mini-sphere centred on anchor,
 * sorted by degree (number of links). Used when expanding a scope or kind.
 *
 * @param {Array} nodes  — node objects with id field
 * @param {Array} links  — link objects with source/target fields
 * @param {{ x, y, z }} anchor  — centre of the mini-sphere
 * @param {number} radius
 * @returns {Array} nodes with x, y, z set
 */
// ── Distance-based node scaling ───────────────────────────────────────────────

/**
 * Scales each node mesh so it subtends exactly targetPx pixels on screen
 * regardless of its distance from the camera (constant angular size).
 *
 * Formula: worldRadius = targetPx * distance * 2 * tan(fov/2) / viewportH
 *   — derived from the pinhole projection: projectedPx = r/d * focalLengthPx
 *     where focalLengthPx = H / (2 * tan(fov/2))
 *
 * @param {Map<string, { mesh: { scale: { setScalar(r: number): void } }, node: { x?, y?, z? } }>} nodeMeshes
 * @param {{ x: number, y: number, z: number }} cameraPos
 * @param {number} targetPx   desired apparent radius in pixels
 * @param {number} fovRad     vertical field-of-view in radians
 * @param {number} viewportH  viewport height in pixels
 */
export function scaleNodesByDistance(nodeMeshes, cameraPos, targetPx, fovRad, viewportH) {
  const unitScale = (targetPx * 2 * Math.tan(fovRad / 2)) / viewportH;
  for (const [, { mesh, node }] of nodeMeshes) {
    const dx = (node.x || 0) - cameraPos.x;
    const dy = (node.y || 0) - cameraPos.y;
    const dz = (node.z || 0) - cameraPos.z;
    const distance = Math.hypot(dx, dy, dz) || 1;
    mesh.scale.setScalar(unitScale * distance);
  }
}

export function placeInMiniSphere(nodes, links, anchor, radius) {
  // Compute degree per node.
  const deg = {};
  for (const l of links) {
    deg[l.source] = (deg[l.source] || 0) + 1;
    deg[l.target] = (deg[l.target] || 0) + 1;
  }
  const sorted = [...nodes].sort((a, b) => (deg[b.id] || 0) - (deg[a.id] || 0));
  const pts = fibonacciSphere(sorted.length, radius);
  sorted.forEach((n, i) => {
    n.x = anchor.x + pts[i].x;
    n.y = anchor.y + pts[i].y;
    n.z = anchor.z + pts[i].z;
  });
  return sorted;
}
