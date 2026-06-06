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

import { KIND_HUES, buildPalette } from './palette.js';

// ── Health color scale (violations → hue) ─────────────────────────────────
// 0 violations: kind color unchanged
// 1–3: amber — something needs attention
// 4+:  red   — broken, needs immediate fix
const HEALTH_HUES = { ok: null, warn: 45, error: 12 };

function healthHue(violations) {
  if (!violations || violations === 0) return HEALTH_HUES.ok;
  if (violations <= 3)                 return HEALTH_HUES.warn;
  return HEALTH_HUES.error;
}

// ── Link colours by relation ───────────────────────────────────────────────
export const LINK_COLORS = {
  'cross-scope': 'rgba(148,163,184,0.8)',
  'parent_of':   'rgba(148,163,184,0.5)',
  'depends_on':  'rgba(251,146,60,0.9)',
  'implements':  'rgba(52,211,153,0.9)',
  'justifies':   'rgba(167,139,250,0.9)',
  'satisfies':   'rgba(56,189,248,0.9)',
  'documents':   'rgba(148,163,184,0.6)',
};
const DEFAULT_LINK_COLOR = 'rgba(148,163,184,0.7)';

// ── Interface ──────────────────────────────────────────────────────────────

export class BaseRenderer {
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
export class KindColorRenderer extends BaseRenderer {
  constructor() {
    super();
    this._palette = null;
  }

  _buildPalette() {
    if (this._palette) return;
    const culori = typeof window !== 'undefined' && window.culori;
    if (culori) {
      this._palette = buildPalette(culori, '#05050f');
    }
  }

  _kindColor(kind) {
    this._buildPalette();
    return this._palette?.kinds?.[kind]?.hex
      ?? FALLBACK_KIND_COLORS[kind]
      ?? '#94a3b8';
  }

  _nodeColor(node) {
    const hue = healthHue(node.violations);
    if (hue !== null) {
      // Health override: build a single-hue colour in the same Oklch space.
      this._buildPalette();
      const culori = typeof window !== 'undefined' && window.culori;
      if (culori) {
        const { formatHex } = culori;
        return formatHex({ mode: 'oklch', l: 0.75, c: 0.2, h: hue });
      }
      return hue === HEALTH_HUES.warn ? '#f59e0b' : '#ef4444';
    }
    return this._kindColor(node.kind);
  }

  apply(g) {
    return g
      .nodeColor(n => this._nodeColor(n))
      .nodeRelSize(6)
      // cbrt gives a steeper size curve: small scopes stay small,
      // large scopes become clearly dominant without swallowing links.
      .nodeVal(n => Math.max(1, Math.cbrt(n.val || 1) * 2))
      .nodeOpacity(0.95)
      .linkColor(l => LINK_COLORS[l.relation] || DEFAULT_LINK_COLOR)
      .linkOpacity(1)
      .linkWidth(l => l.relation === 'parent_of' ? 1 : 2)
      .linkDirectionalArrowLength(l => l.relation === 'depends_on' ? 4 : 0)
      .linkDirectionalArrowRelPos(1);
  }
}

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
      .nodeVal(n => Math.max(1, Math.cbrt(n.val || 1) * 2))
      .nodeOpacity(0.95)
      .linkColor(l => LINK_COLORS[l.relation] || DEFAULT_LINK_COLOR)
      .linkOpacity(1)
      .linkWidth(l => l.relation === 'parent_of' ? 1 : 2);
  }
}
