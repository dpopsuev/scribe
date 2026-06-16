/**
 * shelf-layout.js — Shelf layout engine for the bookshelf view.
 *
 * Assigns nodes to horizontal shelves (fixed Y) based on the active lens.
 * Within each shelf, nodes spread along X via force-directed simulation.
 * Handles animated transitions when the lens changes.
 */

import { kindToLayer } from './lens.js';
import { createLogger } from './logger.js';

const log = createLogger('shelf');

const DEFAULT_SPACING = 120;     // world units between shelves
const TRANSITION_MS   = 500;     // lens switch animation duration
const SHELF_WIDTH     = 800;     // world units — width of indicator planes
const SHELF_DEPTH     = 2;       // world units — thickness of indicator planes
const LABEL_SIZE      = 20;      // world units — label sprite height

const SHELF_HEX_COLORS = [
  0x6366f1,   // indigo
  0x10b981,   // emerald
  0xf59e0b,   // amber
  0xec4899,   // pink
  0x3b82f6,   // blue
  0x8b5cf6,   // purple
  0x0ea5e9,   // sky
];

// ── Shelf assignment ─────────────────────────────────────────────────────

/**
 * Pin each node's Y to its shelf level. Sets node.fy (d3 fixed Y)
 * and node._shelfIndex for rendering.
 *
 * @param {Array} nodes
 * @param {object} lens — active Lens from LensResolver
 * @param {number} spacing — world units between shelves
 */
export function assignShelves(nodes, lens, spacing = DEFAULT_SPACING) {
  if (!lens) return;
  let assigned = 0;
  for (const node of nodes) {
    const idx = kindToLayer(lens, node.kind);
    node.fy = idx * spacing;
    node._shelfIndex = idx;
    assigned++;
  }
  log.info('assignShelves lens=%s nodes=%d layers=%d spacing=%d',
    lens.name, assigned, lens.layers.length, spacing);
}

/**
 * Clear shelf assignments — unpin Y for all nodes.
 */
export function clearShelves(nodes) {
  for (const node of nodes) {
    delete node.fy;
    delete node._shelfIndex;
    delete node._fyTarget;
    delete node._fyStart;
    delete node._transitionStart;
  }
}

// ── Animated transitions ─────────────────────────────────────────────────

/**
 * Start an animated transition to a new lens. Stores transition state
 * on each node; tickShelfTransition() applies the lerp each frame.
 *
 * @param {Array} nodes
 * @param {object} newLens
 * @param {number} spacing
 * @param {number} durationMs
 */
export function transitionShelves(nodes, newLens, spacing = DEFAULT_SPACING, durationMs = TRANSITION_MS) {
  if (!newLens) return;
  const now = performance.now();
  for (const node of nodes) {
    const targetIdx = kindToLayer(newLens, node.kind);
    node._fyTarget = targetIdx * spacing;
    node._fyStart = node.fy ?? node.y ?? 0;
    node._transitionStart = now;
    node._shelfIndex = targetIdx;
  }
  log.info('transitionShelves lens=%s duration=%dms', newLens.name, durationMs);
}

/**
 * Per-frame tick — lerps node.fy toward target. Call from graph.js frame loop.
 * Returns true while any transition is active.
 *
 * @param {Array} nodes
 * @param {number} durationMs
 * @returns {boolean} — true if transitions are still running
 */
export function tickShelfTransition(nodes, durationMs = TRANSITION_MS) {
  const now = performance.now();
  let active = false;
  for (const node of nodes) {
    if (node._fyTarget == null) continue;
    const elapsed = now - (node._transitionStart || now);
    const t = Math.min(1, elapsed / durationMs);
    const eased = 1 - Math.pow(1 - t, 3); // ease-out cubic
    node.fy = node._fyStart + (node._fyTarget - node._fyStart) * eased;
    if (t >= 1) {
      node.fy = node._fyTarget;
      node._fyTarget = null;
    } else {
      active = true;
    }
  }
  return active;
}

