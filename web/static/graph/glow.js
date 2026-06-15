/**
 * glow.js — Compliance glow system for graph nodes.
 *
 * Pure functions for colour and animation configuration.
 * Three.js mesh creation is handled by the browser layer (graph.js)
 * because Three.js cannot be imported in a test environment.
 *
 * The compliance gradient:
 *   violations=0  → Oklch H=140 (green),  weak slow pulse
 *   violations=1  → Oklch H=65  (yellow), visible
 *   violations=2  → Oklch H=25  (orange), strong
 *   violations=3+ → Oklch H=5   (red),    intense fast pulse
 */

// Hue stops keyed by violation count (capped at 3).
const GLOW_HUES = [140, 65, 25, 5];

// Pulse configuration per severity level.
const GLOW_CONFIGS = [
  { freq: 0.30, innerAmp: 0.18, outerAmp: 0.07 }, // green — slow, very weak
  { freq: 0.90, innerAmp: 0.42, outerAmp: 0.18 }, // yellow
  { freq: 1.40, innerAmp: 0.58, outerAmp: 0.25 }, // orange
  { freq: 2.00, innerAmp: 0.72, outerAmp: 0.32 }, // red — fast, intense
];

/**
 * Returns the Oklch hue for a given violation count.
 * 0 → green (140°), 1 → yellow (65°), 2 → orange (25°), 3+ → red (5°).
 */
export function glowHue(violations) {
  return GLOW_HUES[Math.min(violations, GLOW_HUES.length - 1)];
}

/**
 * Returns the pulse configuration for a given violation count.
 * { freq, innerAmp, outerAmp }
 */
export function glowConfig(violations) {
  return GLOW_CONFIGS[Math.min(violations, GLOW_CONFIGS.length - 1)];
}

/**
 * Returns the hex colour for a glow at a given violation count.
 * Requires a culori instance (injected to keep this module testable).
 * Returns null for scope/kind-group nodes (no compliance glow).
 *
 * @param {object} culori   — culori library reference
 * @param {string} nodeKind — node.kind value
 * @param {number} violations — violation count from node.violations
 */
export function glowColor(culori, nodeKind, violations) {
  if (nodeKind === 'project' || nodeKind === 'kind-group') return null;
  const h = glowHue(violations);
  return culori.formatHex({ mode: 'oklch', l: 0.80, c: 0.20, h });
}

/**
 * Compute glow opacity for inner and outer halos at a given time.
 * Uses a sine wave at the configured frequency.
 *
 * @param {object} config   — result of glowConfig(violations)
 * @param {number} timeSec  — performance.now() / 1000
 * @returns {{ inner: number, outer: number }} — opacity values in [0, 1]
 */
export function glowOpacity(config, timeSec) {
  const phase = Math.sin(timeSec * config.freq * Math.PI * 2) * 0.5 + 0.5; // [0,1]
  return {
    inner: config.innerAmp * phase,
    outer: config.outerAmp * phase * 0.6,
  };
}

/**
 * Returns the violation count from an artifact node's labels and extra.
 * Mirrors the Go violationCount() function in web/graph.go.
 *
 * node.violations is pre-computed by the API; this function is provided
 * for cases where raw label/extra data is available client-side.
 */
export function violationCountFromNode(node) {
  // Fast path: API already computed it
  if (typeof node.violations === 'number') return node.violations;

  // Fallback: inspect labels
  const labels = node.labels || [];
  const hasViolation = labels.some(l => l === 'compliance:violation');
  if (!hasViolation) return 0;

  // Try to count from extra
  const viols = node.extra?.compliance_violations;
  if (Array.isArray(viols)) return viols.length || 1;
  return 1;
}
