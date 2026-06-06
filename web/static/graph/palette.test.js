import { describe, it, expect } from 'vitest';
import * as culori from 'culori';
import {
  KIND_HUES, STATUS_HUES, DARK_THRESHOLD,
  hexLuminance, contrastRatio, readableOn, makeColor, buildPalette,
} from './palette.js';

// ── hexLuminance ─────────────────────────────────────────────────────────────

describe('hexLuminance', () => {
  it('black is 0', () => {
    expect(hexLuminance(culori, '#000000')).toBeCloseTo(0, 4);
  });
  it('white is 1', () => {
    expect(hexLuminance(culori, '#ffffff')).toBeCloseTo(1, 4);
  });
  it('graph canvas is very dark', () => {
    expect(hexLuminance(culori, '#05050f')).toBeLessThan(0.005);
  });
});

// ── contrastRatio ────────────────────────────────────────────────────────────

describe('contrastRatio', () => {
  it('black on white is 21:1', () => {
    expect(contrastRatio(culori, '#000000', '#ffffff')).toBeCloseTo(21, 0);
  });
  it('same colour is 1:1', () => {
    expect(contrastRatio(culori, '#3b82f6', '#3b82f6')).toBeCloseTo(1, 4);
  });
});

// ── DARK_THRESHOLD ───────────────────────────────────────────────────────────

describe('DARK_THRESHOLD', () => {
  it('graph canvas is below threshold (dark)', () => {
    expect(hexLuminance(culori, '#05050f')).toBeLessThan(DARK_THRESHOLD);
  });
  it('white is above threshold (light)', () => {
    expect(hexLuminance(culori, '#ffffff')).toBeGreaterThan(DARK_THRESHOLD);
  });
});

// ── makeColor ────────────────────────────────────────────────────────────────

describe('makeColor', () => {
  it('returns a valid hex string', () => {
    const { hex } = makeColor(culori, 220, '#05050f');
    expect(hex).toMatch(/^#[0-9a-f]{6}$/i);
  });

  it('dark bg → isDark=true → bright node (high luminance)', () => {
    const { hex, isDark } = makeColor(culori, 220, '#05050f');
    expect(isDark).toBe(true);
    expect(hexLuminance(culori, hex)).toBeGreaterThan(0.2);
  });

  it('light bg → isDark=false → darker node (lower luminance)', () => {
    const { hex, isDark } = makeColor(culori, 220, '#ffffff');
    expect(isDark).toBe(false);
    expect(hexLuminance(culori, hex)).toBeLessThan(0.4);
  });

  it('dark bg node passes WCAG 3:1 on graph canvas', () => {
    const { hex } = makeColor(culori, 220, '#05050f');
    expect(contrastRatio(culori, hex, '#05050f')).toBeGreaterThan(3);
  });

  it('light bg node passes WCAG 3:1 on white', () => {
    const { hex } = makeColor(culori, 220, '#ffffff');
    expect(contrastRatio(culori, hex, '#ffffff')).toBeGreaterThan(3);
  });
});

// ── All kind hues on all backgrounds ─────────────────────────────────────────

const backgrounds = [
  { name: 'graph canvas (#05050f)', hex: '#05050f' },
  { name: 'dark charcoal (#1a1a2e)', hex: '#1a1a2e' },
  { name: 'white (#ffffff)',         hex: '#ffffff' },
  { name: 'light gray (#f4f4f4)',    hex: '#f4f4f4' },
];

describe('WCAG 3:1 — all kinds on all backgrounds', () => {
  for (const bg of backgrounds) {
    for (const [kind, hue] of Object.entries(KIND_HUES)) {
      it(`${kind} on ${bg.name}`, () => {
        const { hex } = makeColor(culori, hue, bg.hex);
        expect(contrastRatio(culori, hex, bg.hex)).toBeGreaterThan(3);
      });
    }
  }
});

// ── Perceptual distance between kinds ────────────────────────────────────────

describe('perceptual distance — all 16 kinds are distinct', () => {
  function linearRGB(l, c, h) {
    const hr = h * Math.PI / 180;
    const a = c * Math.cos(hr), b = c * Math.sin(hr);
    const l_ = l + 0.3963377774*a + 0.2158037573*b;
    const m_ = l - 0.1055613458*a - 0.0638541728*b;
    const s_ = l - 0.0894841775*a - 1.2914855480*b;
    const lc = l_**3, mc = m_**3, sc = s_**3;
    return [
      Math.max(0, +4.0767416621*lc - 3.3077115913*mc + 0.2309699292*sc),
      Math.max(0, -1.2684380046*lc + 2.6097574011*mc - 0.3413193965*sc),
      Math.max(0, -0.0041960863*lc - 0.7034186147*mc + 1.7076147010*sc),
    ];
  }

  const entries = Object.entries(KIND_HUES);
  for (let i = 0; i < entries.length; i++) {
    for (let j = i + 1; j < entries.length; j++) {
      const [ka, ha] = entries[i];
      const [kb, hb] = entries[j];
      it(`${ka} vs ${kb} dist > 0.08`, () => {
        const [r1,g1,b1] = linearRGB(0.82, 0.18, ha);
        const [r2,g2,b2] = linearRGB(0.82, 0.18, hb);
        const d = Math.sqrt((r1-r2)**2 + (g1-g2)**2 + (b1-b2)**2);
        expect(d).toBeGreaterThan(0.08);
      });
    }
  }
});

// ── buildPalette ─────────────────────────────────────────────────────────────

describe('buildPalette', () => {
  it('returns all 16 kinds', () => {
    const { kinds } = buildPalette(culori, '#05050f');
    expect(Object.keys(kinds)).toHaveLength(Object.keys(KIND_HUES).length);
  });
  it('each kind has hex, tint, text', () => {
    const { kinds } = buildPalette(culori, '#05050f');
    for (const entry of Object.values(kinds)) {
      expect(entry.hex).toMatch(/^#[0-9a-f]{6}$/i);
      expect(entry.tint).toMatch(/^#[0-9a-f]{6}$/i);
      expect(entry.text).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });
});
