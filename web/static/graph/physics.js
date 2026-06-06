/**
 * physics.js — Force simulation geometry for the Scribe graph universe.
 *
 * Pure functions: no DOM, no Three.js, no ForceGraph3D.
 * All functions take explicit arguments — no global state.
 */

/**
 * Gravitational well toward origin with Plummer softening.
 *
 * Implements: a_i = G * mass_i / (r_i² + ε²)^(3/2) · r_i
 *
 * Combined with short-range repulsion (manyBody negative + distanceMax),
 * produces a self-gravitating cluster: dense core, sparse periphery —
 * the equilibrium seen in star clusters and galactic halos.
 * ε (softening) prevents infinite force when two nodes overlap.
 */
/**
 * Returns force parameters for zoom-adaptive clustering.
 * Maps camera distance to gravity/repulsion values using log-space interpolation.
 * t=0 (zoomed in, dist≈150): weak gravity, strong repulsion → spread.
 * t=1 (zoomed out, dist≈3000): strong gravity, weak repulsion → tight.
 */
/**
 * Returns force parameters for the given camera distance, or null if the
 * change from lastDist is below the sensitivity dead zone.
 *
 * @param {number} dist       current smoothed camera distance
 * @param {number} minDist    closest expected zoom
 * @param {number} maxDist    farthest expected zoom
 * @param {number} sensitivity fractional dead zone — skip update if |Δdist/lastDist| < sensitivity
 * @param {number|null} lastDist  distance at which forces were last applied (null = first call)
 * @returns {{ G, rep, dmax } | null}
 */
// Bounds for zoom-adaptive clustering. Values outside these distances clamp.
export const ZOOM_MIN_DIST = 150;   // world units — closest expected camera distance
export const ZOOM_MAX_DIST = 3000;  // world units — farthest expected camera distance

// Force parameter bounds — capped so physics never produces pathological states.
export const GRAVITY_MIN   = 0.01;  // near-zero gravity → nodes float freely
export const GRAVITY_MAX   = 0.41;  // strong gravity → tight cluster
export const REPULSION_MIN = -250;  // strong repulsion → large spread radius
export const REPULSION_MAX = -30;   // weak repulsion  → small spread radius
export const DMAX_MIN      = 50;    // world units — repulsion only at very close range
export const DMAX_MAX      = 600;   // world units — repulsion acts across full cluster

export function forcesForDist(dist, minDist = ZOOM_MIN_DIST, maxDist = ZOOM_MAX_DIST, sensitivity = 0.05, lastDist = undefined) {
  // Dead zone: if change is below sensitivity, caller should skip the update.
  if (lastDist != null && Math.abs(dist - lastDist) / lastDist < sensitivity) {
    return null;
  }
  const t = Math.max(0, Math.min(1,
    Math.log(dist / minDist) / Math.log(maxDist / minDist),
  ));
  // t=0 (zoomed in):  weak gravity + strong repulsion → loose/large cluster
  // t=1 (zoomed out): strong gravity + weak repulsion → tight/small cluster
  const G    = 0.01 + 0.4  * t * t;
  const rep  = -(250 - 220 * t * t);
  const dmax = 600  - 550 * t;
  return {
    G:    Math.max(GRAVITY_MIN,   Math.min(GRAVITY_MAX,   G)),
    rep:  Math.max(REPULSION_MIN, Math.min(REPULSION_MAX, rep)),
    dmax: Math.max(DMAX_MIN,      Math.min(DMAX_MAX,      dmax)),
  };
}

// ── Camera fit distance ───────────────────────────────────────────────────

/**
 * Camera distance required to show a cluster of radius R inside the FOV
 * with the given padding factor.
 *
 * Derived from: fill = 2·atan(R/D) / FOV
 * Rearranged:   D = R / tan(FOV/2) · padding
 *
 * @param {number} R          cluster bounding radius (world units)
 * @param {number} fovDeg     camera vertical FOV in degrees
 * @param {number} padding    breathing-room multiplier (1.0 = exactly fits)
 */
export function computeFitDistance(R, fovDeg = 75, padding = 1.25) {
  const halfFovRad = fovDeg / 2 * Math.PI / 180;
  return (R / Math.tan(halfFovRad)) * padding;
}

