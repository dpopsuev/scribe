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

describe('label-node sync: same camera + position = same screen coords', () => {
  it('label position matches WebGL position for same world coords', () => {
    // Simulates the drift bug: if uploadNodes uses position P1 but
    // renderLabels reads position P2 (d3 ticked between them), labels drift.
    // Fix: both must read from same snapshot within one rAF frame.
    const cam: Camera = { x: 10, y: -5, zoom: 2.5, width: 1200, height: 900 };

    // Node at world position after simulation tick
    const worldX = 42, worldY = -17;

    // WebGL: shader uses buildViewMatrix → clip → viewport
    const m = buildViewMatrix(cam);
    const clipX = m[0] * worldX + m[3] * worldY + m[6];
    const clipY = m[1] * worldX + m[4] * worldY + m[7];
    const shaderScreenX = (clipX + 1) / 2 * cam.width;
    const shaderScreenY = (1 - clipY) / 2 * cam.height;

    // Label: uses worldToScreen
    const [labelX, labelY] = worldToScreen(cam, worldX, worldY);

    // Both must be identical for labels to be pinned to nodes
    expect(Math.abs(shaderScreenX - labelX)).toBeLessThan(0.01);
    expect(Math.abs(shaderScreenY - labelY)).toBeLessThan(0.01);
  });

  it('position change between upload and render causes drift', () => {
    // This test documents the bug: if d3-force ticks between uploadNodes
    // and renderLabels, the world positions differ.
    const cam: Camera = { x: 0, y: 0, zoom: 1, width: 800, height: 600 };

    const posAtUpload = { x: 100, y: 50 };
    const posAtRender = { x: 105, y: 48 }; // d3 ticked, moved 5px

    const [uploadScreenX] = worldToScreen(cam, posAtUpload.x, posAtUpload.y);
    const [renderScreenX] = worldToScreen(cam, posAtRender.x, posAtRender.y);

    const drift = Math.abs(uploadScreenX - renderScreenX);
    // 5 world units * zoom 1 = 5 CSS pixels of drift
    expect(drift).toBeCloseTo(5, 0);
    // This is the bug — drift > 0 means label is detached from node
    expect(drift).toBeGreaterThan(0);
  });

  it('same position at upload and render = zero drift', () => {
    const cam: Camera = { x: 0, y: 0, zoom: 1, width: 800, height: 600 };
    const pos = { x: 100, y: 50 };

    const [uploadScreenX, uploadScreenY] = worldToScreen(cam, pos.x, pos.y);
    const [renderScreenX, renderScreenY] = worldToScreen(cam, pos.x, pos.y);

    expect(uploadScreenX - renderScreenX).toBe(0);
    expect(uploadScreenY - renderScreenY).toBe(0);
  });
});

describe('position preservation across simulation rebuild', () => {
  it('settled position survives rebuild (not reset to layout)', () => {
    const layoutPos = { x: 0, y: 0 };
    const settledPos = { x: 42, y: -17 };

    const prevPositions = new Map([['node-1', settledPos]]);
    const result = prevPositions.get('node-1') ?? layoutPos;

    expect(result.x).toBe(42);
    expect(result.y).toBe(-17);
  });

  it('new node without previous position uses layout', () => {
    const layoutPos = { x: 10, y: 20 };

    const prevPositions = new Map<string, { x: number; y: number }>();
    const result = prevPositions.get('new-node') ?? layoutPos;

    expect(result.x).toBe(10);
    expect(result.y).toBe(20);
  });

  it('filtered-out node loses position, gets layout on return', () => {
    const layoutPos = { x: 5, y: 5 };
    const settledPos = { x: 100, y: -50 };

    // Node was in previous sim
    const prevWithNode = new Map([['node-1', settledPos]]);
    expect((prevWithNode.get('node-1') ?? layoutPos).x).toBe(100);

    // After filter removed node and it returns, prevPositions is empty
    const prevWithout = new Map<string, { x: number; y: number }>();
    expect((prevWithout.get('node-1') ?? layoutPos).x).toBe(5);
  });

  it('label screen position matches WebGL after rebuild with preserved position', () => {
    const cam: Camera = { x: 10, y: -5, zoom: 2, width: 800, height: 600 };
    const settledPos = { x: 42, y: -17 };

    const m = buildViewMatrix(cam);
    const clipX = m[0] * settledPos.x + m[6];
    const clipY = m[4] * settledPos.y + m[7];
    const shaderX = (clipX + 1) / 2 * cam.width;
    const shaderY = (1 - clipY) / 2 * cam.height;

    const [labelX, labelY] = worldToScreen(cam, settledPos.x, settledPos.y);

    expect(Math.abs(shaderX - labelX)).toBeLessThan(0.01);
    expect(Math.abs(shaderY - labelY)).toBeLessThan(0.01);
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
