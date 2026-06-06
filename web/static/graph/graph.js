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
         forceSelfGravity }                     from './physics.js';
import { glowColor, glowConfig }               from './glow.js';
import { fetchScopeGraph, fetchKindGraph,
         fetchArtifactGraph,
         patchArtifact }                        from './api.js';
import { setModeBadge, depthFromExpanded, setStats,
         renderExpandedTags, openSidebar, closeSidebar,
         showContextMenu }                       from './ui.js';
import { KindColorRenderer }                   from './renderer.js';
import { createLogger }                        from './logger.js';
import { createGraphState }                    from './graph-state.js';

const log = createLogger('graph');

const GRAPH_BG          = '#05050f';
const UNIVERSE_RADIUS   = 180;
const MINI_KIND_R       = 55;
const MINI_ART_R        = 28;
const VC_LERP           = 0.015;


const DEFAULT_STATUSES = 'active,draft,current,proposed,in_progress,in_review,fleeting';

let state = null;   // createGraphState() — one per initGraph call
let deps  = {};     // injected CDN globals
let palette = null; // built from culori + GRAPH_BG
let renderer = null;

let Graph = null;


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

  // N-body physics: Plummer-softened gravity toward origin (dense core) +
  // short-range repulsion (prevents collapse). No radial shell constraint.
  Graph.d3Force('center',  null);
  Graph.d3Force('radial',  null);
  Graph.d3Force('gravity', forceSelfGravity(0.12, 40, 'val'));
  Graph.d3Force('charge')?.strength?.(-80);
  Graph.d3Force('charge')?.distanceMax?.(180);

  state.macroData = { nodes: sorted, links: data.links };
  state.expandedScopes.clear();
  state.expandedKinds.clear();
  clearBubbles();
  applyGraphData();
  updateModeBadge();

  // Camera aims immediately then re-aims after physics settles.
  // fz=0 pins nodes to XY plane so re-aim is safe (no 3D scatter).
  aimAtCenterOfMass(0);
  setTimeout(() => aimAtCenterOfMass(800), 3000);
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
  kindData.nodes = placeInMiniSphere(kindData.nodes, kindData.links, anchor, MINI_KIND_R);
  for (const n of kindData.nodes) {
    state.scopeSpherePos.set('kind:' + scopeName + ':' + (n.group || n.kind), { x: n.x, y: n.y, z: n.z });
  }

  state.expandedScopes.set(scopeName, kindData);
  createBubble(scopeName, MINI_KIND_R + 10);
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
  artData.nodes = placeInMiniSphere(artData.nodes, artData.links, anchor, MINI_ART_R);

  state.expandedKinds.set(key, artData);
  createBubble('kind:' + key, MINI_ART_R + 10);
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


function aimAtCenterOfMass(animMs = 0) {
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
  const camDist = Math.min(radius * 3.5, UNIVERSE_RADIUS * 5);
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
  const startPos = { x: cam.x, y: cam.y, z: cam.z };
  const startTgt = { x: controls.target.x, y: controls.target.y, z: controls.target.z };
  const t0 = performance.now();
  const ease = t => t < 0.5 ? 2*t*t : -1+(4-2*t)*t;
  const lerp = (a, b, t) => a + (b-a)*t;
  (function tick() {
    const t = ease(Math.min((performance.now()-t0)/animMs, 1));
    controls.object.position.set(lerp(startPos.x,targetPos.x,t), lerp(startPos.y,targetPos.y,t), lerp(startPos.z,targetPos.z,t));
    controls.target.set(lerp(startTgt.x,com.x,t), lerp(startTgt.y,com.y,t), lerp(startTgt.z,com.z,t));
    controls.update();
    if (t < 1) requestAnimationFrame(tick);
  })();
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
  state.virtualCenter.x += (com.x - state.virtualCenter.x) * VC_LERP;
  state.virtualCenter.y += (com.y - state.virtualCenter.y) * VC_LERP;
  state.virtualCenter.z += (com.z - state.virtualCenter.z) * VC_LERP;
  tickGlows();
}


function onNodeClickWithDbl(node) {
  const now = Date.now();
  if (state.lastClick.node === node && now - state.lastClick.time < 300) {
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
    .linkCurvature(l => l.relation === 'depends_on' ? 0.15 : 0)
    .d3AlphaDecay(0.004)
    .d3VelocityDecay(0.25)
    .warmupTicks(80)
    .cooldownTime(Infinity)
    .onNodeClick(onNodeClickWithDbl)
    .onNodeRightClick(onNodeRightClick)
    .onBackgroundClick((() => {
      let lastBgClick = 0;
      return () => {
        closeSidebar(state.els.sidebar);
        const now = Date.now();
        if (now - lastBgClick < 300) aimAtCenterOfMass(600);
        lastBgClick = now;
      };
    })())
    .onEngineTick(onTick);

  // Allow close zoom and fast scroll.
  const cam = Graph.camera();
  cam.near = 0.1;
  cam.updateProjectionMatrix();
  const ctrl = Graph.controls();
  ctrl.minDistance = 0;
  ctrl.zoomSpeed   = 3.0;

  // Resize handler.
  window.addEventListener('resize', () =>
    Graph.width(window.innerWidth).height(window.innerHeight));

  // Load scope data.
  loadMacro().catch(e => log.error('boot error=%s', e.message));
}

// Expose virtualCenter accessor for browser tests.
export function getVirtualCenter() { return state?.virtualCenter; }
