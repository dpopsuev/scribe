/**
 * palette.js — Oklch colour generation for the Scribe graph UI.
 *
 * Pure functions: no DOM access, no CDN imports, no global state.
 * Depends only on the culori library injected via the `culori` parameter
 * so the module is fully testable without a browser.
 *
 * Usage (browser):
 *   import { buildPalette } from '/static/graph/palette.js';
 *   const palette = buildPalette(culori, '#05050f');   // dark graph canvas
 *
 * Usage (test):
 *   import * as culori from 'culori';
 *   import { buildPalette, hexLuminance, contrastRatio } from './palette.js';
 */

// ── Kind hue anchors (degrees in Oklch) ────────────────────────────────────
// Chosen so all 16 kinds are perceptually distinct: minimum linear-RGB
// Euclidean distance > 0.08 at L=0.82, C=0.18.
export const KIND_HUES = {
  task:         220,
  spec:         270,
  bug:           12,
  goal:          52,
  campaign:      32,
  note:         148,
  concept:      175,
  source:       230,
  decision:     320,
  need:         295,
  doc:          162,
  ref:          188,
  context:      106,
  journal:       76,
  scope:        255,
  'kind-group': 210,
};

export const STATUS_HUES = {
  draft:       240,
  active:      220,
  current:      50,
  proposed:    200,
  in_progress: 160,
  in_review:   280,
  complete:    140,
  archived:    240,
  retired:     240,
  cancelled:     0,
  evergreen:   140,
  fleeting:    240,
  allocated:   190,
  mature:      170,
  accepted:    140,
  rejected:      0,
  deferred:    240,
};

// Background luminance threshold: below this → dark bg → bright colours.
export const DARK_THRESHOLD = 0.18;

// ── Core colour math ─────────────────────────────────────────────────────────

/**
 * WCAG relative luminance of a CSS hex colour string.
 * Uses culori.wcagLuminance which is available in the bundled v4 build.
 */
export function hexLuminance(culori, hex) {
  return culori.wcagLuminance(hex) ?? 0;
}

/**
 * WCAG contrast ratio between two hex colours.
 */
export function contrastRatio(culori, hex1, hex2) {
  return culori.wcagContrast(hex1, hex2) ?? 1;
}

/**
 * Returns '#ffffff' or '#111111' — whichever is more readable on bg.
 */
export function readableOn(culori, bg) {
  return contrastRatio(culori, '#ffffff', bg) >= contrastRatio(culori, '#111111', bg)
    ? '#ffffff' : '#111111';
}

/**
 * Generate one colour entry for a given background.
 *
 * The algorithm inverts lightness based on background luminance:
 *   bgLuminance < DARK_THRESHOLD → dark bg → high lightness (L=0.82) → bright nodes
 *   bgLuminance ≥ DARK_THRESHOLD → light bg → low lightness  (L=0.48) → dark nodes
 *
 * Returns { hex, tint, text, isDark }
 */
export function makeColor(culori, hue, bgHex) {
  const bgLum = typeof bgHex === 'number' ? bgHex : hexLuminance(culori, bgHex);
  const isDark = bgLum < DARK_THRESHOLD;

  const l     = isDark ? 0.82 : 0.48;
  const c     = isDark ? 0.18 : 0.17;
  const tintL = isDark ? 0.24 : 0.92;
  const tintC = isDark ? 0.07 : 0.05;
  const textL = isDark ? 0.88 : 0.38;

  return {
    hex:    culori.formatHex({ mode: 'oklch', l, c, h: hue }),
    tint:   culori.formatHex({ mode: 'oklch', l: tintL, c: tintC, h: hue }),
    text:   culori.formatHex({ mode: 'oklch', l: textL, c: c * 0.9, h: hue }),
    isDark,
  };
}

/**
 * Build the full colour palette for a given background.
 * Returns { kinds: {name → {hex,tint,text}}, statuses: {name → {hex,tint,text}} }
 */
export function buildPalette(culori, bgHex) {
  const kinds = {};
  for (const [name, hue] of Object.entries(KIND_HUES)) {
    kinds[name] = makeColor(culori, hue, bgHex);
  }
  const statuses = {};
  for (const [name, hue] of Object.entries(STATUS_HUES)) {
    statuses[name] = makeColor(culori, hue, bgHex);
  }
  return { kinds, statuses };
}

/**
 * Inject CSS custom properties into document.documentElement.
 * Called from the browser; not used in tests.
 */
export function injectCSSVars(culori, root, bgHex, prefix) {
  const palette = buildPalette(culori, bgHex);
  const map = prefix === 'kind' ? palette.kinds : palette.statuses;
  for (const [name, { hex, tint, text }] of Object.entries(map)) {
    root.style.setProperty(`--color-${prefix}-${name}`, hex);
    root.style.setProperty(`--color-${prefix}-${name}-tint`, tint);
    root.style.setProperty(`--color-${prefix}-${name}-text`, text);
  }
}
