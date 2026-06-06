/**
 * renderer.js — Plug-and-play node appearance for ForceGraph3D.
 *
 * Single Responsibility: each renderer configures ONLY how nodes look.
 * Open/Closed: new renderers extend BaseRenderer without touching graph.js.
 * Liskov: any renderer can replace any other — same interface, same data.
 * Interface Segregation: one method — apply(graphBuilder) → graphBuilder.
 * Dependency Inversion: graph.js depends on the BaseRenderer interface,
 *   not on concrete color logic.
 *
 * graph.js owns: data fetching, physics, camera, links, lifecycle.
 * Renderer owns: nodeColor, nodeVal, nodeRelSize, nodeOpacity — nothing else.
 *
 * Usage:
 *   import { KindColorRenderer } from './renderer.js';
 *   const renderer = new KindColorRenderer();
 *   renderer.apply(graphBuilder);  // returns graphBuilder for chaining
 */

// Hardcoded hex strings that work without culori or CSS custom properties.
// Source of truth for KindColorRenderer. CSSVarRenderer reads from CSS vars
// injected by layout.html's palette script and falls back to this map.

export const KIND_COLORS = {
  task:      '#3b82f6',  // blue-500
  spec:      '#8b5cf6',  // violet-500
  bug:       '#ef4444',  // red-500
  goal:      '#f59e0b',  // amber-500
  campaign:  '#f97316',  // orange-500
  note:      '#10b981',  // emerald-500
  concept:   '#06b6d4',  // cyan-500
  source:    '#64748b',  // slate-500
  decision:  '#ec4899',  // pink-500
  need:      '#a78bfa',  // violet-400
  doc:       '#22d3ee',  // cyan-400
  ref:       '#94a3b8',  // slate-400
  scope:     '#6dc6ff',  // sky accent
  sprint:    '#34d399',  // emerald-400
};

export const DEFAULT_NODE_COLOR = '#94a3b8';


export class BaseRenderer {
  /** Apply node appearance to the ForceGraph3D builder. Returns builder. */
  apply(_graphBuilder) {
    throw new Error('apply() not implemented');
  }
}


/**
 * Reproduces the v1 node appearance exactly.
 * Hardcoded KIND_COLORS map, sqrt(val)*1.5 sizing, opacity 0.9.
 * Does not depend on CSS custom properties or external THREE.
 */
export class KindColorRenderer extends BaseRenderer {
  apply(g) {
    return g
      .nodeColor(n => KIND_COLORS[n.kind] || DEFAULT_NODE_COLOR)
      .nodeRelSize(6)
      .nodeVal(n => Math.max(1, Math.sqrt(n.val || 1)))
      .nodeOpacity(0.9);
  }
}


/**
 * Reads node colors from CSS custom properties injected by layout.html.
 * Falls back to KIND_COLORS if the property is not set.
 *
 * @browser-only — depends on document.documentElement and getComputedStyle.
 *   Do not use in Node.js or unit-test environments.
 */
export class CSSVarRenderer extends BaseRenderer {
  apply(g) {
    return g
      .nodeColor(n => {
        const v = getComputedStyle(document.documentElement)
          .getPropertyValue(`--graph-color-kind-${n.kind}`).trim();
        return v || KIND_COLORS[n.kind] || DEFAULT_NODE_COLOR;
      })
      .nodeRelSize(8)
      .nodeVal(n => Math.sqrt(n.val || 1) * 1.5)
      .nodeOpacity(0.9);
  }
}
