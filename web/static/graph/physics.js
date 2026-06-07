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
export const GRAVITY_MIN   = 0.30;  // minimum safe G with LJ sigma=20, epsilon=0.002, softening=5
export const GRAVITY_MAX   = 0.80;  // maximum useful G — above this cluster over-compresses
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
  // Gravity rises quadratically from floor to ceiling as camera zooms out.
  // Repulsion and reach fall linearly, pulling nodes tighter at distance.
  const GRAVITY_FLOOR     = 0.01;  // minimum gravity — nodes float when zoomed in
  const GRAVITY_SWING     = 0.40;  // range above floor gravity covers (floor + swing = near GRAVITY_MAX)
  const REPULSION_NEAR    = 250;   // repulsion magnitude when zoomed in (spread out)
  const REPULSION_SWING   = 220;   // how much repulsion drops as camera zooms out
  const DMAX_NEAR         = 600;   // repulsion reach when zoomed in
  const DMAX_SWING        = 550;   // how much reach shrinks as camera zooms out
  const G    = GRAVITY_FLOOR   + GRAVITY_SWING   * t * t;
  const rep  = -(REPULSION_NEAR - REPULSION_SWING * t * t);
  const dmax = DMAX_NEAR       - DMAX_SWING       * t;
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

const COUNT_RADIUS_SCALE = 80; // world units at baseline of 10 nodes

/**
 * Maximum cluster radius for n nodes — logarithmic so more nodes get
 * slightly more room without the cluster growing linearly:
 *   n=10  → 80   (baseline)
 *   n=100 → 160  (10× nodes → 2× radius)
 *   n=1000→ 240  (100× nodes → 3× radius)
 */
export function clusterMaxRadius(n) {
  return COUNT_RADIUS_SCALE * Math.max(1, Math.log10(Math.max(n, 10) / 10) + 1);
}

// Calibrated so that 87 nodes with avg nodeVal≈4.3 (val=10 each) gives radius≈155,
// matching clusterMaxRadius(87) for continuity with the count-based formula.
const VOLUME_RADIUS_SCALE = 21.5;

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
  return VOLUME_RADIUS_SCALE * Math.cbrt(Math.max(1, totalNodeVolume));
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



/**
 * Pairwise N-body gravity — each node attracts every other node with force
 * proportional to the ATTRACTOR's mass (not the pulled node's mass).
 *
 *   a_i += G · α · mass_j / (r_ij² + ε²) · unit(j→i)   for all j ≠ i
 *
 * Because the attractor mass scales the force, heavy nodes pull everything
 * toward themselves strongly. Nodes sort by mass from center outward —
 * the heaviest cluster at the core, the lightest at the periphery.
 *
 * O(n²) pairs per tick — fine for the graphs this codebase handles (≤ 300 nodes).
 * Plummer softening ε prevents the force from diverging when nodes overlap.
 */
/**
 * Centripetal cohesion — pulls every node toward the origin with force G/r.
 *
 * This is a 1/r force (not 1/r²), so it remains effective at the initial
 * placement radius (UNIVERSE_RADIUS=180) and brings nodes into the cluster
 * region within a few hundred simulation ticks.  Called with no massKey so
 * every node gets the same pull (mass=1) — N-body gravity handles stratification.
 */
export function forceSelfGravity(initialG = 0.15, softening = 30, massKey) {
  let G = initialG;
  let nodes;
  function force(alpha) {
    for (const n of nodes) {
      const mass = massKey ? Math.max(1, n[massKey] || 1) : 1;
      const x = n.x || 0, y = n.y || 0, z = n.z || 0;
      const r2 = x*x + y*y + z*z + softening*softening;
      const k  = G * alpha * mass / Math.sqrt(r2);
      n.vx = (n.vx || 0) - x * k;
      n.vy = (n.vy || 0) - y * k;
      n.vz = (n.vz || 0) - z * k;
    }
  }
  force.initialize = ns => { nodes = ns; };
  force.setG = newG => { G = newG; };
  return force;
}

/**
 * Lennard-Jones repulsion — the r^-12 term of the LJ potential.
 *
 * Prior art: Lennard-Jones 12-6 potential (1931), canonical model for
 * intermolecular repulsion due to Pauli exclusion at close range.
 * Unlike Coulombic r^-2 repulsion (our old manyBody charge), this is
 * effectively ZERO beyond ~2σ — no long-range pressure, no hollow shell.
 *
 *   F_rep = 48·ε·(σ^12/r^13)   [repulsive, pointing away]
 *
 * σ (sigma): equilibrium separation — nodes settle ~σ apart.
 *            Set to ≈ 2× average node sphere radius.
 * ε (epsilon): well depth — controls how hard nodes push apart at contact.
 *
 * Cutoff at r = 2σ: beyond that (σ/r)^12 < 0.00024, negligible.
 */
