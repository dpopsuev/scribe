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
         forceSelfGravity,
         forcesForDist,
         clusterMaxRadius,
         clusterRadiusFromVolume,
         forceRadiusCap,
         computeFitDistance,
         computeFitDistanceForCount,
         ZOOM_MIN_DIST,
         ZOOM_MAX_DIST }                         from './physics.js';
import { glowColor, glowConfig }               from './glow.js';
import { fetchScopeGraph, fetchKindGraph,
         fetchArtifactGraph,
         patchArtifact }                        from './api.js';
import { setModeBadge, depthFromExpanded, setStats,
         renderExpandedTags, openSidebar, closeSidebar,
         showContextMenu }                       from './ui.js';
import { KindColorRenderer, NODE_SIZE_MIN, NODE_SIZE_MAX } from './renderer.js';
import { createLogger }                        from './logger.js';
import { createGraphState }                    from './graph-state.js';

const log = createLogger('graph');

const GRAPH_BG             = '#05050f';
const UNIVERSE_RADIUS      = 180;   // world units — scope sphere initial placement radius
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
const CAMERA_DIST_MAX      = UNIVERSE_RADIUS * 3; // fallback cap when no fit distance known
const CAMERA_MIN_DIST      = 10;   // prevents camera passing through node spheres

// ── Force initial state ────────────────────────────────────────────────────
const GRAVITY_INIT         = 0.12;  // starting gravity strength (mid-range zoom)
const REPULSION_INIT       = -80;   // starting repulsion strength (mid-range zoom)
const DMAX_INIT            = 180;   // starting repulsion reach in world units (mid-range zoom)
const GRAVITY_SOFTENING    = 40;    // Plummer softening radius — prevents singularity at origin
const GRAVITY_REHEAT_DELTA = 0.005; // minimum G change that warrants a physics reheat

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
const RECENTER_ANIM_MS     = 1000;  // camera fly to centre on background double-click
const SETTLE_ANIM_MS       = 1800;  // camera re-aim after physics settle
const HOME_ANIM_MS         = 2500;  // camera fly-home on idle timeout

// ── Link curves ────────────────────────────────────────────────────────────
const DEPENDS_ON_CURVATURE = 0.15;  // arc bend for depends_on edges — shows direction clearly

// ── Idle orbit ─────────────────────────────────────────────────────────────
// Speed ramps up/down each frame (flywheel feel) — never snaps on or off.
const ORBIT_CRUISE_SPEED   = 0.4;   // deg/s at cruise — slow enough to feel calm
const ORBIT_RAMP_RATE      = 0.025; // fraction of speed gap closed per frame (~2 s to full speed)
const IDLE_ORBIT_MS        = 4000;  // idle before spin begins
const IDLE_HOME_MS         = 20000; // idle before camera flies home
const FIT_ALL_PADDING      = 1.8;   // cluster fills ~58% of FOV — comfortable breathing room

// ── Physics settle delay ───────────────────────────────────────────────────
const PHYSICS_SETTLE_MS    = 3000;  // wait this long after data load before re-aiming camera


const DEFAULT_STATUSES = 'active,draft,current,proposed,in_progress,in_review,fleeting';

// Computes the visual nodeVal for one node — identical to renderer.js nodeVal lambda.
// Used to derive cluster radius and camera distance from actual node sizes, not just count.
// NODE_SIZE_MIN / NODE_SIZE_MAX imported from renderer.js — single source of truth.
function nodeVisualVolume(node) {
  return Math.max(NODE_SIZE_MIN, Math.min(NODE_SIZE_MAX, Math.cbrt(node.val || 1) * 2));
}

// Sum of nodeVisualVolume across all nodes — the shared input for radius and camera distance.
function totalNodeVolume(nodes) {
  return nodes.reduce((s, n) => s + nodeVisualVolume(n), 0);
}

let state = null;   // createGraphState() — one per initGraph call
let deps  = {};     // injected CDN globals
let palette = null; // built from culori + GRAPH_BG
let renderer = null;
let Graph = null;
let fitAllNodesFn  = null; // set by initGraph; called from loadMacro after data is loaded
let enableOrbitFn  = null; // set by initGraph; called from loadMacro to start spinning on boot

// Active camera animation — null when idle.
// tickCamAnim() reads this each frame and clears it when done.
// Only distance + target are animated; autoRotate owns the direction.
let camAnim = null;

