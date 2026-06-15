/**
 * graph.js — Entry point for the Scribe 3D graph UI.
 *
 * Wires all extracted modules into a ForceGraph3D instance.
 * CDN globals (ForceGraph3D, THREE, culori, htmx) are injected via deps
 * so this module can be tested without a browser.
 *
 * Usage in graph.html:
 *   <script type="module">
 *     import { initGraph } from '/static/graph/graph.js';
 *     initGraph({ ForceGraph3D: window.ForceGraph3D, culori: window.culori, htmx: window.htmx });
 *   </script>
 */

import { buildPalette }                         from './palette.js';
import { centerOfMass, parentNodes,
         placeInMiniSphere,
         equatorPriorityPositions,
         forceCentripetal,
         forceNBodyGravity,
         forcesForDist,
         clusterRadiusFromVolume,
         forceRadiusCap,
         ZOOM_MIN_DIST,
         ZOOM_MAX_DIST }                         from './physics.js';
import { glowColor, glowConfig, glowOpacity }  from './glow.js';
import { fetchScopeGraph, fetchKindGraph,
         fetchArtifactGraph,
         patchArtifact }                        from './api.js';
import { setModeBadge, depthFromExpanded, setStats,
         renderExpandedTags, openSidebar, closeSidebar,
         showContextMenu }                       from './ui.js';
import { KindColorRenderer, NODE_SIZE_MIN, NODE_SIZE_MAX, SPHERE_SCALE, nodeVal } from './renderer.js';
import { createLogger }                        from './logger.js';
import { createGraphState }                    from './graph-state.js';

const log = createLogger('graph');

const GRAPH_BG             = '#05050f';

const KIND_SPHERE_RADIUS   = 55;    // world units — kind-group mini-sphere radius
const ARTIFACT_SPHERE_RADIUS = 28;  // world units — artifact mini-sphere radius

// ── Smoothing rates ────────────────────────────────────────────────────────
// All smoothing uses: smoothed = prev * (1 - rate) + raw * rate
// Higher rate = faster response, more jitter. Lower rate = smoother, more lag.

const CAMERA_DIST_SMOOTHING  = 0.08;  // ~12-frame lag at 60 fps (~0.2 s window)
const ORBIT_PIVOT_DRIFT_RATE = 0.015; // slow drift keeps the orbit pivot stable

// ── Force adaptation ───────────────────────────────────────────────────────
const FORCE_ADAPT_RATE    = 0.06;  // fraction of force gap closed per frame (~0.8 s to settle)
const FORCE_DEAD_ZONE     = 0.05;  // camera must move >5% before forces re-tune
const CAMERA_PULLBACK_MULT = 2.0;  // fallback: camera placed at cluster_radius × this

const CAMERA_MIN_DIST      = 10;   // prevents camera passing through node spheres

// ── Viewport-aware zoom-out boundary ──────────────────────────────────────
// The maximum zoom-out distance is not a constant — it depends on:
//   1. Cluster bounding radius (from node volume)
//   2. Largest node sphere radius (surface extends past the cluster centre)
//   3. Minimum gap between the outermost surface and the frustum edge
//   4. Viewport aspect ratio (portrait vs landscape changes the constraining FOV)
// computeMaxZoomOut() recomputes all four factors on every call.
const MIN_NODE_SCREEN_GAP  = 8;    // world units of breathing room inside the frustum edge
const FALLBACK_COMFORT     = 1.05; // padding used when viewport dimensions are unavailable

// ═══════════════════════════════════════════════════════════════════════════
// Cluster physics — all constants derived from two roots:
//   BASELINE_NODE_RADIUS   average rendered sphere radius in world units
//   SPACING_RATIO          personal-space volume target (1× = touching, 3× = spacious)
//
// Derivation chain:
//   nodeVal_avg  = clamp(cbrt(val_avg=10)×2, NODE_SIZE_MIN=2, NODE_SIZE_MAX=40) ≈ 4.31
//   BASELINE_NODE_RADIUS = cbrt(nodeVal_avg) × SPHERE_SCALE = cbrt(4.31) × 6 ≈ 9.77
//
//   Personal-space volume ratio ρ = (separation/2 / r_node)³
//     ρ=2 → separation = 2 × ∛2 × r_node ≈ 25 (old minimum — was too tight)
//     ρ=4 → separation = 2 × ∛4 × r_node ≈ 31 (new target — 4× volume each)
//     ρ=6 → separation = 2 × ∛6 × r_node ≈ 36 (new maximum)
//
//   NODE_SEPARATION = 2 × ∛SPACING_RATIO × BASELINE_NODE_RADIUS
//
//   Balance condition (cohesion = repulsion at r = NODE_SEPARATION):
//     G_COHESION × r / √(r² + S²) = |REPULSION_STRENGTH| / r²
//     → REPULSION_STRENGTH = −G_COHESION × r³ / √(r² + COHESION_SOFTENING²)
//
//   REPULSION_DMAX = NODE_SEPARATION × 2
//     Wide ramp-down zone softens the repulsion onset → reduces jitter.
//     At REPULSION_DMAX, cohesion force still exceeds repulsion — inward restoring force exists.
//
//   UNIVERSE_RADIUS = NODE_SEPARATION × 3.2
//     Chosen so fibonacciSphere(87) minimum pair distance (= 0.22×R) > REPULSION_DMAX/3.
// ═══════════════════════════════════════════════════════════════════════════

const BASELINE_NODE_RADIUS = Math.cbrt(Math.max(NODE_SIZE_MIN, Math.min(NODE_SIZE_MAX, Math.cbrt(10) * 2))) * SPHERE_SCALE; // ≈ 9.8
const SPACING_RATIO      = 8.0;  // personal-space volume = 8× node volume (was 4, bumped 2×)
const NODE_SEPARATION    = 2 * Math.cbrt(SPACING_RATIO) * BASELINE_NODE_RADIUS;  // ≈ 24.6 world units

// Cohesion: centripetal 1/r force — uniform pull so all nodes converge at the same rate.
// N-body gravity handles mass stratification (heavy nodes to centre).
const G_COHESION           = 0.3;   // root constant — drives all repulsion/universe derivations
const COHESION_SOFTENING   = 30;    // Plummer softening radius — prevents singularity at origin

// N-body gravity: 1/r² force, mass-proportional — secondary, provides mass stratification.
const GRAVITY_INIT         = 0.30;  // fixed (zoom adaptor disabled); equals G_COHESION by design
const GRAVITY_SOFTENING    = 5;     // small — manyBody repulsion handles close-range separation
const GRAVITY_REHEAT_DELTA = 0.005; // minimum G change that warrants a physics reheat

// Short-range manyBody repulsion: d3's forceManyBody is numerically stable with d3's Verlet
// integrator (proven stable; LJ r^-12 caused crash-through without a thermostat).
// REPULSION_STRENGTH and REPULSION_DMAX are derived — do not edit them directly.
const REPULSION_STRENGTH   = -(G_COHESION * NODE_SEPARATION ** 3 /
  Math.sqrt(NODE_SEPARATION ** 2 + COHESION_SOFTENING ** 2)); // balance at NODE_SEPARATION ≈ -115
const REPULSION_DMAX       = NODE_SEPARATION * 2;              // wide ramp-down → less jitter ≈ 49

