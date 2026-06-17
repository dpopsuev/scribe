import { describe, it, expect } from 'vitest';
import { buildViewMatrix, worldToScreen, verifyTransform } from './transform';
import type { Camera } from './transform';

describe('worldToScreen matches shader matrix', () => {
  const baseCam: Camera = { x: 0, y: 0, zoom: 1, width: 800, height: 600 };

  it('origin at default camera', () => {
    const r = verifyTransform(baseCam, 0, 0);
    expect(r.match).toBe(true);
    expect(r.shaderScreen[0]).toBeCloseTo(400);
    expect(r.shaderScreen[1]).toBeCloseTo(300);
  });

  it('positive world Y maps to upper screen', () => {
    const r = verifyTransform(baseCam, 0, 100);
    expect(r.match).toBe(true);
    expect(r.shaderScreen[1]).toBeLessThan(300);
  });

  it('after panning camera right', () => {
    const cam = { ...baseCam, x: 50 };
    const r = verifyTransform(cam, 50, 0);
    expect(r.match).toBe(true);
    expect(r.shaderScreen[0]).toBeCloseTo(400);
  });

  it('after panning camera up (THE BUG)', () => {
    const cam = { ...baseCam, y: 50 };
    const r = verifyTransform(cam, 0, 50);
    expect(r.match).toBe(true);
    expect(r.shaderScreen[1]).toBeCloseTo(300);
  });

  it('after zooming in 2x', () => {
    const cam = { ...baseCam, zoom: 2 };
    const r = verifyTransform(cam, 100, 0);
    expect(r.match).toBe(true);
    expect(r.shaderScreen[0]).toBeCloseTo(600);
  });

  it('combined pan + zoom', () => {
    const cam = { ...baseCam, x: 30, y: -20, zoom: 3 };
    const r = verifyTransform(cam, 50, 10);
    expect(r.match).toBe(true);
  });

  it('negative coordinates', () => {
    const cam = { ...baseCam, x: -100, y: -50, zoom: 0.5 };
    const r = verifyTransform(cam, -80, -30);
    expect(r.match).toBe(true);
  });

  it('at extreme zoom', () => {
    const cam = { ...baseCam, zoom: 20 };
    const r = verifyTransform(cam, 5, 5);
    expect(r.match).toBe(true);
  });
});

describe('regression: old buggy buildViewMatrix', () => {
  it('FAILS with ty = +camY * sy (the bug in GraphCanvas.svelte)', () => {
    const cam: Camera = { x: 0, y: 50, zoom: 1, width: 800, height: 600 };
    // The old buggy code: ty = camY * sy (positive)
    const sx = 2 * cam.zoom / cam.width;
    const sy = 2 * cam.zoom / cam.height;
    const buggyTy = cam.y * sy; // BUG: should be -cam.y * sy
    const buggyMatrix = new Float32Array([sx, 0, 0, 0, sy, 0, -cam.x * sx, buggyTy, 1]);

    // Compute shader screen position with buggy matrix
    const wx = 0, wy = 50;
    const clipX = buggyMatrix[0] * wx + buggyMatrix[6];
    const clipY = buggyMatrix[4] * wy + buggyMatrix[7];
    const shaderY = (1 - clipY) / 2 * cam.height;

    // Compute label screen position (correct formula)
    const [, labelY] = worldToScreen(cam, wx, wy);

    // These should match but DON'T with the buggy matrix
    expect(Math.abs(shaderY - labelY)).toBeGreaterThan(1);
  });

  it('PASSES with ty = -camY * sy (the fix)', () => {
    const cam: Camera = { x: 0, y: 50, zoom: 1, width: 800, height: 600 };
    const m = buildViewMatrix(cam);

    const wx = 0, wy = 50;
    const clipX = m[0] * wx + m[6];
    const clipY = m[4] * wy + m[7];
    const shaderY = (1 - clipY) / 2 * cam.height;
    const [, labelY] = worldToScreen(cam, wx, wy);

    expect(Math.abs(shaderY - labelY)).toBeLessThan(0.01);
  });
});
