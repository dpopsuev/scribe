import { describe, it, expect } from 'vitest';
import { buildViewMatrix, worldToScreen } from './transform';
import type { Camera } from './transform';
import { fitBounds, startTransition, tickTransition } from './camera';

function makeNodes(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    x: Math.cos(i * 2.4) * 200,
    y: Math.sin(i * 2.4) * 200,
    _size: 4 + (i % 15),
  }));
}

const cam: Camera = { x: 10, y: -20, zoom: 3, width: 1920, height: 1080 };

describe('render hot-path benchmarks', () => {
  it('buildViewMatrix × 10000 under 15ms', () => {
    const t0 = performance.now();
    for (let i = 0; i < 10000; i++) buildViewMatrix(cam);
    const ms = performance.now() - t0;
    expect(ms).toBeLessThan(50);
  });

  it('worldToScreen × 10000 under 15ms', () => {
    const t0 = performance.now();
    for (let i = 0; i < 10000; i++) worldToScreen(cam, i * 0.1, i * 0.05);
    const ms = performance.now() - t0;
    expect(ms).toBeLessThan(50);
  });

  it('fitBounds 43 nodes under 0.1ms', () => {
    const nodes = makeNodes(43);
    const t0 = performance.now();
    for (let i = 0; i < 1000; i++) fitBounds(nodes, 1920, 1080);
    const msPerCall = (performance.now() - t0) / 1000;
    expect(msPerCall).toBeLessThan(0.1);
  });

  it('fitBounds 1000 nodes under 0.5ms', () => {
    const nodes = makeNodes(1000);
    const t0 = performance.now();
    for (let i = 0; i < 100; i++) fitBounds(nodes, 1920, 1080);
    const msPerCall = (performance.now() - t0) / 100;
    expect(msPerCall).toBeLessThan(0.5);
  });

  it('tickTransition × 10000 under 10ms', () => {
    const tr = startTransition({ x: 0, y: 0, zoom: 1 }, { x: 100, y: 50, zoom: 4 });
    const t0 = performance.now();
    for (let i = 0; i < 10000; i++) tickTransition(tr, tr.startTime + i * 0.08);
    const ms = performance.now() - t0;
    expect(ms).toBeLessThan(10);
  });

  it('full frame budget: 43-node render pipeline under 1ms', () => {
    const nodes = makeNodes(43);
    const t0 = performance.now();
    for (let i = 0; i < 1000; i++) {
      buildViewMatrix(cam);
      for (const n of nodes) worldToScreen(cam, n.x, n.y);
      fitBounds(nodes, 1920, 1080);
    }
    const msPerFrame = (performance.now() - t0) / 1000;
    expect(msPerFrame).toBeLessThan(1);
  });
});