// ── Post-settle node correction ────────────────────────────────────────────
// After d3 alpha decays to zero (~7 s post-load), a frame-loop tick takes over.
// It applies two position-based desired-state corrections at 15 fps — no forces,
// no velocities, no oscillation; same pattern as tickZoomAdaptor's lerp-toward-target.
//
//   1. Drift toward cluster centre — mass-weighted: heavy nodes drift faster → sink to centre.
//      drift_fraction = CLUSTER_DRIFT_RATE × ln(1 + val)
//
//   2. Separation — push pairs closer than NODE_SEPARATION apart by half the overlap.
//      This is the d3 forceCollide approach: position correction, not force/velocity.
//
// CLUSTER_CORRECTION_START_MS: enough time for d3 alpha to decay (≈5 s) plus settling margin.
const CLUSTER_DRIFT_RATE         = 0.0005; // fraction per call × ln(1+val) pulled toward centre
const CLUSTER_SEPARATION_STRENGTH = 0.5;   // fraction of overlap resolved per correction call
const CLUSTER_CORRECTION_START_MS = 7000;  // ms after loadMacro before correction activates

// ── Zoom-out headroom ──────────────────────────────────────────────────────
const ZOOM_OUT_HEADROOM  = 1.1;  // 10% extra margin so outermost node surfaces clear the frustum

// Placement radius: nodes start on a fibonacciSphere at UNIVERSE_RADIUS.
// NODE_SEPARATION × 3.2 ensures min pair distance on the sphere (= 0.22R) ≈ REPULSION_DMAX/2.
const UNIVERSE_RADIUS      = Math.ceil(NODE_SEPARATION * 3.2); // ≈ 79 world units
const CAMERA_DIST_MAX      = UNIVERSE_RADIUS * 3;              // fallback before data is loaded ≈ 237

// ── Scroll zoom momentum ───────────────────────────────────────────────────
// Each scroll tick adds an impulse; the impulse coasts to zero each frame.
const SCROLL_ZOOM_IMPULSE  = 0.05;  // log-space speed added per scroll tick
const SCROLL_ZOOM_COAST    = 0.88;  // fraction of speed kept each frame (~400 ms to stop)

// ── Label rendering ────────────────────────────────────────────────────────
const LABEL_UPDATE_EVERY_N_FRAMES = 4;    // ~15 fps updates — imperceptible at human refresh rate
const LABEL_OPACITY_EPSILON       = 0.02; // skip GPU write if opacity change < 2%
const LABEL_FADE_START_DIST       = 300;  // world units — full opacity within this distance
const LABEL_FADE_END_DIST         = 900;  // world units — fully transparent beyond this distance

// ── Frame budget ───────────────────────────────────────────────────────────
const FRAME_BUDGET_MS      = 8;    // JS budget per frame — headroom for Three.js + browser

// ── Interaction timing ─────────────────────────────────────────────────────
const DOUBLE_CLICK_MAX_MS  = 300;   // max gap between two clicks to count as double-click
// ── Camera desired-state correction ────────────────────────────────────────
// Two per-frame lerp rates replace all discrete camera animations.
// Pattern: tickCameraCorrection runs every frame, reads camDesiredDist, lerps toward it.
// Same pattern as tickZoomAdaptor (desired state → slowly correct).
const CAM_TARGET_DRIFT     = 0.008; // fraction/frame ctrl.target follows actual CoM (~1.2 s lag)
const CAM_TARGET_DEAD_ZONE = 1.0;   // world units — no-op if CoM is this close to current target
const CAM_DIST_CORRECTION  = 0.020; // fraction/frame camera distance follows camDesiredDist (~2.5 s)
const CAM_DIST_DEAD_ZONE   = 3.0;   // world units — no-op if camera is this close to desired distance

// ── Link curves ────────────────────────────────────────────────────────────
const DEPENDS_ON_CURVATURE = 0.15;  // arc bend for depends_on edges — shows direction clearly

// ── Idle orbit ─────────────────────────────────────────────────────────────
// Speed ramps up/down each frame (flywheel feel) — never snaps on or off.
const ORBIT_CRUISE_SPEED   = 0.4;   // deg/s at cruise — slow enough to feel calm
const ORBIT_RAMP_RATE      = 0.025; // fraction of speed gap closed per frame (~2 s to full speed)
const IDLE_ORBIT_MS        = 4000;  // idle before spin begins
const IDLE_HOME_MS         = 20000; // idle before camera flies home

// ── Physics settle delay ───────────────────────────────────────────────────



const DEFAULT_STATUSES = 'work.draft,work.active,work.blocked,work.complete,note.fleeting,note.mature,note.evergreen,decision.proposed,decision.accepted,cancelled,archived,active';

// Sum of nodeVal across all nodes — the shared input for radius and camera distance.
function totalNodeVolume(nodes) {
  return nodes.reduce((s, n) => s + nodeVal(n), 0);
}

// World-space radius of the largest rendered sphere.
// ForceGraph3D: sphere_world_radius = cbrt(nodeVal) * nodeRelSize (= SPHERE_SCALE).
function maxNodeWorldRadius(nodes) {
  return nodes.reduce((max, n) => Math.max(max, Math.cbrt(nodeVal(n)) * SPHERE_SCALE), 0);
}

/**
 * Camera distance that guarantees every node surface is inside the frustum,
 * accounting for viewport aspect ratio.
 *
 * Bounding radius = cluster_cap_radius + largest_node_sphere + gap
 * Constraining FOV = min(vertical, horizontal) — whichever dimension is narrower.
 * D = boundingRadius / tan(fovEff / 2)
 *
 * @param {Array}  nodes   — current graph nodes
 * @param {object} camera  — Three.js PerspectiveCamera (camera.fov, camera.aspect)
 */
function computeMaxZoomOut(nodes, camera) {
  const vol      = totalNodeVolume(nodes);
  const capR     = clusterRadiusFromVolume(vol);
  const nodeR    = maxNodeWorldRadius(nodes);
  const boundR   = capR + nodeR + MIN_NODE_SCREEN_GAP;

  const fovVRad  = camera.fov * Math.PI / 180;
  const aspect   = camera.aspect || FALLBACK_COMFORT;
  const fovHRad  = 2 * Math.atan(Math.tan(fovVRad / 2) * aspect);
  const fovEff   = Math.min(fovVRad, fovHRad);

  return boundR / Math.tan(fovEff / 2) * ZOOM_OUT_HEADROOM;
}

let state = null;   // createGraphState() — one per initGraph call
let deps  = {};     // injected CDN globals
let palette = null; // built from culori + GRAPH_BG
let renderer = null;
let Graph = null;
let fitAllNodesFn  = null; // set by initGraph; called from loadMacro after data is loaded
let enableOrbitFn  = null; // set by initGraph; called from loadMacro to start spinning on boot

// Desired camera distance — set by fitAllNodes/aimAtCenterOfMass, cleared by onInteraction.
// tickCameraCorrection lerps the actual distance toward this each frame.
// null = user controls distance (no correction active).
let camDesiredDist = null;

