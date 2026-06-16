/**
 * lens.js — Schema-driven lens definitions and Strategy Pattern resolver.
 *
 * A Lens maps artifact kinds to visual shelf layers. Different lenses
 * reorganize the same graph data into different layered arrangements
 * (effort tracking, code intelligence, AI context, etc.).
 *
 * The LensResolver selects the active lens via one of three strategies:
 *   auto   — count kind namespace distribution, pick dominant
 *   manual — user picks from a dropdown
 *   focus  — derives lens from a clicked node's namespace
 */

import { createLogger } from './logger.js';

const log = createLogger('lens');

// ── Hardcoded fallback registry ───────────────────────────────────────────
// Used when /api/v1/schema/hierarchy is unavailable. Mirrors the CRD
// parent_of hierarchy defined in parchment's relationship YAML files.
const FALLBACK_REGISTRY = {
  effort: {
    layers: [
      { kinds: ['effort.campaign'], label: 'Campaign' },
      { kinds: ['effort.goal'], label: 'Goal' },
      { kinds: ['effort.task'], label: 'Task' },
    ],
  },
  intent: {
    layers: [
      { kinds: ['intent.need'], label: 'Need' },
      { kinds: ['intent.spec'], label: 'Spec' },
      { kinds: ['intent.decision'], label: 'Decision' },
      { kinds: ['intent.bug'], label: 'Bug' },
    ],
  },
  knowledge: {
    layers: [
      { kinds: ['knowledge.source'], label: 'Source' },
      { kinds: ['knowledge.concept', 'knowledge.context'], label: 'Concept' },
      { kinds: ['knowledge.note', 'knowledge.journal'], label: 'Notes' },
    ],
  },
  ctx: {
    layers: [
      { kinds: ['ctx.session'], label: 'Session' },
      { kinds: ['ctx.turn'], label: 'Turn' },
      { kinds: ['ctx.tool-call'], label: 'Tool Call' },
    ],
  },
  investigation: {
    layers: [
      { kinds: ['investigation.case'], label: 'Case' },
      { kinds: ['investigation.observation', 'investigation.cause'], label: 'Evidence' },
    ],
  },
  code: {
    layers: [
      { kinds: ['code.interface'], label: 'Interface' },
      { kinds: ['code.test'], label: 'Test' },
    ],
  },
  support: {
    layers: [
      { kinds: ['support.doc', 'support.ref'], label: 'Documentation' },
      { kinds: ['support.config', 'support.template', 'support.rule'], label: 'Config' },
      { kinds: ['support.section', 'support.paragraph', 'support.mirror'], label: 'Structure' },
    ],
  },
};

/**
 * Fetch lens registry from the backend schema hierarchy endpoint.
 * Falls back to FALLBACK_REGISTRY on error.
 *
 * @param {Function} fetchFn — fetch implementation (injected for testability)
 * @returns {Promise<Map<string, {name: string, label: string, layers: Array, fallbackIndex: number}>>}
 */
export async function fetchLensRegistry(fetchFn) {
  const registry = new Map();

  let apiData = {};
  try {
    const res = await fetchFn('/api/v1/schema/hierarchy');
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const json = await res.json();
    apiData = json.namespaces || {};
  } catch (err) {
    log.warn('fetchLensRegistry API unavailable, using fallbacks only: %s', err.message);
  }

  // Merge: prefer API data when it has >1 layer (real hierarchy), else use fallback.
  const allNS = new Set([...Object.keys(apiData), ...Object.keys(FALLBACK_REGISTRY)]);
  for (const ns of allNS) {
    const apiNS = apiData[ns];
    const fbNS = FALLBACK_REGISTRY[ns];
    const source = (apiNS && apiNS.layers && apiNS.layers.length > 1) ? apiNS : fbNS;
    if (!source) continue;

    const layers = (source.layers || []).map(l => ({
      kinds: l.kinds || [],
      label: l.label || '',
    }));
    registry.set(ns, {
      name: ns,
      label: capitalize(ns),
      layers,
      fallbackIndex: layers.length,
    });
  }

  log.info('fetchLensRegistry loaded %d namespaces', registry.size);
  return registry;
}