// Zoom-adaptive force state — module-level so loadMacro() can reference them.
// loadMacro is a module-scope async function; variables declared inside
// initGraph() are not in its closure scope.
let currentG    = GRAVITY_INIT;
let currentRep  = REPULSION_INIT;
let currentDmax = DMAX_INIT;
let gravityForce  = null; // created inside initGraph once forceSelfGravity is ready
let capForce      = null; // forceRadiusCap — prevents unbounded scatter

function mergedGraphData() {
  const nodes = [...state.macroData.nodes];
  const links = [...state.macroData.links];
  for (const [, d] of state.expandedScopes) { nodes.push(...d.nodes); links.push(...d.links); }
  for (const [, d] of state.expandedKinds)  { nodes.push(...d.nodes); links.push(...d.links); }
  return { nodes, links };
}

function applyGraphData() {
  const data = mergedGraphData();
  log.info('applyGraphData nodes=%d links=%d', data.nodes.length, data.links.length);
  Graph.graphData(data);
  // no post-processing — renderer owns appearance only, not material patching
  const { nodes, links } = Graph.graphData();
  setStats(state.els.stats, nodes.length, links.length);
  renderExpandedTags(state.els.expandedWrap, state.els.expandedList, state.expandedScopes, state.expandedKinds,
    collapseScope, (sc, kind) => {
      state.expandedKinds.delete(sc + ':' + kind);
      removeBubble('kind:' + sc + ':' + kind);
      applyGraphData();
      updateModeBadge();
    });
  setTimeout(rebuildGlows, 50);
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

  Graph.d3Force('center',  null);
  Graph.d3Force('radial',  null);
  Graph.d3Force('gravity', gravityForce);
  Graph.d3Force('cap',     capForce);
  Graph.d3Force('charge')?.strength?.(currentRep);
  Graph.d3Force('charge')?.distanceMax?.(currentDmax);

  // Give renderer the full node set so it can normalise sizes and pre-build textures.
  renderer.init(sorted);

  state.macroData = { nodes: sorted, links: data.links };
  state.expandedScopes.clear();
  state.expandedKinds.clear();
  clearBubbles();
  applyGraphData();
  updateModeBadge();

  // Camera: FOV-accurate fit immediately, then smooth re-fit after physics settles.
  // fitAllNodesFn is set by initGraph; null-guard for the case loadMacro races before init.
  if (fitAllNodesFn) {
    fitAllNodesFn(0);
    setTimeout(() => fitAllNodesFn(SETTLE_ANIM_MS), PHYSICS_SETTLE_MS);
  } else {
    aimAtCenterOfMass(0);
    setTimeout(() => aimAtCenterOfMass(SETTLE_ANIM_MS), PHYSICS_SETTLE_MS);
  }
  if (enableOrbitFn) enableOrbitFn();
}


async function expandScope(scopeName) {
  if (state.expandedScopes.has(scopeName)) return;
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
  log.info('expandScope scope=%s kinds=%d', scopeName, kindData.nodes.length);
  removeMacroNode('scope:' + scopeName);

  const anchor = state.scopeSpherePos.get(scopeName) || { x: 0, y: 0, z: 0 };
  kindData.nodes = placeInMiniSphere(kindData.nodes, kindData.links, anchor, KIND_SPHERE_RADIUS);
  for (const n of kindData.nodes) {
    state.scopeSpherePos.set('kind:' + scopeName + ':' + (n.group || n.kind), { x: n.x, y: n.y, z: n.z });
  }

  state.expandedScopes.set(scopeName, kindData);
  createBubble(scopeName, KIND_SPHERE_RADIUS + 10);
  applyGraphData();
  updateModeBadge();
}


async function expandKind(scopeName, kindName) {
  const key = scopeName + ':' + kindName;
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

  const kindNodeId = 'kind:' + key;
  if (state.expandedScopes.has(scopeName)) {
    const sd = state.expandedScopes.get(scopeName);
    sd.nodes = sd.nodes.filter(n => n.id !== kindNodeId);
    sd.links = sd.links.filter(l => l.source !== kindNodeId && l.target !== kindNodeId &&
      l.source?.id !== kindNodeId && l.target?.id !== kindNodeId);
  }

  const anchor = state.scopeSpherePos.get(kindNodeId) || state.scopeSpherePos.get(scopeName) || { x: 0, y: 0, z: 0 };
  artData.nodes = placeInMiniSphere(artData.nodes, artData.links, anchor, ARTIFACT_SPHERE_RADIUS);

  state.expandedKinds.set(key, artData);
  createBubble('kind:' + key, ARTIFACT_SPHERE_RADIUS + 10);
  applyGraphData();
  updateModeBadge();
}