// Zoom-adaptive force state — module-level so loadMacro() can reference them.
// loadMacro is a module-scope async function; variables declared inside
// initGraph() are not in its closure scope.
let currentG    = GRAVITY_INIT;
let cohesionForce         = null;     // forceCentripetal (uniform) — fast convergence from any radius
let gravityForce          = null;     // forceNBodyGravity — mass-proportional stratification
let correctionStartTime   = Infinity; // epoch ms — set in loadMacro; guards tickNodeCorrection
let capForce      = null; // forceRadiusCap — prevents unbounded scatter

function mergedGraphData() {
  const nodes = [...state.macroData.nodes];
  const links = [...state.macroData.links];
  for (const [, d] of state.expandedScopes) { nodes.push(...d.nodes); links.push(...d.links); }
  for (const [, d] of state.expandedKinds)  { nodes.push(...d.nodes); links.push(...d.links); }
  return { nodes, links };
}

function applyGraphData() {
  const raw = mergedGraphData();
  const data = filterGraphData(raw);
  log.info('applyGraphData nodes=%d links=%d (raw %d)', data.nodes.length, data.links.length, raw.nodes.length);
  Graph.graphData(data);
  // no post-processing — renderer owns appearance only, not material patching
  const { nodes, links } = Graph.graphData();
  setStats(state.els.stats, nodes.length, links.length);
  renderExpandedTags(state.els.expandedWrap, state.els.expandedList, state.expandedScopes, state.expandedKinds,
    collapseScope, (sc, kind) => {
      state.expandedKinds.delete(`${sc}:${kind}`);
      removeBubble(`kind:${sc}:${kind}`);
      applyGraphData();
      updateModeBadge();
    });
  setTimeout(recreateGlows, 50);
}

function updateModeBadge() {
  setModeBadge(state.els.modeBadge, depthFromExpanded(state.expandedScopes.size, state.expandedKinds.size));
}


async function loadMacro() {
  if (state.els.stats) state.els.stats.textContent = 'Loading universe…';
  setModeBadge(state.els.modeBadge, 'scope');

  let data;
  try {
    data = await fetchScopeGraph(deps.fetch);
  } catch (err) {
    log.error('loadMacro: fetch failed error=%s', err.message);
    if (state.els.stats) state.els.stats.textContent = 'Failed to load graph data';
    return;
  }
  log.info('loadMacro nodes=%d links=%d renderer=%s',
    data.nodes.length, data.links.length, renderer.constructor.name);

  // Sort by degree.
  const deg = {};
  for (const l of data.links) {
    deg[l.source] = (deg[l.source] || 0) + 1;
    deg[l.target] = (deg[l.target] || 0) + 1;
  }
  const sorted = [...data.nodes].sort((a, b) => (deg[b.id] || 0) - (deg[a.id] || 0));

  // Seed positions on a sphere so nodes start spread (not all at origin).
  const positions = equatorPriorityPositions(sorted.length, UNIVERSE_RADIUS);
  sorted.forEach((n, i) => {
    n.x = positions[i].x;
    n.y = positions[i].y;
    n.z = positions[i].z;
    state.scopeSpherePos.set(n.scope || n.name, positions[i]);
  });



  // N-body physics: gravity + repulsion + radius cap.
  // Radius scales with total node visual volume — larger/more nodes get more room.
  const vol  = totalNodeVolume(sorted);
  const maxR = clusterRadiusFromVolume(vol);
  capForce.setMaxRadius(maxR);
  log.info('loadMacro cap radius=%d vol=%d nodes=%d', Math.round(maxR), Math.round(vol), sorted.length);

  Graph.d3Force('center',   null);
  Graph.d3Force('radial',   null);
  Graph.d3Force('cohesion', cohesionForce);  // fast convergence from any radius
  Graph.d3Force('gravity',  gravityForce);   // mass stratification
  Graph.d3Force('cap',      capForce);
  // Short-range manyBody repulsion: proven stable with d3's integrator.
  // distanceMax=30 makes it contact-only — no long-range pressure, no hollow shell.
  Graph.d3Force('charge')?.strength?.(REPULSION_STRENGTH);
  Graph.d3Force('charge')?.distanceMax?.(REPULSION_DMAX);

  // Give renderer the full node set so it can normalise sizes and pre-build textures.
  renderer.init(sorted);

  state.macroData = { nodes: sorted, links: data.links };
  state.expandedScopes.clear();
  state.expandedKinds.clear();
  clearBubbles();
  applyGraphData();
  updateModeBadge();

  // Camera: FOV-accurate fit immediately, then smooth re-fit after physics settles.
  // Boot placement: aimAtCenterOfMass centres the camera, fitAllNodes computes the
  // actual-position-based fit distance and sets camDesiredDist. Then snap immediately.
  aimAtCenterOfMass();
  if (fitAllNodesFn) {
    fitAllNodesFn(); // sets camDesiredDist = actualFitDist()
    if (camDesiredDist) {
      const cam = Graph.camera(), ctrl = Graph.controls();
      const dx = cam.position.x-ctrl.target.x, dy=cam.position.y-ctrl.target.y, dz=cam.position.z-ctrl.target.z;
      const f = camDesiredDist / (Math.hypot(dx,dy,dz)||1);
      cam.position.set(ctrl.target.x+dx*f, ctrl.target.y+dy*f, ctrl.target.z+dz*f);
      ctrl.update();
    }
  }
  if (enableOrbitFn) enableOrbitFn();
  correctionStartTime = Date.now() + CLUSTER_CORRECTION_START_MS;
}


async function expandScope(scopeName) {
  if (state.expandedScopes.has(scopeName)) return;
  const t0 = performance.now();
  const status    = document.getElementById('status-select')?.value || DEFAULT_STATUSES;
  const relations = activeRelations();
  if (state.els.stats) state.els.stats.textContent = `Loading ${scopeName} kinds…`;

  let kindData;
  try {
    kindData = await fetchKindGraph(deps.fetch, scopeName, status.split(','), relations);
  } catch (err) {
    log.error('expandScope scope=%s error=%s', scopeName, err.message);
    return;
  }
  const fetchMs = (performance.now() - t0).toFixed(1);
  log.info('expandScope scope=%s kinds=%d links=%d fetch=%sms', scopeName, kindData.nodes.length, kindData.links.length, fetchMs);
  removeMacroNode(`scope:${scopeName}`);

  const anchor = state.scopeSpherePos.get(scopeName) || { x: 0, y: 0, z: 0 };
  kindData.nodes = placeInMiniSphere(kindData.nodes, kindData.links, anchor, KIND_SPHERE_RADIUS);
  for (const n of kindData.nodes) {
    state.scopeSpherePos.set(`kind:${scopeName}:${n.group || n.kind}`, { x: n.x, y: n.y, z: n.z });
  }

  state.expandedScopes.set(scopeName, kindData);
  createBubble(scopeName, KIND_SPHERE_RADIUS + 10);
  applyGraphData();
  updateModeBadge();
}