/**
 * Find the shelf index for a given kind within a lens.
 *
 * @param {{layers: Array<{kinds: string[]}>}} lens
 * @param {string} kind — e.g. "effort.task"
 * @returns {number} — layer index, or lens.fallbackIndex if not found
 */
export function kindToLayer(lens, kind) {
  if (!lens || !lens.layers) return 0;
  // Structural super-nodes: always pin to top shelves
  if (kind === 'project') return 0;
  if (kind === 'kind-group') return 1;
  for (let i = 0; i < lens.layers.length; i++) {
    if (lens.layers[i].kinds.includes(kind)) return i + 2; // offset by 2 for project/kind-group
  }
  return (lens.fallbackIndex ?? lens.layers.length) + 2;
}

/**
 * Extract namespace prefix from a kind string.
 * "effort.task" → "effort", "project" → "project", "kind-group" → "kind-group"
 */
export function kindNamespace(kind) {
  if (!kind) return '';
  const dot = kind.indexOf('.');
  return dot > 0 ? kind.slice(0, dot) : kind;
}

// ── LensResolver — Strategy Pattern ──────────────────────────────────────

export class LensResolver {
  constructor() {
    this.mode = 'auto';       // 'auto' | 'manual' | 'focus'
    this.manualLens = null;   // Lens — set by dropdown
    this.focusKind = null;    // string — kind namespace from clicked node
  }

  /**
   * Resolve the active lens based on current strategy.
   *
   * @param {Array} nodes — current graph nodes
   * @param {Map} registry — Map<namespace, Lens>
   * @returns {{name, label, layers, fallbackIndex}|null}
   */
  resolve(nodes, registry) {
    switch (this.mode) {
      case 'manual':
        if (this.manualLens && registry.has(this.manualLens)) {
          return registry.get(this.manualLens);
        }
        return this._autoDetect(nodes, registry);

      case 'focus':
        if (this.focusKind && registry.has(this.focusKind)) {
          return registry.get(this.focusKind);
        }
        return this._autoDetect(nodes, registry);

      case 'auto':
      default:
        return this._autoDetect(nodes, registry);
    }
  }

  /**
   * Auto-detect: count kind namespace distribution in visible nodes,
   * pick the dominant namespace. Skip structural nodes (project, kind-group).
   */
  _autoDetect(nodes, registry) {
    const counts = {};
    for (const n of nodes) {
      if (n.kind === 'project' || n.kind === 'kind-group') continue;
      const ns = kindNamespace(n.kind);
      if (ns && registry.has(ns)) {
        counts[ns] = (counts[ns] || 0) + 1;
      }
    }

    let best = null, bestCount = 0;
    for (const [ns, count] of Object.entries(counts)) {
      if (count > bestCount) { best = ns; bestCount = count; }
    }

    if (best) {
      log.info('autoDetect lens=%s count=%d', best, bestCount);
      return registry.get(best);
    }

    // Fallback: return first available lens
    const first = registry.values().next();
    return first.done ? null : first.value;
  }

  setMode(mode) {
    if (['auto', 'manual', 'focus'].includes(mode)) {
      this.mode = mode;
      log.info('setMode mode=%s', mode);
    }
  }

  setManual(nsName) {
    this.manualLens = nsName;
  }

  setFocus(kind) {
    this.focusKind = kindNamespace(kind);
    log.info('setFocus ns=%s from kind=%s', this.focusKind, kind);
  }
}

/**
 * Build a combined "all namespaces" lens that stacks all namespace layers.
 * Useful when no single namespace dominates.
 */
export function buildCombinedLens(registry) {
  const layers = [];
  for (const [, lens] of registry) {
    for (const layer of lens.layers) {
      layers.push(layer);
    }
  }
  return {
    name: 'all',
    label: 'All',
    layers,
    fallbackIndex: layers.length,
  };
}

function capitalize(s) {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : '';
}
