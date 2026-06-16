/**
 * renderer.js — Plug-and-play node and link appearance for ForceGraph3D.
 *
 * Single Responsibility: each renderer configures ONLY how nodes and links look.
 * Open/Closed: new renderers extend BaseRenderer without touching graph.js.
 * Liskov: any renderer can replace any other — same interface, same data.
 * Interface Segregation: one method — apply(graphBuilder) → graphBuilder.
 * Dependency Inversion: graph.js depends on the BaseRenderer interface,
 *   not on concrete color logic.
 *
 * graph.js owns: data fetching, physics, camera, lifecycle.
 * Renderer owns: nodeColor, nodeVal, nodeRelSize, nodeOpacity, linkColor, linkWidth, linkOpacity.
 */

import { buildPalette } from './palette.js';

// ── Node appearance ────────────────────────────────────────────────────────
export const SPHERE_SCALE  = 6;    // ForceGraph3D nodeRelSize — world radius = cbrt(nodeVal) × this
const NODE_OPACITY         = 0.95; // slight transparency improves depth perception
const SPHERE_SEGMENTS      = 16;   // longitude/latitude divisions — smoother sphere edges
const ARROW_HEAD_SIZE      = 4;    // world units — depends_on arrow head length
export const NODE_SIZE_MIN = 2;    // nodeVal floor — prevents invisible micro-nodes
export const NODE_SIZE_MAX = 40;   // nodeVal ceiling — prevents nodes swallowing links

export function nodeVal(node) {
  return Math.max(NODE_SIZE_MIN, Math.min(NODE_SIZE_MAX, Math.cbrt(node.val || 1) * 2));
}
const ARTIFACT_COUNT_DIVISOR = 20; // tooltip val = raw artifact count ÷ this

// ── Link appearance ────────────────────────────────────────────────────────
const LINK_WIDTH_PRIMARY   = 1.5;  // world units — default cross-scope / dependency links
const LINK_WIDTH_SECONDARY = 0.6;  // world units — parent_of (subordinate visual weight)
const LINK_WIDTH_ACCENT    = 2.0;  // world units — depends_on, implements (strong semantic weight)

// ── Label canvas ───────────────────────────────────────────────────────────
const LABEL_SPRITE_SIZE    = 28;   // world units — height of the floating name sprite

// ── Health color scale (violations → Oklch hue) ───────────────────────────
// 0 violations: kind color unchanged
// 1–3: amber (H=45°) — needs attention
// 4+:  red   (H=12°) — broken
const HEALTH_HUES = { ok: null, warn: 45, error: 12 };
const HEALTH_VIOLATION_WARN_THRESHOLD  = 1;
const HEALTH_VIOLATION_ERROR_THRESHOLD = 4;

function healthHue(violations) {
  if (!violations || violations < HEALTH_VIOLATION_WARN_THRESHOLD)  return HEALTH_HUES.ok;
  if (violations < HEALTH_VIOLATION_ERROR_THRESHOLD)                 return HEALTH_HUES.warn;
  return HEALTH_HUES.error;
}

// ── Link colours by relation ───────────────────────────────────────────────
// Semantic color coding: each relation family gets a distinct hue.
// Structural edges are muted; semantic edges are vivid.
// Palette limited to 7 distinct hues for accessibility (grayscale-safe via opacity).
export const LINK_COLORS = {
  'contains':    'rgba(160,170,190,0.12)',   // near-invisible — hierarchy scaffolding
  'cross-scope': 'rgba(160,170,190,0.20)',   // light grey — structural
  'parent_of':   'rgba(160,170,190,0.18)',   // light grey — hierarchy
  'depends_on':  'rgba(251,146,60,0.70)',    // orange — critical path / blocking
  'blocks':      'rgba(239,68,68,0.70)',     // red — impediment
  'implements':  'rgba(52,211,153,0.65)',     // green — realization / delivery
  'satisfies':   'rgba(52,211,153,0.50)',     // green (lighter) — conformance
  'justifies':   'rgba(139,92,246,0.60)',     // purple — rationale / reasoning
  'cites':       'rgba(139,92,246,0.45)',     // purple (lighter) — provenance
  'documents':   'rgba(59,130,246,0.50)',     // blue — reference / documentation
  'relates_to':  'rgba(59,130,246,0.30)',     // blue (lighter) — generic association
  'mentions':    'rgba(59,130,246,0.20)',     // blue (faint) — weak reference
  'elaborates':  'rgba(236,72,153,0.55)',     // pink — expansion / detail
  'contradicts': 'rgba(239,68,68,0.55)',      // red — conflict
  'synthesises': 'rgba(14,165,233,0.55)',     // cyan — synthesis
  'remembers':   'rgba(245,158,11,0.45)',     // amber — memory / recall
};
const DEFAULT_LINK_COLOR = 'rgba(120,130,160,0.20)';