async function collapseScope(scopeName) {
  log.info('collapseScope scope=%s', scopeName);
  for (const key of [...state.expandedKinds.keys()]) {
    if (key.startsWith(scopeName + ':')) {
      state.expandedKinds.delete(key);
      removeBubble('kind:' + key);
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


// distOverride: when set by fitAllNodes(), uses FOV-computed distance instead of radius*mult.
function aimAtCenterOfMass(animMs = 0, distOverride = null) {
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
  const targetPos = { x: com.x+dx*scale, y: com.y+dy*scale, z: com.z+dz*scale };

  if (animMs <= 0) {
    controls.object.position.set(targetPos.x, targetPos.y, targetPos.z);
    controls.target.set(com.x, com.y, com.z);
    controls.update();
    log.info('aimCamera com=(%d,%d,%d) radius=%d camDist=%d cam=(%d,%d,%d)',
      Math.round(com.x), Math.round(com.y), Math.round(com.z),
      Math.round(radius), Math.round(camDist),
      Math.round(targetPos.x), Math.round(targetPos.y), Math.round(targetPos.z));
    return;
  }

  // Animated: record start/end state for tickCamAnim() to step each frame.
  // Only distance and target are animated — autoRotate keeps driving the direction,
  // so orbit continues uninterrupted while the camera glides to its new position.
  const startDist = Math.hypot(cam.x - controls.target.x, cam.y - controls.target.y, cam.z - controls.target.z);
  camAnim = {
    startDist,
    targetDist: camDist,
    startTarget: { x: controls.target.x, y: controls.target.y, z: controls.target.z },
    endTarget:   { x: com.x, y: com.y, z: com.z },
    t0:          performance.now(),
    duration:    animMs,
  };
}


function createBubble(key, initialRadius = 100) {
  if (!deps.THREE) return;
  const scene = Graph.scene();
  const color = new deps.THREE.Color(palette?.kinds?.scope?.hex || '#6366f1');
  for (const [bkey, cfg] of [[key, { opacity: 0.04, side: deps.THREE.BackSide }], [key+'_wire', { opacity: 0.1, wireframe: true }]]) {
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
  for (const k of [key, key+'_wire']) {
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
  for (const k of [key, key+'_wire']) {
    const m = state.scopeBubbles.get(k);
    if (m) { m.position.set(cx,cy,cz); m.scale.setScalar(r); }
  }
}




function rebuildGlows() {
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
    inner.userData = { cfg, amp: cfg.innerAmp };
    outer.userData = { cfg, amp: cfg.outerAmp };
    scene.add(inner); scene.add(outer);
    state.glowMeshes.set(node.id, { node, inner, outer });
  }
}

function tickGlows() {
  const t = performance.now() / 1000;
  for (const { node, inner, outer } of state.glowMeshes.values()) {
    const nx=node.x||0, ny=node.y||0, nz=node.z||0;
    inner.position.set(nx,ny,nz); outer.position.set(nx,ny,nz);
    const phase = Math.sin(t * inner.userData.cfg.freq * Math.PI * 2) * 0.5 + 0.5;
    inner.material.opacity = inner.userData.amp * phase;
    outer.material.opacity = outer.userData.amp * phase * 0.6;
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
    fitBubble('kind:'+key, Graph.graphData().nodes.filter(n => n.scope===sc && n.kind===kind), 25);
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


function activeRelations() {
  return [...document.querySelectorAll('.rel-toggle.active')].map(el => el.dataset.rel);
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
  renderer.apply(graphBuilder);

  Graph = window._Graph = graphBuilder
    .nodeLabel(n => {
      const title = n.kind === 'scope'
        ? `<strong>${n.name}</strong><br><span style="opacity:0.7">${n.val} artifacts — click to expand</span>`
        : `<strong>${n.id}</strong><br>${n.name}`;
      return `<div style="background:rgba(0,0,0,0.85);color:#e2e8f0;padding:5px 9px;border-radius:5px;font-size:12px;pointer-events:none;max-width:260px">${title}</div>`;
    })
    .nodeResolution(12)
    // link appearance owned by renderer
    .linkDirectionalParticles(l => l.relation === 'depends_on' ? 2 : 0)
    .linkDirectionalParticleSpeed(0.004)
    .linkDirectionalParticleWidth(1.5)
    .linkCurvature(l => l.relation === 'depends_on' ? DEPENDS_ON_CURVATURE : 0)
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
        if (now - lastBgClick < DOUBLE_CLICK_MAX_MS) aimAtCenterOfMass(RECENTER_ANIM_MS);
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

  // Resize handler.
  window.addEventListener('resize', () =>
    Graph.width(window.innerWidth).height(window.innerHeight));

  // ── Fit all nodes ──────────────────────────────────────────────────────
  // Positions camera so the entire node cluster fills the viewport.
  // Uses the camera's actual FOV to compute exact distance — guarantees
  // every node is visible with FIT_ALL_PADDING breathing room.
  function fitAllNodes(animMs = 800) {
    const nodes = Graph.graphData().nodes;
    if (!nodes.length) return;
    // Cluster radius and camera distance both derive from total node visual volume —
    // same formula as the renderer, same formula as the force cap in loadMacro.
    // No static distance cap: ctrl.maxDistance becomes the zoom-out boundary instead.
    const vol     = totalNodeVolume(nodes);
    const radius  = clusterRadiusFromVolume(vol);
    const fitDist = computeFitDistance(radius, Graph.camera().fov, FIT_ALL_PADDING);
    Graph.controls().maxDistance = fitDist;
    aimAtCenterOfMass(animMs, fitDist);
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
    fitAllNodes(HOME_ANIM_MS);
    enableOrbit();
  }

  function resetIdleTimers() {
    clearTimeout(orbitTimer);
    clearTimeout(homeTimer);
    orbitTimer = setTimeout(enableOrbit, IDLE_ORBIT_MS);
    homeTimer  = setTimeout(goHome,      IDLE_HOME_MS);
  }

  function onInteraction() {
    orbitTarget = 0; // ramp-down starts in tickOrbitRamp
    resetIdleTimers();
  }

  // Zoom momentum state — accumulates scroll velocity, decays each frame.
  let zoomVelocity = 0;

  const graphRoot = document.getElementById('graph-root');
  if (graphRoot) {
    graphRoot.addEventListener('pointerdown', onInteraction);
    // Native zoom is disabled (ctrl.enableZoom=false); we drive it with momentum.
    // positive deltaY = scroll down = zoom out (increase distance).
    graphRoot.addEventListener('wheel', (e) => {
      e.preventDefault();
      zoomVelocity += (e.deltaY > 0 ? 1 : -1) * SCROLL_ZOOM_IMPULSE;
      onInteraction();
    }, { passive: false });
  }

  // On boot: fit all nodes first (camera already positioned by aimAtCenterOfMass,
  // but recalculate with exact FOV), then start idle timers.
  // fitAllNodes is called from loadMacro after data is loaded — no race with setTimeout.
  setTimeout(resetIdleTimers, 600);

  // ── Per-frame adaptive systems ────────────────────────────────────────────
  let smoothDist = null;
  let frameCount = 0;
  let lastForceDist = null;

  // gravityForce/currentG/Rep/Dmax are module-level — initialise forces now.
  gravityForce = forceSelfGravity(currentG, GRAVITY_SOFTENING, 'val');
  capForce     = forceRadiusCap(UNIVERSE_RADIUS); // placeholder radius; loadMacro sets real value

  // ── Zoom adaptor (SRP: only touches forces) ───────────────────────────────
  // Runs every frame. Smooths camera distance (EMA), lerps current force
  // parameters toward the desired state, skips writes below dead zone.
  //
  // Dead zone (sensitivity=0.05): if zoom changed < 5% from last applied
  // distance, forcesForDist returns null → lerp target stays unchanged.
  // This prevents micro-jitter from continuously nudging forces.
  const LERP = FORCE_ADAPT_RATE;
  const SENSITIVITY = FORCE_DEAD_ZONE;

  function tickZoomAdaptor() {
    const cam  = Graph.camera();
    const ctrl = Graph.controls();
    if (!cam || !ctrl) return;

    const rawDist = Math.hypot(
      cam.position.x - ctrl.target.x,
      cam.position.y - ctrl.target.y,
      cam.position.z - ctrl.target.z,
    );

    // EMA smoothing — α=0.08 ≈ 12 frames lag, filters per-frame scroll jitter.
    smoothDist = smoothDist == null ? rawDist : smoothDist * (1 - CAMERA_DIST_SMOOTHING) + rawDist * CAMERA_DIST_SMOOTHING;

    // Compute desired state. Returns null if within dead zone (< 5% change).
    const desired = forcesForDist(smoothDist, ZOOM_MIN_DIST, ZOOM_MAX_DIST, SENSITIVITY, lastForceDist);
    if (desired !== null) lastForceDist = smoothDist;

    // Lerp current toward desired (or hold if in dead zone — target unchanged).
    const targetG    = desired ? desired.G    : currentG;
    const targetRep  = desired ? desired.rep  : currentRep;
    const targetDmax = desired ? desired.dmax : currentDmax;

    const prevG = currentG;
    currentG    += (targetG    - currentG)    * LERP;
    currentRep  += (targetRep  - currentRep)  * LERP;
    currentDmax += (targetDmax - currentDmax) * LERP;

    // Update forces in-place — no re-registration, no allocation.
    gravityForce.setG(currentG);
    Graph.d3Force('charge')?.strength?.(currentRep);
    Graph.d3Force('charge')?.distanceMax?.(currentDmax);

    // Physics must be running for force changes to move nodes.
    // Without d3AlphaTarget (not in 3d-force-graph 1.80.0), reheat when
    // G changes meaningfully so nodes drift to the new equilibrium.
    if (Math.abs(currentG - prevG) > GRAVITY_REHEAT_DELTA) {
      Graph.d3ReheatSimulation();
    }
  }

  // ── Camera position animation ─────────────────────────────────────────────
  // Steps camAnim each frame — animates only radial distance + target point.
  // Direction is left to OrbitControls so autoRotate keeps running during fly.
  const ease = t => (1 - Math.cos(Math.PI * t)) / 2; // easeInOutSine
  const lerp = (a, b, t) => a + (b - a) * t;

  function tickCamAnim() {
    if (!camAnim) return;
    const cam  = Graph.camera();
    const ctrl = Graph.controls();
    if (!cam || !ctrl) return;

    const t = ease(Math.min((performance.now() - camAnim.t0) / camAnim.duration, 1));

    // Step target point.
    ctrl.target.set(
      lerp(camAnim.startTarget.x, camAnim.endTarget.x, t),
      lerp(camAnim.startTarget.y, camAnim.endTarget.y, t),
      lerp(camAnim.startTarget.z, camAnim.endTarget.z, t),
    );

    // Step distance along whatever direction autoRotate has landed the camera.
    const dx = cam.position.x - ctrl.target.x;
    const dy = cam.position.y - ctrl.target.y;
    const dz = cam.position.z - ctrl.target.z;
    const currentDist = Math.hypot(dx, dy, dz) || 1;
    const newDist = lerp(camAnim.startDist, camAnim.targetDist, t);
    const f = newDist / currentDist;
    cam.position.set(ctrl.target.x + dx * f, ctrl.target.y + dy * f, ctrl.target.z + dz * f);

    if (t >= 1) camAnim = null;
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
  // Runs every LABEL_INTERVAL frames. Skips individual nodes when opacity
  // delta < OPACITY_EPSILON — avoids GPU state changes for imperceptible diffs.
  const LABEL_INTERVAL  = LABEL_UPDATE_EVERY_N_FRAMES;
  const OPACITY_EPSILON = LABEL_OPACITY_EPSILON;
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
      if (Math.abs(opacity - prev) < OPACITY_EPSILON) continue; // skip imperceptible change
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

    // Camera animation runs every frame — steps distance + target, preserves orbit direction.
    tickCamAnim();

    // Orbit ramp runs every frame — lerps autoRotateSpeed toward target (flywheel feel).
    tickOrbitRamp();

    // Zoom momentum runs every frame — moves camera along view ray, decays velocity.
    tickZoomMomentum();

    // Zoom adaptation runs every frame — pure lerp + dead zone, O(1), ~0.1ms.
    tickZoomAdaptor();

    // Label updates: every LABEL_INTERVAL frames, skip if last frame was expensive.
    if (frameCount % LABEL_INTERVAL === 0 && frameMs < FRAME_BUDGET_MS) {
      tickLabelManager();
    }

    frameMs = performance.now() - t0;
  })();

  loadMacro().catch(e => log.error('boot error=%s', e.message));
}

// Expose virtualCenter accessor for browser tests.
export function getVirtualCenter() { return state?.virtualCenter; }