async function expandKind(scopeName, kindName) {
  const key = `${scopeName}:${kindName}`;
  if (state.expandedKinds.has(key)) return;
  const status    = document.getElementById('status-select')?.value || DEFAULT_STATUSES;
  const relations = activeRelations();
  if (state.els.stats) state.els.stats.textContent = `Loading ${scopeName}/${kindName}…`;

  let artData;
  try {
    artData = await fetchArtifactGraph(deps.fetch, scopeName, status.split(','), relations);
  } catch (err) {
    log.error('expandKind scope=%s kind=%s error=%s', scopeName, kindName, err.message);
    return;
  }
  log.info('expandKind scope=%s kind=%s artifacts=%d', scopeName, kindName, artData.nodes.length);
  artData.nodes = artData.nodes.filter(n => n.kind === kindName);
  const nodeIds = new Set(artData.nodes.map(n => n.id));
  artData.links = artData.links.filter(l => nodeIds.has(l.source) && nodeIds.has(l.target));

  const kindNodeId = `kind:${key}`;
  if (state.expandedScopes.has(scopeName)) {
    const sd = state.expandedScopes.get(scopeName);
    sd.nodes = sd.nodes.filter(n => n.id !== kindNodeId);
    sd.links = sd.links.filter(l => l.source !== kindNodeId && l.target !== kindNodeId &&
      l.source?.id !== kindNodeId && l.target?.id !== kindNodeId);
  }

  const anchor = state.scopeSpherePos.get(kindNodeId) || state.scopeSpherePos.get(scopeName) || { x: 0, y: 0, z: 0 };
  artData.nodes = placeInMiniSphere(artData.nodes, artData.links, anchor, ARTIFACT_SPHERE_RADIUS);

  state.expandedKinds.set(key, artData);
  createBubble(`kind:${key}`, ARTIFACT_SPHERE_RADIUS + 10);
  applyGraphData();
  updateModeBadge();
}


async function collapseScope(scopeName) {
  log.info('collapseScope scope=%s', scopeName);
  for (const key of [...state.expandedKinds.keys()]) {
    if (key.startsWith(`${scopeName}:`)) {
      state.expandedKinds.delete(key);
      removeBubble(`kind:${key}`);
    }
  }
  state.expandedScopes.delete(scopeName);
  removeBubble(scopeName);

  const fresh = await fetchScopeGraph(deps.fetch);
  const stillExpanded = new Set(state.expandedScopes.keys());
  state.macroData.nodes = fresh.nodes.filter(n => !stillExpanded.has(n.scope));
  state.macroData.links = fresh.links.filter(l => {
    const fs = (l.source?.id || l.source)?.replace('scope:', '');
    const ts = (l.target?.id || l.target)?.replace('scope:', '');
    return !stillExpanded.has(fs) && !stillExpanded.has(ts);
  });
  applyGraphData();
  updateModeBadge();
}

function removeMacroNode(nodeId) {
  state.macroData.nodes = state.macroData.nodes.filter(n => n.id !== nodeId);
  state.macroData.links = state.macroData.links.filter(l =>
    l.source !== nodeId && l.target !== nodeId &&
    l.source?.id !== nodeId && l.target?.id !== nodeId);
}


// Place camera at the cluster centre. Called once at boot (animMs=0) for initial placement;
// all subsequent corrections happen via tickCameraCorrection (desired-state lerp).
function aimAtCenterOfMass(distOverride = null) {
  const controls = Graph.controls();
  if (!controls?.target) return;

  const com = centerOfMass(Graph.graphData().nodes);
  state.virtualCenter.x = com.x; state.virtualCenter.y = com.y; state.virtualCenter.z = com.z;

  const pool = parentNodes(Graph.graphData().nodes);
  let radius = 200;
  for (const n of pool) {
    const d = Math.hypot((n.x||0)-com.x, (n.y||0)-com.y, (n.z||0)-com.z);
    if (d > radius) radius = d;
  }
  const camDist = distOverride ?? Math.min(radius * CAMERA_PULLBACK_MULT, CAMERA_DIST_MAX);
  const cam = controls.object.position;
  const dx = cam.x-com.x, dy = cam.y-com.y, dz = cam.z-com.z;
  const len = Math.hypot(dx, dy, dz) || 1;
  const scale = camDist / len;

  controls.object.position.set(com.x+dx*scale, com.y+dy*scale, com.z+dz*scale);
  controls.target.set(com.x, com.y, com.z);
  controls.update();
  camDesiredDist = camDist; // tickCameraCorrection keeps it there as nodes settle
  log.info('aimCamera com=(%d,%d,%d) radius=%d camDist=%d cam=(%d,%d,%d)',
    Math.round(com.x), Math.round(com.y), Math.round(com.z),
    Math.round(radius), Math.round(camDist),
    Math.round(controls.object.position.x), Math.round(controls.object.position.y), Math.round(controls.object.position.z));
}


function createBubble(key, initialRadius = 100) {
  if (!deps.THREE) return;
  const scene = Graph.scene();
  const color = new deps.THREE.Color(palette?.kinds?.scope?.hex || '#6366f1');
  for (const [bkey, cfg] of [[key, { opacity: 0.04, side: deps.THREE.BackSide }], [`${key}_wire`, { opacity: 0.1, wireframe: true }]]) {
    const geo  = new deps.THREE.SphereGeometry(1, 32, 32);
    const mat  = new deps.THREE.MeshBasicMaterial({ color, transparent: true, ...cfg, depthWrite: false });
    const mesh = new deps.THREE.Mesh(geo, mat);
    mesh.scale.setScalar(initialRadius);
    scene.add(mesh);
    state.scopeBubbles.set(bkey, mesh);
  }
}

function removeBubble(key) {
  const scene = Graph.scene();
  for (const k of [key, `${key}_wire`]) {
    const m = state.scopeBubbles.get(k);
    if (m) { scene.remove(m); state.scopeBubbles.delete(k); }
  }
}

function clearBubbles() {
  const scene = Graph.scene();
  for (const [, m] of state.scopeBubbles) scene.remove(m);
  state.scopeBubbles.clear();
}

function fitBubble(key, memberNodes, padding) {
  if (!memberNodes.length) return;
  let cx=0, cy=0, cz=0;
  for (const n of memberNodes) { cx+=n.x||0; cy+=n.y||0; cz+=n.z||0; }
  cx/=memberNodes.length; cy/=memberNodes.length; cz/=memberNodes.length;
  let r = 20;
  for (const n of memberNodes) { const d=Math.hypot((n.x||0)-cx,(n.y||0)-cy,(n.z||0)-cz); if(d>r) r=d; }
  r += padding;
  for (const k of [key, `${key}_wire`]) {
    const m = state.scopeBubbles.get(k);
    if (m) { m.position.set(cx,cy,cz); m.scale.setScalar(r); }
  }
}




function recreateGlows() {
  if (!deps.THREE) return;
  const scene = Graph.scene();
  for (const { inner, outer } of state.glowMeshes.values()) {
    scene.remove(inner); scene.remove(outer);
    inner.geometry.dispose(); inner.material.dispose();
    outer.geometry.dispose(); outer.material.dispose();
  }
  state.glowMeshes.clear();
  for (const node of Graph.graphData().nodes) {
    const hex = glowColor(deps.culori, node.kind, node.violations || 0);
    if (!hex) continue;
    const col = parseInt(hex.replace('#',''), 16);
    const nodeR = Math.sqrt(node.val || 1) * 8;
    const makeSphere = (r, opacity) => {
      const g = new deps.THREE.SphereGeometry(r, 16, 12);
      const m = new deps.THREE.MeshBasicMaterial({ color: col, transparent: true, opacity,
        side: deps.THREE.BackSide, depthWrite: false, blending: deps.THREE.AdditiveBlending });
      return new deps.THREE.Mesh(g, m);
    };
    const cfg   = glowConfig(node.violations || 0);
    const inner = makeSphere(nodeR * 1.6, 0);
    const outer = makeSphere(nodeR * 2.8, 0);
    inner.userData = { cfg };
    outer.userData = { cfg };
    scene.add(inner); scene.add(outer);
    state.glowMeshes.set(node.id, { node, inner, outer });
  }
}