// ── Link width by relation ────────────────────────────────────────────────
const LINK_WIDTHS = {
  'parent_of':   LINK_WIDTH_SECONDARY,
  'documents':   LINK_WIDTH_SECONDARY,
  'mentions':    LINK_WIDTH_SECONDARY,
  'relates_to':  LINK_WIDTH_SECONDARY,
  'cross-scope': LINK_WIDTH_PRIMARY,
  'depends_on':  LINK_WIDTH_ACCENT,
  'implements':  LINK_WIDTH_ACCENT,
  'blocks':      LINK_WIDTH_ACCENT,
  'justifies':   LINK_WIDTH_PRIMARY,
  'satisfies':   LINK_WIDTH_PRIMARY,
  'cites':       LINK_WIDTH_PRIMARY,
};

// ── Interface ──────────────────────────────────────────────────────────────

export class BaseRenderer {
  /**
   * Pre-compute per-dataset statistics before apply() is called.
   * graph.js calls this once after fetching scope nodes.
   */
  init(_nodes) {}

  /** Apply node and link appearance to the ForceGraph3D builder. Returns builder. */
  apply(_graphBuilder) {
    throw new Error('apply() not implemented');
  }
}

// ── KindColorRenderer ──────────────────────────────────────────────────────

/**
 * Generates node colours from the Oklch palette (same algorithm as the page theme)
 * using window.culori if available, falling back to hardcoded hex.
 *
 * Features:
 * - Color by kind (perceptually distinct Oklch hues)
 * - Health override: amber (1-3 violations) or red (4+)
 * - Size by scope density: nodeVal = cbrt(val) so large scopes are clearly bigger
 * - Link width and colour by relation type
 */
// ── Label canvas ──────────────────────────────────────────────────────────

const CANVAS_W = 256;
const CANVAS_H = 56;

function makeLabelCanvas(name, count) {
  const canvas = document.createElement('canvas');
  canvas.width  = CANVAS_W;
  canvas.height = CANVAS_H;
  const ctx = canvas.getContext('2d');

  ctx.clearRect(0, 0, CANVAS_W, CANVAS_H);

  ctx.fillStyle = 'rgba(5,5,15,0.78)';
  const r = 8;
  ctx.beginPath();
  ctx.moveTo(r, 0); ctx.lineTo(CANVAS_W - r, 0);
  ctx.arcTo(CANVAS_W, 0, CANVAS_W, r, r);
  ctx.lineTo(CANVAS_W, CANVAS_H - r);
  ctx.arcTo(CANVAS_W, CANVAS_H, CANVAS_W - r, CANVAS_H, r);
  ctx.lineTo(r, CANVAS_H);
  ctx.arcTo(0, CANVAS_H, 0, CANVAS_H - r, r);
  ctx.lineTo(0, r);
  ctx.arcTo(0, 0, r, 0, r);
  ctx.closePath();
  ctx.fill();

  ctx.font = 'bold 18px system-ui,sans-serif';
  ctx.fillStyle = '#e2e8f0';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'top';
  ctx.fillText(name, CANVAS_W / 2, 6);

  ctx.font = '14px system-ui,sans-serif';
  ctx.fillStyle = '#94a3b8';
  ctx.fillText(`${count} artifacts`, CANVAS_W / 2, 30);

  return canvas;
}

// ── KindColorRenderer ──────────────────────────────────────────────────────

export class KindColorRenderer extends BaseRenderer {
  constructor() {
    super();
    this._palette  = null;
    this._bg       = '#05050f';
    this._minCbrt  = 1;
    this._maxCbrt  = 1;
    // Canvas texture cache: nodeId → { key, canvas }
    // key = `${name}|${val}|${violations}` — only recreate when data changes.
    this._canvasCache = new Map();
  }

  // Called by graph.js once the scope node array is available.
  init(nodes) {
    const vals = nodes.map(n => n.val || 1);
    this._minCbrt = Math.cbrt(Math.min(...vals));
    this._maxCbrt = Math.cbrt(Math.max(...vals));
  }

  _buildPalette(bg) {
    if (this._palette) return;
    const culori = typeof window !== 'undefined' && window.culori;
    if (culori) this._palette = buildPalette(culori, bg);
  }

  _kindColor(kind) {
    this._buildPalette(this._bg);
    return this._palette?.kinds?.[kind]?.hex ?? FALLBACK_KIND_COLORS[kind] ?? '#94a3b8';
  }

  _nodeColor(node) {
    if (node.kind === 'project') return '#c8d0dc';
    if (node.kind === 'kind-group') {
      this._buildPalette(this._bg);
      const culori = typeof window !== 'undefined' && window.culori;
      const baseKind = node.group?.split('.')?.pop() || node.name;
      if (culori) {
        const hue = this._palette?.kinds?.[baseKind]?.hue ?? 210;
        return culori.formatHex({ mode: 'oklch', l: 0.55, c: 0.12, h: hue });
      }
      return FALLBACK_KIND_COLORS[baseKind] || '#6b7280';
    }
    const hue = healthHue(node.violations);
    if (hue !== null) {
      this._buildPalette(this._bg);
      const culori = typeof window !== 'undefined' && window.culori;
      if (culori) return culori.formatHex({ mode: 'oklch', l: 0.75, c: 0.2, h: hue });
      return hue === HEALTH_HUES.warn ? '#f59e0b' : '#ef4444';
    }
    const artKind = node.kind?.split('.')?.pop() || node.kind;
    return this._kindColor(artKind);
  }