// ── Shelf visual indicators ──────────────────────────────────────────────

/**
 * Create Three.js shelf indicator planes and labels for each layer.
 *
 * @param {object} THREE — Three.js library
 * @param {object} scene — Three.js scene
 * @param {object} lens — active Lens
 * @param {number} spacing — world units between shelves
 * @returns {Function} cleanup — call to remove all indicators from scene
 */
export function createShelfIndicators(THREE, scene, lens, spacing = DEFAULT_SPACING) {
  if (!THREE || !scene || !lens) return () => {};

  const meshes = [];
  const totalLayers = lens.layers.length + 1; // +1 for fallback

  for (let i = 0; i < lens.layers.length; i++) {
    const y = i * spacing;
    const layer = lens.layers[i];
    const colorIdx = i % SHELF_HEX_COLORS.length;

    // Bright horizontal line at each shelf level
    const points = [
      new THREE.Vector3(-SHELF_WIDTH / 2, y, 0),
      new THREE.Vector3(SHELF_WIDTH / 2, y, 0),
    ];
    const lineGeo = new THREE.BufferGeometry().setFromPoints(points);
    const lineMat = new THREE.LineBasicMaterial({
      color: SHELF_HEX_COLORS[colorIdx],
      transparent: true,
      opacity: 0.25,
      depthWrite: false,
    });
    const line = new THREE.Line(lineGeo, lineMat);
    scene.add(line);
    meshes.push(line);

    // Shelf label at left edge — larger and brighter
    const label = makeShelfLabel(THREE, layer.label, i, totalLayers);
    label.position.set(-SHELF_WIDTH / 2 - 40, y, 0);
    scene.add(label);
    meshes.push(label);
  }

  log.info('createShelfIndicators layers=%d', lens.layers.length);

  return function cleanup() {
    for (const m of meshes) {
      scene.remove(m);
      if (m.geometry) m.geometry.dispose();
      if (m.material) {
        if (m.material.map) m.material.map.dispose();
        m.material.dispose();
      }
    }
    meshes.length = 0;
  };
}

function makeShelfLabel(THREE, text, index, total) {
  const canvas = document.createElement('canvas');
  canvas.width = 256;
  canvas.height = 48;
  const ctx = canvas.getContext('2d');

  ctx.clearRect(0, 0, 256, 48);
  ctx.fillStyle = 'rgba(5,5,20,0.85)';
  roundRect(ctx, 0, 0, 256, 48, 6);
  ctx.fill();

  ctx.font = 'bold 20px system-ui, sans-serif';
  ctx.fillStyle = '#e2e8f0';
  ctx.textAlign = 'right';
  ctx.textBaseline = 'middle';
  ctx.fillText(text, 248, 24);

  const texture = new THREE.CanvasTexture(canvas);
  const material = new THREE.SpriteMaterial({
    map: texture,
    transparent: true,
    depthTest: false,
  });
  const sprite = new THREE.Sprite(material);
  sprite.scale.set(LABEL_SIZE * 2, LABEL_SIZE * 0.5, 1);
  return sprite;
}

function roundRect(ctx, x, y, w, h, r) {
  ctx.beginPath();
  ctx.moveTo(x + r, y);
  ctx.lineTo(x + w - r, y);
  ctx.arcTo(x + w, y, x + w, y + r, r);
  ctx.lineTo(x + w, y + h - r);
  ctx.arcTo(x + w, y + h, x + w - r, y + h, r);
  ctx.lineTo(x + r, y + h);
  ctx.arcTo(x, y + h, x, y + h - r, r);
  ctx.lineTo(x, y + r);
  ctx.arcTo(x, y, x + r, y, r);
  ctx.closePath();
}

/**
 * Compute the vertical center of all shelves — useful for camera positioning.
 */
export function shelfCenterY(lens, spacing = DEFAULT_SPACING) {
  if (!lens || !lens.layers.length) return 0;
  return ((lens.layers.length - 1) * spacing) / 2;
}