/**
 * Camera distance using clusterMaxRadius(n) as the reference — not actual
 * node positions. Ensures boot and idle produce the same camera placement
 * regardless of transient physics state.
 */
export function computeFitDistanceForCount(n, fovDeg = 75, padding = 1.25) {
  return computeFitDistance(clusterMaxRadius(n), fovDeg, padding);
}

/**
 * Camera distance using clusterRadiusFromVolume(totalNodeVolume).
 * totalNodeVolume = sum(nodeVal for all nodes); same formula as renderer.
 */
export function computeFitDistanceForVolume(totalNodeVolume, fovDeg = 75, padding = 1.25) {
  return computeFitDistance(clusterRadiusFromVolume(totalNodeVolume), fovDeg, padding);
}

// ── Cluster radius cap ────────────────────────────────────────────────────

const BASE_CLUSTER_RADIUS = 80; // world units at 10 nodes

/**
 * Maximum cluster radius for n nodes — logarithmic so more nodes get
 * slightly more room without the cluster growing linearly:
 *   n=10  → 80   (baseline)
 *   n=100 → 160  (10× nodes → 2× radius)
 *   n=1000→ 240  (100× nodes → 3× radius)
 */
export function clusterMaxRadius(n) {
  return BASE_CLUSTER_RADIUS * Math.max(1, Math.log10(Math.max(n, 10) / 10) + 1);
}

// Calibrated so that 87 nodes with avg nodeVal≈4.3 (val=10 each) gives radius≈155,
// matching clusterMaxRadius(87) for continuity.
const VOLUME_CLUSTER_BASE = 21.5;

/**
 * Cluster radius from total node visual volume — the sum of nodeVal across all
 * nodes, where nodeVal uses the same formula as the renderer:
 *   nodeVal(n) = clamp(cbrt(n.val || 1) * 2, NODE_SIZE_MIN, NODE_SIZE_MAX)
 *
 * Scales as cbrt(totalVolume): doubling total node volume grows the cluster
 * radius by 26%. Larger or heavier nodes → bigger cluster → camera further out.
 * Both the force cap radius and the idle camera distance use this value, so
 * zoom level and cluster tightness are always derived from the same input.
 */
export function clusterRadiusFromVolume(totalNodeVolume) {
  return VOLUME_CLUSTER_BASE * Math.cbrt(Math.max(1, totalNodeVolume));
}

/**
 * Soft radius cap: pulls nodes toward origin only when they exceed maxRadius.
 * Inside the cap: zero force. Outside: restoring force proportional to excess.
 * Prevents repulsion from scattering nodes indefinitely.
 */
export function forceRadiusCap(maxRadius, strength = 0.15) {
  let cap   = maxRadius;
  let nodes = [];
  function force(alpha) {
    for (const n of nodes) {
      const x = n.x || 0, y = n.y || 0, z = n.z || 0;
      const r = Math.hypot(x, y, z);
      if (r <= cap) continue;
      const k = strength * alpha * (r - cap) / r;
      n.vx = (n.vx || 0) - x * k;
      n.vy = (n.vy || 0) - y * k;
      n.vz = (n.vz || 0) - z * k;
    }
  }
  force.initialize  = ns  => { nodes = ns; };
  force.setMaxRadius = r  => { cap   = r; };
  return force;
}

export function forceSelfGravity(initialG = 0.15, softening = 30, massKey = 'val') {
  let G = initialG;
  let nodes;
  function force(alpha) {
    for (const n of nodes) {
      const mass = Math.max(1, n[massKey] || 1);
      const x = n.x || 0, y = n.y || 0, z = n.z || 0;
      const r2 = x*x + y*y + z*z + softening*softening;
      const k  = G * alpha * mass / Math.sqrt(r2);
      n.vx = (n.vx || 0) - x * k;
      n.vy = (n.vy || 0) - y * k;
      n.vz = (n.vz || 0) - z * k;
    }
  }
  force.initialize = ns => { nodes = ns; };
  // In-place update — avoids re-registering the force on every animation frame.
  force.setG = newG => { G = newG; };
  return force;
}


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
