import { describe, it, expect } from 'vitest';
import * as culori from 'culori';
import {
  glowHue, glowConfig, glowColor, glowOpacity, violationCountFromNode,
} from './glow.js';

// ── glowHue ───────────────────────────────────────────────────────────────────

describe('glowHue', () => {
  it('0 violations → green (140°)', () => expect(glowHue(0)).toBe(140));
  it('1 violation  → yellow (65°)', () => expect(glowHue(1)).toBe(65));
  it('2 violations → orange (25°)', () => expect(glowHue(2)).toBe(25));
  it('3 violations → red (5°)',     () => expect(glowHue(3)).toBe(5));
  it('10 violations → red (capped)', () => expect(glowHue(10)).toBe(5));

  it('hue decreases as violations increase (green→red)', () => {
    expect(glowHue(0)).toBeGreaterThan(glowHue(1));
    expect(glowHue(1)).toBeGreaterThan(glowHue(2));
    expect(glowHue(2)).toBeGreaterThan(glowHue(3));
  });
});

// ── glowConfig ────────────────────────────────────────────────────────────────

describe('glowConfig', () => {
  it('0 violations → lowest frequency and amplitude', () => {
    const c0 = glowConfig(0);
    const c3 = glowConfig(3);
    expect(c0.freq).toBeLessThan(c3.freq);
    expect(c0.innerAmp).toBeLessThan(c3.innerAmp);
  });

  it('all configs have freq, innerAmp, outerAmp', () => {
    for (let v = 0; v <= 4; v++) {
      const c = glowConfig(v);
      expect(c).toHaveProperty('freq');
      expect(c).toHaveProperty('innerAmp');
      expect(c).toHaveProperty('outerAmp');
    }
  });

  it('outerAmp is always less than innerAmp (softer falloff)', () => {
    for (let v = 0; v <= 3; v++) {
      const c = glowConfig(v);
      expect(c.outerAmp).toBeLessThan(c.innerAmp);
    }
  });
});

// ── glowColor ─────────────────────────────────────────────────────────────────

describe('glowColor', () => {
  it('scope nodes return null (no glow)', () => {
    expect(glowColor(culori, 'scope', 0)).toBeNull();
    expect(glowColor(culori, 'scope', 3)).toBeNull();
  });

  it('kind-group nodes return null (no glow)', () => {
    expect(glowColor(culori, 'kind-group', 2)).toBeNull();
  });

  it('artifact nodes return a hex string', () => {
    const color = glowColor(culori, 'task', 0);
    expect(color).toMatch(/^#[0-9a-f]{6}$/i);
  });

  it('green (0 violations) is actually green-ish', () => {
    const hex = glowColor(culori, 'task', 0);
    const rgb = culori.parse(hex);
    // Green: g > r, g > b
    expect(rgb.g).toBeGreaterThan(rgb.r);
    expect(rgb.g).toBeGreaterThan(rgb.b);
  });

  it('red (3+ violations) is actually red-ish', () => {
    const hex = glowColor(culori, 'task', 3);
    const rgb = culori.parse(hex);
    // Red: r > g, r > b
    expect(rgb.r).toBeGreaterThan(rgb.g);
    expect(rgb.r).toBeGreaterThan(rgb.b);
  });

  it('colour shifts from green toward red as violations increase', () => {
    const colors = [0, 1, 2, 3].map(v => culori.parse(glowColor(culori, 'task', v)));
    // Red channel should increase as violations increase
    expect(colors[3].r).toBeGreaterThan(colors[0].r);
    // Green channel should decrease
    expect(colors[0].g).toBeGreaterThan(colors[3].g);
  });
});

// ── glowOpacity ───────────────────────────────────────────────────────────────

describe('glowOpacity', () => {
  it('returns inner and outer opacity', () => {
    const cfg = glowConfig(1);
    const { inner, outer } = glowOpacity(cfg, 0);
    expect(typeof inner).toBe('number');
    expect(typeof outer).toBe('number');
  });

  it('opacity is in [0, maxAmp]', () => {
    const cfg = glowConfig(2);
    for (let t = 0; t < 5; t += 0.1) {
      const { inner, outer } = glowOpacity(cfg, t);
      expect(inner).toBeGreaterThanOrEqual(0);
      expect(inner).toBeLessThanOrEqual(cfg.innerAmp + 0.001);
      expect(outer).toBeGreaterThanOrEqual(0);
      expect(outer).toBeLessThanOrEqual(cfg.outerAmp + 0.001);
    }
  });

  it('outer is always softer than inner', () => {
    const cfg = glowConfig(3);
    for (let t = 0.1; t < 3; t += 0.3) {
      const { inner, outer } = glowOpacity(cfg, t);
      if (inner > 0) expect(outer).toBeLessThan(inner);
    }
  });

  it('oscillates — not constant', () => {
    const cfg = glowConfig(1);
    const opacities = Array.from({ length: 20 }, (_, i) => glowOpacity(cfg, i * 0.1).inner);
    const min = Math.min(...opacities);
    const max = Math.max(...opacities);
    expect(max - min).toBeGreaterThan(0.01);
  });
});

// ── violationCountFromNode ────────────────────────────────────────────────────

describe('violationCountFromNode', () => {
  it('uses node.violations if present', () => {
    expect(violationCountFromNode({ violations: 3 })).toBe(3);
    expect(violationCountFromNode({ violations: 0 })).toBe(0);
  });

  it('no labels → 0', () => {
    expect(violationCountFromNode({})).toBe(0);
  });

  it('compliance:ok label → 0', () => {
    expect(violationCountFromNode({ labels: ['compliance:ok'] })).toBe(0);
  });

  it('compliance:violation label, no extra → 1', () => {
    expect(violationCountFromNode({ labels: ['compliance:violation'] })).toBe(1);
  });

  it('compliance:violation with extra array → array length', () => {
    const node = {
      labels: ['compliance:violation'],
      extra: { compliance_violations: ['miss1', 'miss2', 'miss3'] },
    };
    expect(violationCountFromNode(node)).toBe(3);
  });

  it('compliance:violation with empty extra array → 1 (label authoritative)', () => {
    const node = {
      labels: ['compliance:violation'],
      extra: { compliance_violations: [] },
    };
    expect(violationCountFromNode(node)).toBe(1);
  });
});