  // Smooth cube-root normalisation: [minVal, maxVal] → [minSize, maxSize]
  _nodeVal(val) {
    const MIN_SIZE = 2, MAX_SIZE = 40;
    const span = this._maxCbrt - this._minCbrt || 1;
    const t = (Math.cbrt(val || 1) - this._minCbrt) / span;
    return MIN_SIZE + (MAX_SIZE - MIN_SIZE) * Math.max(0, Math.min(1, t));
  }

  _labelSprite(node) {
    const THREE = typeof window !== 'undefined' && window.THREE;
    if (!THREE) return null;
    const cacheKey = `${node.name}|${node.val}|${node.violations}`;
    let canvas = this._canvasCache.get(node.id)?.key === cacheKey
      ? this._canvasCache.get(node.id).canvas
      : null;
    if (!canvas) {
      canvas = makeLabelCanvas(node.name, (node.val || 1) * ARTIFACT_COUNT_DIVISOR);
      this._canvasCache.set(node.id, { key: cacheKey, canvas });
    }
    const texture  = new THREE.CanvasTexture(canvas);
    const material = new THREE.SpriteMaterial({
      map: texture, transparent: true,
      depthTest: false,  // always renders in front
    });
    const sprite = new THREE.Sprite(material);
    sprite.scale.set(LABEL_SPRITE_SIZE, LABEL_SPRITE_SIZE * CANVAS_H / CANVAS_W, 1);
    // Position above node — offset by the node's rendered radius
    const radius = Math.cbrt(this._nodeVal(node.val)) * SPHERE_SCALE;
    sprite.position.set(0, radius + LABEL_SPRITE_SIZE * 0.3, 0);
    return sprite;
  }

  apply(g, bg) {
    if (bg) this._bg = bg;
    return g
      .nodeColor(n => this._nodeColor(n))
      .nodeRelSize(SPHERE_SCALE)
      .nodeVal(n => nodeVal(n))
      .nodeOpacity(NODE_OPACITY)
      .nodeThreeObject(n => this._labelSprite(n))
      .nodeThreeObjectExtend(true)
      .nodeResolution(SPHERE_SEGMENTS)
      .linkColor(l => LINK_COLORS[l.relation] || DEFAULT_LINK_COLOR)
      .linkOpacity(1)
      .linkWidth(l => LINK_WIDTHS[l.relation] ?? LINK_WIDTH_PRIMARY)
      .linkDirectionalArrowLength(l => DIRECTIONAL_RELATIONS.has(l.relation) ? ARROW_HEAD_SIZE : 0)
      .linkDirectionalArrowRelPos(1);
  }
}

const DIRECTIONAL_RELATIONS = new Set(['depends_on', 'implements', 'justifies', 'satisfies', 'blocks', 'cites']);

// ── Fallback palette (no culori) ───────────────────────────────────────────

export const FALLBACK_KIND_COLORS = {
  task:          '#3b82f6',
  spec:          '#8b5cf6',
  bug:           '#ef4444',
  goal:          '#f59e0b',
  campaign:      '#f97316',
  note:          '#10b981',
  concept:       '#06b6d4',
  source:        '#64748b',
  decision:      '#ec4899',
  need:          '#a78bfa',
  doc:           '#22d3ee',
  ref:           '#94a3b8',
  scope:         '#6dc6ff',
  'kind-group':  '#a5f3fc',
  sprint:        '#34d399',
};

// ── CSSVarRenderer — palette-driven appearance (next ablation candidate) ──

/**
 * Reads node colors from CSS custom properties injected by layout.html.
 * Falls back to FALLBACK_KIND_COLORS if the property is not set.
 *
 * @browser-only — depends on document.documentElement and getComputedStyle.
 */
export class CSSVarRenderer extends BaseRenderer {
  apply(g) {
    return g
      .nodeColor(n => {
        const v = getComputedStyle(document.documentElement)
          .getPropertyValue(`--graph-color-kind-${n.kind}`).trim();
        return v || FALLBACK_KIND_COLORS[n.kind] || '#94a3b8';
      })
      .nodeRelSize(6)
      .nodeVal(n => nodeVal(n))
      .nodeOpacity(0.95)
      .linkColor(l => LINK_COLORS[l.relation] || DEFAULT_LINK_COLOR)
      .linkOpacity(1)
      .linkWidth(l => LINK_WIDTHS[l.relation] ?? LINK_WIDTH_PRIMARY);
  }
}