function tickGlows() {
  const t = performance.now() / 1000;
  for (const { node, inner, outer } of state.glowMeshes.values()) {
    const nx=node.x||0, ny=node.y||0, nz=node.z||0;
    inner.position.set(nx,ny,nz); outer.position.set(nx,ny,nz);
    const { inner: iOp, outer: oOp } = glowOpacity(inner.userData.cfg, t);
    inner.material.opacity = iOp;
    outer.material.opacity = oOp;
  }
}


function onTick() {
  if (++state.tickCount % 6 !== 0) return;
  // Update bubble geometry.
  for (const [scopeName] of state.expandedScopes) {
    fitBubble(scopeName, Graph.graphData().nodes.filter(n => n.scope===scopeName && n.kind==='kind-group'), 50);
  }
  for (const [key] of state.expandedKinds) {
    const [sc, kind] = key.split(':');
    fitBubble(`kind:${key}`, Graph.graphData().nodes.filter(n => n.scope===sc && n.kind===kind), 25);
  }
  // Lerp state.virtualCenter toward weighted CoM — informational only, no camera side-effects.
  const com = centerOfMass(Graph.graphData().nodes);
  state.virtualCenter.x += (com.x - state.virtualCenter.x) * ORBIT_PIVOT_DRIFT_RATE;
  state.virtualCenter.y += (com.y - state.virtualCenter.y) * ORBIT_PIVOT_DRIFT_RATE;
  state.virtualCenter.z += (com.z - state.virtualCenter.z) * ORBIT_PIVOT_DRIFT_RATE;
  tickGlows();
}


function onNodeClickWithDbl(node) {
  const now = Date.now();
  if (state.lastClick.node === node && now - state.lastClick.time < DOUBLE_CLICK_MAX_MS) {
    state.lastClick.node = null;
    if (node.kind !== 'scope' && node.scope) collapseScope(node.scope);
    return;
  }
  state.lastClick.node = node; state.lastClick.time = now;
  if (node.kind === 'scope')      { expandScope(node.scope || node.name); return; }
  if (node.kind === 'kind-group') { expandKind(node.scope, node.group); return; }
  openSidebar(state.els.sidebar, state.els.sidebarContent, deps.fetch, deps.htmx, node.id);
  const ctrl = Graph.controls();
  if (ctrl?.target) {
    const APPROACH = 120;
    const com = state.virtualCenter;
    const dx=(node.x||0)-com.x, dy=(node.y||0)-com.y, dz=(node.z||0)-com.z;
    const dist=Math.hypot(dx,dy,dz)||1, scale=(dist+APPROACH)/dist;
    ctrl.object.position.set(com.x+dx*scale, com.y+dy*scale, com.z+dz*scale);
    ctrl.target.set(node.x||0, node.y||0, node.z||0);
    ctrl.update();
  }
}

function onNodeRightClick(node, event) {
  event.preventDefault();
  let items;
  if (node.kind === 'scope') {
    items = [{ label: '🔭 Expand (kinds)', action: () => expandScope(node.scope || node.name) }];
  } else if (node.kind === 'kind-group') {
    items = [
      { label: '🔬 Expand (artifacts)', action: () => expandKind(node.scope, node.group) },
      { label: '📦 Collapse scope',     action: () => collapseScope(node.scope) },
    ];
  } else {
    items = [
      { label: '🔗 Open detail',    action: () => window.open(`/artifacts/${node.id}`, '_blank') },
      { label: '🌳 Open tree',      action: () => window.open(`/tree/${node.id}`, '_blank') },
      { label: '📦 Collapse scope', action: () => collapseScope(node.scope) },
      { sep: true },
      { label: '⬆ Set active',     action: () => patchArtifact(deps.fetch, node.id, 'status', 'active').catch(e => log.error('patch id=%s error=%s', node.id, e.message)) },
      { label: '✓ Set complete',    action: () => patchArtifact(deps.fetch, node.id, 'status', 'complete').catch(e => log.error('patch id=%s error=%s', node.id, e.message)) },
    ];
  }
  showContextMenu(state.els.ctxMenu, event.clientX, event.clientY, items);
}


function filterGraphData(data) {
  const kinds = activeKindPrefixes();
  const minRefs = minRefsValue();
  let nodes = data.nodes;
  if (kinds.length > 0) {
    nodes = nodes.filter(n => {
      if (n.kind === 'scope' || n.kind === 'kind-group') return true;
      return kinds.some(k => n.kind?.startsWith(k));
    });
  }
  if (minRefs > 0) {
    const degree = {};
    data.links.forEach(l => {
      const src = typeof l.source === 'object' ? l.source.id : l.source;
      const tgt = typeof l.target === 'object' ? l.target.id : l.target;
      degree[src] = (degree[src] || 0) + 1;
      degree[tgt] = (degree[tgt] || 0) + 1;
    });
    nodes = nodes.filter(n => {
      if (n.kind === 'scope' || n.kind === 'kind-group') return true;
      return (degree[n.id] || 0) >= minRefs;
    });
  }
  const nodeIds = new Set(nodes.map(n => n.id));
  const links = data.links.filter(l => {
    const src = typeof l.source === 'object' ? l.source.id : l.source;
    const tgt = typeof l.target === 'object' ? l.target.id : l.target;
    return nodeIds.has(src) && nodeIds.has(tgt);
  });
  return { nodes, links };
}

function activeRelations() {
  return [...document.querySelectorAll('.rel-toggle.active[data-rel]')].map(el => el.dataset.rel);
}

function activeKindPrefixes() {
  return [...document.querySelectorAll('.rel-toggle.active[data-kind]')].map(el => el.dataset.kind);
}

function minRefsValue() {
  const el = document.getElementById('minrefs-slider');
  return el ? parseInt(el.value, 10) : 0;
}


/**
 * Initialise the graph. Call once after CDN scripts have loaded.
 *
 * @param {object} injectedDeps — { ForceGraph3D, THREE, culori, htmx, fetch? }
 */