export function forceLennardJonesRepulsion(sigma = 20, epsilon = 0.008) {
  let nodes = [];
  let sig = sigma, eps = epsilon;
  function force(alpha) {
    const n    = nodes.length;
    const cut2 = (2 * sig) * (2 * sig); // skip pairs beyond 2σ
    const s6   = Math.pow(sig, 6);
    const s12  = s6 * s6;
    for (let i = 0; i < n; i++) {
      const ni = nodes[i];
      for (let j = i + 1; j < n; j++) {
        const nj = nodes[j];
        const dx = (nj.x || 0) - (ni.x || 0);
        const dy = (nj.y || 0) - (ni.y || 0);
        const dz = (nj.z || 0) - (ni.z || 0);
        const r2 = dx*dx + dy*dy + dz*dz;
        if (r2 === 0 || r2 > cut2) continue;
        // F = 48·ε·σ^12/r^14 × r_vec   (from -dV/dr, repulsive direction)
        const r2_inv = 1 / (r2 || 1e-6);
        const r6_inv = r2_inv * r2_inv * r2_inv;
        const f = 48 * eps * s12 * r6_inv * r6_inv * r2_inv * alpha;
        ni.vx = (ni.vx || 0) - dx * f;
        ni.vy = (ni.vy || 0) - dy * f;
        ni.vz = (ni.vz || 0) - dz * f;
        nj.vx = (nj.vx || 0) + dx * f;
        nj.vy = (nj.vy || 0) + dy * f;
        nj.vz = (nj.vz || 0) + dz * f;
      }
    }
  }
  force.initialize    = ns  => { nodes = ns; };
  force.setSigma      = s   => { sig = s; };
  force.setEpsilon    = e   => { eps = e; };
  return force;
}

export function forceNBodyGravity(initialG = 0.15, softening = 40, massKey = 'val') {
  let G = initialG;
  let nodes = [];
  function force(alpha) {
    const n = nodes.length;
    for (let i = 0; i < n; i++) {
      const ni = nodes[i];
      const mi = Math.max(1, ni[massKey] || 1);
      for (let j = i + 1; j < n; j++) {
        const nj = nodes[j];
        const mj = Math.max(1, nj[massKey] || 1);
        const dx = (nj.x || 0) - (ni.x || 0);
        const dy = (nj.y || 0) - (ni.y || 0);
        const dz = (nj.z || 0) - (ni.z || 0);
        const r2 = dx*dx + dy*dy + dz*dz + softening*softening;
        // f = G·α / r³  (Plummer softening folds ε into r2 so no separate /r term needed)
        const f = G * alpha / (r2 * Math.sqrt(r2));
        // Acceleration on i toward j scales with j's mass; vice-versa.
        ni.vx = (ni.vx || 0) + dx * f * mj;
        ni.vy = (ni.vy || 0) + dy * f * mj;
        ni.vz = (ni.vz || 0) + dz * f * mj;
        nj.vx = (nj.vx || 0) - dx * f * mi;
        nj.vy = (nj.vy || 0) - dy * f * mi;
        nj.vz = (nj.vz || 0) - dz * f * mi;
      }
    }
  }
  force.initialize = ns => { nodes = ns; };
  // Same interface as forceSelfGravity — zoom adaptor calls setG in-place.
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
 * Energy minimization pre-pass — resolves node overlaps before the d3 simulation starts.
 *
 * Prior art: standard MD practice of "energy minimization before dynamics" (steepest
 * descent position correction). Avoids the crash-through instability that occurs when
 * nodes arrive at contact with kinetic energy > LJ potential barrier.
 *
 * Each iteration pushes overlapping pairs apart by half the overlap (position-based,
 * no velocity). After `iterations` passes, all pairs are ≥ sigma apart.
 * O(n²) per iteration but runs once synchronously — trivial for n ≤ 300.
 *
 * @param {Array} nodes      — graph nodes with x, y, z properties (modified in-place)
 * @param {number} sigma     — minimum allowed separation (LJ_SIGMA)
 * @param {number} iterations — number of correction passes (50 is sufficient for n ≤ 200)
 */
export function presettleNodes(nodes, sigma, iterations = 50) {
  const n = nodes.length;
  const cut2 = sigma * sigma;
  for (let iter = 0; iter < iterations; iter++) {
    let anyOverlap = false;
    for (let i = 0; i < n; i++) {
      const ni = nodes[i];
      for (let j = i + 1; j < n; j++) {
        const nj = nodes[j];
        const dx = (nj.x || 0) - (ni.x || 0);
        const dy = (nj.y || 0) - (ni.y || 0);
        const dz = (nj.z || 0) - (ni.z || 0);
        const r2 = dx*dx + dy*dy + dz*dz;
        if (r2 >= cut2 || r2 === 0) continue;
        anyOverlap = true;
        const r  = Math.sqrt(r2);
        const push = (sigma - r) / (2 * r); // half of overlap, normalised
        ni.x = (ni.x || 0) - dx * push;
        ni.y = (ni.y || 0) - dy * push;
        ni.z = (ni.z || 0) - dz * push;
        nj.x = (nj.x || 0) + dx * push;
        nj.y = (nj.y || 0) + dy * push;
        nj.z = (nj.z || 0) + dz * push;
      }
    }
    if (!anyOverlap) break; // converged early
  }
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