export function initGraph(injectedDeps) {
  state = createGraphState();
  deps = {
    fetch: window.fetch.bind(window),
    ...injectedDeps,
  };

  // Build colour palette from graph background.
  palette = buildPalette(deps.culori, GRAPH_BG);

  // Resolve DOM elements.
  state.els.modeBadge     = document.getElementById('mode-badge');
  state.els.stats         = document.getElementById('stats');
  state.els.expandedWrap  = document.getElementById('expanded-wrap');
  state.els.expandedList  = document.getElementById('expanded-list');
  state.els.sidebar       = document.getElementById('sidebar');
  state.els.sidebarContent = document.getElementById('sidebar-content');
  state.els.ctxMenu       = document.getElementById('ctx-menu');
  const sidebarCloseBtn   = document.getElementById('sidebar-close');
  if (sidebarCloseBtn) sidebarCloseBtn.onclick = () => closeSidebar(state.els.sidebar);

  document.querySelectorAll('.rel-toggle[data-rel]').forEach(el => {
    el.onclick = () => { el.classList.toggle('active'); };
  });
  document.querySelectorAll('.rel-toggle[data-kind]').forEach(el => {
    el.onclick = () => { el.classList.toggle('active'); };
  });
  const minrefsSlider = document.getElementById('minrefs-slider');
  const minrefsVal = document.getElementById('minrefs-val');
  if (minrefsSlider && minrefsVal) {
    minrefsSlider.oninput = () => { minrefsVal.textContent = minrefsSlider.value; };
  }

  // Renderer: swap this one line to change node appearance.
  // KindColorRenderer — hardcoded kind colors, opacity 0.9 (v1 exact)
  // CSSVarRenderer    — colors from CSS custom properties (palette-driven)
  renderer = new KindColorRenderer();
  log.info('init renderer=%s bg=%s', renderer.constructor.name, GRAPH_BG);

  // Build ForceGraph3D.
  const graphBuilder = deps.ForceGraph3D({ controlType: 'orbit' })(
    document.getElementById('graph-root')
  )
    .backgroundColor(GRAPH_BG);

  // Renderer owns only node appearance — one call, no side effects.
  renderer.apply(graphBuilder, GRAPH_BG);

  Graph = window._Graph = graphBuilder
    .nodeLabel(n => {
      const title = n.kind === 'scope'
        ? `<strong>${n.name}</strong><br><span style="opacity:0.7">${n.val} artifacts — click to expand</span>`
        : `<strong>${n.id}</strong><br>${n.name}`;
      return `<div style="background:rgba(0,0,0,0.85);color:#e2e8f0;padding:5px 9px;border-radius:5px;font-size:12px;pointer-events:none;max-width:260px">${title}</div>`;
    })
    .nodeResolution(12)
    // link appearance owned by renderer
    .linkDirectionalParticles(l => {
      if (l.relation === 'depends_on' || l.relation === 'blocks') return 3;
      if (l.relation === 'implements' || l.relation === 'satisfies') return 2;
      return 0;
    })
    .linkDirectionalParticleSpeed(l => l.relation === 'depends_on' ? 0.004 : 0.003)
    .linkDirectionalParticleWidth(1.2)
    .linkDirectionalParticleColor(l => {
      const srcNode = state.graphData?.nodes?.find(n => n.id === l.source?.id || n.id === l.source);
      return srcNode ? renderer._nodeColor(srcNode) : null;
    })
    .linkCurvature(l => {
      if (l.relation === 'depends_on' || l.relation === 'blocks') return DEPENDS_ON_CURVATURE;
      if (l.relation === 'implements' || l.relation === 'justifies') return 0.08;
      return 0;
    })
    .d3VelocityDecay(0.3)
    .warmupTicks(300)
    .cooldownTime(Infinity)
    .onNodeClick(onNodeClickWithDbl)
    .onNodeRightClick(onNodeRightClick)
    .onBackgroundClick((() => {
      let lastBgClick = 0;
      return () => {
        closeSidebar(state.els.sidebar);
        const now = Date.now();
        if (now - lastBgClick < DOUBLE_CLICK_MAX_MS) { if (fitAllNodesFn) fitAllNodesFn(); }
        lastBgClick = now;
      };
    })())
    .onEngineTick(onTick);

  // Camera near plane and zoom bounds.
  const cam = Graph.camera();
  cam.near = 0.1;
  cam.updateProjectionMatrix();
  const ctrl = Graph.controls();
  ctrl.minDistance  = CAMERA_MIN_DIST;
  ctrl.maxDistance  = CAMERA_DIST_MAX; // overwritten by fitAllNodes once data is loaded
  ctrl.enableZoom   = false;           // zoom handled manually for momentum effect

  // Resize handler — update graph dimensions and recompute maxDistance.
  // maxDistance depends on viewport aspect ratio, so it changes on resize.
  window.addEventListener('resize', () => {
    Graph.width(window.innerWidth).height(window.innerHeight);
    const nodes = Graph.graphData().nodes;
    if (nodes.length) Graph.controls().maxDistance = computeMaxZoomOut(nodes, Graph.camera());
  });

  // ── Fit all nodes ──────────────────────────────────────────────────────
  // Positions camera so the entire node cluster fills the viewport.
  // Uses the camera's actual FOV to compute exact distance — guarantees
   // Computes the camera distance from ACTUAL node positions (same formula as tickMaxZoomBoundary).
   // Using estimated clusterRadiusFromVolume caused camDesiredDist > ctrl.maxDistance, which
   // made tickCameraCorrection clamp and jump when tickMaxZoomBoundary shrank the boundary.
   function actualFitDist() {
     const nodes = Graph.graphData().nodes;
     if (!nodes.length) return null;
     const cam  = Graph.camera();
     const ctrl = Graph.controls();
     const tgt  = ctrl.target;
     let maxR = 0;
     for (const n of nodes) {
       const d = Math.hypot((n.x||0)-tgt.x, (n.y||0)-tgt.y, (n.z||0)-tgt.z);
       maxR = Math.max(maxR, d + Math.cbrt(nodeVal(n)) * SPHERE_SCALE);
     }
     maxR += MIN_NODE_SCREEN_GAP;
     const fovVRad = cam.fov * Math.PI / 180;
     const fovHRad = 2 * Math.atan(Math.tan(fovVRad / 2) * (cam.aspect || 1));
     return maxR / Math.tan(Math.min(fovVRad, fovHRad) / 2) * ZOOM_OUT_HEADROOM;
   }

   // Sets camDesiredDist so tickCameraCorrection smoothly guides the camera.
   // No animMs — all movement happens via the per-frame lerp, never as discrete animations.
   function fitAllNodes() {
     const d = actualFitDist();
     if (!d) return;
     Graph.controls().maxDistance = d;
     camDesiredDist = d;
   }
  fitAllNodesFn  = fitAllNodes;
  enableOrbitFn  = enableOrbit;

  // ── Idle orbit ──────────────────────────────────────────────────────────
  // Short idle  (IDLE_ORBIT_MS): begin ramp-up toward cruise speed.
  // Long idle   (IDLE_HOME_MS):  return to fit-all-nodes view, continue orbit.
  // Speed is lerped each frame (tickOrbitRamp) — never snaps on or off.
  let orbitTimer    = null;
  let homeTimer     = null;
  let orbitTarget   = 0;                   // deg/s — what we're ramping toward
  let orbitCurrent  = 0;                   // deg/s — actual speed this frame

  function enableOrbit() {
    orbitTarget = ORBIT_CRUISE_SPEED; // ramp starts in tickOrbitRamp
  }

  function goHome() {
    fitAllNodes();  // sets camDesiredDist — tickCameraCorrection handles smooth fly
    enableOrbit();
  }

  function resetIdleTimers() {
    clearTimeout(orbitTimer);
    clearTimeout(homeTimer);
    orbitTimer = setTimeout(enableOrbit, IDLE_ORBIT_MS);
    homeTimer  = setTimeout(goHome,      IDLE_HOME_MS);
  }

  function onInteraction() {
    orbitTarget    = 0;    // ramp-down starts in tickOrbitRamp
    camDesiredDist = null; // user controls distance — don't fight the scroll
    resetIdleTimers();
  }

  // Zoom momentum state — accumulates scroll velocity, decays each frame.
  let zoomVelocity = 0;

  const graphRoot = document.getElementById('graph-root');
  if (graphRoot) {
    graphRoot.addEventListener('pointerdown', onInteraction);
    // Native zoom is disabled (ctrl.enableZoom=false); we drive it with momentum.
    // deltaY > 0 = wheel backward = zoom out (camera moves further away).
    // Positive velocity → Math.exp(-velocity) < 1 → distance shrinks → zoom in.
    // So we negate deltaY sign: scroll out → negative velocity → distance grows.
    graphRoot.addEventListener('wheel', (e) => {
      e.preventDefault();
      zoomVelocity += (e.deltaY > 0 ? -1 : 1) * SCROLL_ZOOM_IMPULSE;
      onInteraction();
    }, { passive: false });
  }

  // On boot: fit all nodes first (camera already positioned by aimAtCenterOfMass,
  // but recalculate with exact FOV), then start idle timers.
  // fitAllNodes is called from loadMacro after data is loaded — no race with setTimeout.
  setTimeout(resetIdleTimers, 600);

  // ── Per-frame adaptive systems ────────────────────────────────────────────
  let smoothedCamDist = null;
  let frameCount = 0;
  let lastAppliedCamDist = null;

  // gravityForce/currentG/Rep/Dmax are module-level — initialise forces now.
  cohesionForce = forceCentripetal(G_COHESION, COHESION_SOFTENING); // no massKey = uniform pull
  gravityForce  = forceNBodyGravity(currentG, GRAVITY_SOFTENING, 'val');
  capForce      = forceRadiusCap(UNIVERSE_RADIUS); // placeholder radius; loadMacro sets real value

  // ── Zoom adaptor (SRP: only touches forces) ───────────────────────────────
  // Runs every frame. Smooths camera distance (EMA), lerps current force
  // parameters toward the desired state, skips writes below dead zone.
  //
  // Dead zone (sensitivity=0.05): if zoom changed < 5% from last applied
  // distance, forcesForDist returns null → lerp target stays unchanged.
  // This prevents micro-jitter from continuously nudging forces.
  const LERP = FORCE_ADAPT_RATE;
  const SENSITIVITY = FORCE_DEAD_ZONE;

  function _tickZoomAdaptor() {
    const cam  = Graph.camera();
    const ctrl = Graph.controls();
    if (!cam || !ctrl) return;

    const rawDist = Math.hypot(
      cam.position.x - ctrl.target.x,
      cam.position.y - ctrl.target.y,
      cam.position.z - ctrl.target.z,
    );

    // EMA smoothing — α=0.08 ≈ 12 frames lag, filters per-frame scroll jitter.
    smoothedCamDist = smoothedCamDist == null ? rawDist : smoothedCamDist * (1 - CAMERA_DIST_SMOOTHING) + rawDist * CAMERA_DIST_SMOOTHING;

    // Adapt only gravity with zoom — LJ repulsion is fixed (contact-only, zoom-invariant).
    // Returns null when camera hasn't moved enough to warrant a force update.
    const desired = forcesForDist(smoothedCamDist, ZOOM_MIN_DIST, ZOOM_MAX_DIST, SENSITIVITY, lastAppliedCamDist);
    if (desired !== null) lastAppliedCamDist = smoothedCamDist;

    const targetG = desired ? desired.G : currentG;
    const prevG   = currentG;
    currentG += (targetG - currentG) * LERP;
    gravityForce.setG(currentG);

    if (Math.abs(currentG - prevG) > GRAVITY_REHEAT_DELTA) {
      Graph.d3ReheatSimulation();
    }
  }

  // ── Camera desired-state correction ──────────────────────────────────────
  // Replaces all discrete camera animations. Two per-frame lerps — same pattern
  // as tickZoomAdaptor: desired state → slowly correct, never overshoot.
  //
  //   target drift:     ctrl.target follows actual CoM at CAM_TARGET_DRIFT/frame.
  //                     Runs every 4 frames (O(n) CoM). Prevents off-centre orbit
  //                     when tickNodeCorrection shifts node positions.
  //
  //   distance lerp:    when camDesiredDist is set, camera distance lerps toward it
  //                     at CAM_DIST_CORRECTION/frame. Cleared by onInteraction so
  //                     user scroll is never fought.
  function tickCameraCorrection() {
    // Root cause of orbit bumps: OrbitControls owns cam.position via its spherical coordinate
    // model; we also write cam.position and ctrl.target. Two writers in the same RAF = jerk.
    // Fix: yield entirely to OrbitControls while orbit is spinning. Corrections only apply
    // when the camera is stationary (orbitCurrent at or near zero).
    if (Math.abs(orbitCurrent) > 0.01) return;

    const cam  = Graph.camera();
    const ctrl = Graph.controls();
    if (!cam || !ctrl) return;

    // Target drift — track actual CoM so orbit pivot doesn't wander.
    // Dead zone: no-op if CoM is within CAM_TARGET_DEAD_ZONE — prevents micro-corrections
    // when the cluster is stable.
    if (frameCount % 4 === 0) {
      const nodes = Graph.graphData().nodes;
      if (nodes.length) {
        const com = centerOfMass(nodes);
        const targetError = Math.hypot(
          com.x - ctrl.target.x,
          com.y - ctrl.target.y,
          com.z - ctrl.target.z,
        );
        if (targetError > CAM_TARGET_DEAD_ZONE) {
          ctrl.target.x += (com.x - ctrl.target.x) * CAM_TARGET_DRIFT;
          ctrl.target.y += (com.y - ctrl.target.y) * CAM_TARGET_DRIFT;
          ctrl.target.z += (com.z - ctrl.target.z) * CAM_TARGET_DRIFT;
        }
      }
    }

    // Distance lerp — guides camera to desired distance when set.
    // Dead zone: no-op if within CAM_DIST_DEAD_ZONE — prevents tiny ticks during orbit
    // from producing visible micro-steps.
    if (camDesiredDist === null) return;
    const dx = cam.position.x - ctrl.target.x;
    const dy = cam.position.y - ctrl.target.y;
    const dz = cam.position.z - ctrl.target.z;
    const dist = Math.hypot(dx, dy, dz) || 1;
    const delta = camDesiredDist - dist;
    if (Math.abs(delta) < CAM_DIST_DEAD_ZONE) return;
    const newDist = Math.max(ctrl.minDistance,
      Math.min(ctrl.maxDistance, dist + delta * CAM_DIST_CORRECTION));
    const f = newDist / dist;
    cam.position.set(ctrl.target.x + dx*f, ctrl.target.y + dy*f, ctrl.target.z + dz*f);
  }

  // ── Orbit speed ramp ──────────────────────────────────────────────────────
  // Ramps orbitCurrent toward orbitTarget each frame (~2 s to full speed at ORBIT_RAMP_RATE).
  // Keeps ctrl.autoRotate true while spinning so OrbitControls applies the rotation.
  function tickOrbitRamp() {
    orbitCurrent += (orbitTarget - orbitCurrent) * ORBIT_RAMP_RATE;
    const ctrl = Graph.controls();
    if (!ctrl) return;
    if (Math.abs(orbitCurrent) < 0.0005) {
      orbitCurrent = 0;
      ctrl.autoRotate = false;
    } else {
      ctrl.autoRotate      = true;
      ctrl.autoRotateSpeed = orbitCurrent;
    }
  }

  // ── Dynamic zoom boundary ─────────────────────────────────────────────────
  // ctrl.maxDistance = camera distance at which all node surfaces are inside the frustum.
  // Recomputed from ACTUAL positions every 60 frames — adjusts as physics settles and
  // as the user resizes the window (aspect ratio changes the constraining FOV).
   function tickMaxZoomBoundary() {
     if (frameCount % 60 !== 0) return;
     const d = actualFitDist();
     if (!d) return;
     Graph.controls().maxDistance = d;
      // Keep camDesiredDist in sync so camera follows the cluster as nodes settle.
      // Lerp toward the new value instead of snapping — prevents the bump that occurs
      // when a 60-frame boundary coincides with orbit, causing tickCameraCorrection
      // to exceed CAM_DIST_DEAD_ZONE and move the camera mid-orbit.
      if (camDesiredDist !== null) {
        camDesiredDist += (d - camDesiredDist) * 0.1; // smooth 10% approach per boundary tick
      }
   }

  // ── Post-settle node correction ───────────────────────────────────────────
  // Runs at 15 fps after CLUSTER_CORRECTION_START_MS. Pure position math — no forces,
  // no velocities, no oscillation. "Desired state → slowly correct" pattern.
  //
  //   Step 1: drift — pull each node toward cluster centre at a rate proportional to
  //           ln(1+val). Heavier nodes drift faster → they accumulate at the centre.
  //
  //   Step 2: separation — for each overlapping pair (r < NODE_SEPARATION), push the
  //           positions apart by half the overlap (d3 forceCollide style).
  //           O(n²) but n ≤ 200 → ~0.1 ms per call.
  function tickNodeCorrection() {
    if (Date.now() < correctionStartTime) return;
    if (frameCount % 4 !== 0) return; // 15 fps
    const nodes = Graph.graphData().nodes;
    if (!nodes.length) return;
    const sep = NODE_SEPARATION;

    for (const n of nodes) {
      const drift = CLUSTER_DRIFT_RATE * Math.log1p(n.val || 1);
      n.x = (n.x || 0) * (1 - drift);
      n.y = (n.y || 0) * (1 - drift);
      n.z = (n.z || 0) * (1 - drift);
    }

    for (let i = 0; i < nodes.length; i++) {
      const ni = nodes[i];
      for (let j = i + 1; j < nodes.length; j++) {
        const nj = nodes[j];
        const dx = (ni.x || 0) - (nj.x || 0);
        const dy = (ni.y || 0) - (nj.y || 0);
        const dz = (ni.z || 0) - (nj.z || 0);
        const r  = Math.hypot(dx, dy, dz) || 1;
        if (r >= sep) continue;
        const push = (sep - r) / (2 * r) * CLUSTER_SEPARATION_STRENGTH;
        ni.x += dx * push;  ni.y += dy * push;  ni.z += dz * push;
        nj.x -= dx * push;  nj.y -= dy * push;  nj.z -= dz * push;
      }
    }
  }

  // ── Zoom momentum ─────────────────────────────────────────────────────────
  // Each frame: move camera along current view ray by the accumulated velocity
  // (log-space, so zoom is multiplicative), then decay the velocity.
  // OrbitControls reads cam.position on its next update() and preserves the
  // new distance because ctrl.enableZoom=false keeps its internal scale=1.
  function tickZoomMomentum() {
    if (Math.abs(zoomVelocity) < 0.0005) { zoomVelocity = 0; return; }
    const c    = Graph.camera();
    const ctrl = Graph.controls();
    if (!c || !ctrl) return;
    const tgt  = ctrl.target;
    const dx   = c.position.x - tgt.x;
    const dy   = c.position.y - tgt.y;
    const dz   = c.position.z - tgt.z;
    const dist = Math.hypot(dx, dy, dz) || 1;
    // Positive velocity = zoom in (camera moves closer).
    const newDist = Math.max(
      ctrl.minDistance,
      Math.min(ctrl.maxDistance, dist * Math.exp(-zoomVelocity)),
    );
    const f = newDist / dist;
    c.position.set(tgt.x + dx * f, tgt.y + dy * f, tgt.z + dz * f);
    zoomVelocity *= SCROLL_ZOOM_COAST;
  }

  // ── Label manager — lazy, throttled, threshold-gated ─────────────────────
  // Runs every LABEL_UPDATE_EVERY_N_FRAMES frames. Skips individual nodes when opacity
  // delta < LABEL_OPACITY_EPSILON — avoids GPU state changes for imperceptible diffs.
  const lastOpacity = new Map(); // nodeId → last written opacity

  function tickLabelManager() {
    const cam = Graph.camera();
    if (!cam) return;
    const FADE_START = LABEL_FADE_START_DIST, FADE_END = LABEL_FADE_END_DIST;
    for (const node of Graph.graphData().nodes) {
      const sprite = node.__threeObj?.children?.[0];
      if (!sprite?.isSprite) continue;
      const d = Math.hypot(
        (node.x || 0) - cam.position.x,
        (node.y || 0) - cam.position.y,
        (node.z || 0) - cam.position.z,
      );
      const opacity = Math.max(0, Math.min(1, (FADE_END - d) / (FADE_END - FADE_START)));
      const prev = lastOpacity.get(node.id) ?? -1;
      if (Math.abs(opacity - prev) < LABEL_OPACITY_EPSILON) continue; // skip imperceptible change
      sprite.material.opacity = opacity;
      sprite.renderOrder = Math.round(100000 / Math.max(d, 1));
      lastOpacity.set(node.id, opacity);
    }
  }

  // ── Self-throttling frame loop ─────────────────────────────────────────────
  // Measures its own wall time. If we exceeded half the frame budget last tick,
  // skip heavy work this tick so the renderer stays unblocked.
  // Priority: zoom adaptation (very slow) > label updates (slow) > nothing.
  // FRAME_BUDGET_MS is defined at module level
  let frameMs = 0;

  (function frame() {
    requestAnimationFrame(frame);
    if (!Graph) return;
    const t0 = performance.now();
    frameCount++;

    // Camera correction runs every frame — desired-state lerp toward CoM and fit distance.
    tickCameraCorrection();

    // Zoom boundary recomputed from actual positions every ~1 s — stays accurate as physics settles.
    tickMaxZoomBoundary();

    // Orbit ramp runs every frame — lerps autoRotateSpeed toward target (flywheel feel).
    tickOrbitRamp();

    // Zoom momentum runs every frame — moves camera along view ray, decays velocity.
    tickZoomMomentum();

    // Post-settle correction: gentle drift + separation at 15 fps after d3 alpha decays.
    tickNodeCorrection();

    // Label updates: every LABEL_UPDATE_EVERY_N_FRAMES frames, skip if last frame was expensive.
    if (frameCount % LABEL_UPDATE_EVERY_N_FRAMES === 0 && frameMs < FRAME_BUDGET_MS) {
      tickLabelManager();
    }

    frameMs = performance.now() - t0;
  })();

  loadMacro().catch(e => log.error('boot error=%s', e.message));
}

// Expose virtualCenter accessor for browser tests.
export function getOrbitPivot() { return state?.virtualCenter; }
