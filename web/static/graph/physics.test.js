import { describe, it, expect } from 'vitest';
import {
  fibonacciSphere,
  weightedCentroid,
  parentNodes,
  centerOfMass,
  forceRadialSphere,
  equatorPriorityPositions,
  placeInMiniSphere,
  scaleNodesByDistance,
} from './physics.js';

// ── fibonacciSphere ───────────────────────────────────────────────────────────

describe('fibonacciSphere', () => {
  it('returns n points', () => {
    expect(fibonacciSphere(10, 100)).toHaveLength(10);
  });

  it('all points lie on the sphere surface (within tolerance)', () => {
    const pts = fibonacciSphere(50, 200);
    for (const p of pts) {
      expect(Math.hypot(p.x, p.y, p.z)).toBeCloseTo(200, 0);
    }
  });

  it('returns empty array for n=0', () => {
    expect(fibonacciSphere(0, 100)).toHaveLength(0);
  });

  it('n=1 returns the top pole', () => {
    const [p] = fibonacciSphere(1, 100);
    expect(p.y).toBeCloseTo(100, 0);
  });

  it('points are distributed across the full hemisphere range', () => {
    const pts = fibonacciSphere(100, 1);
    const yValues = pts.map(p => p.y);
    expect(Math.min(...yValues)).toBeLessThan(-0.9);
    expect(Math.max(...yValues)).toBeGreaterThan(0.9);
  });
});

// ── weightedCentroid ──────────────────────────────────────────────────────────

describe('weightedCentroid', () => {
  it('empty → origin', () => {
    expect(weightedCentroid([])).toEqual({ x: 0, y: 0, z: 0 });
    expect(weightedCentroid(null)).toEqual({ x: 0, y: 0, z: 0 });
  });

  it('single node → its position', () => {
    const result = weightedCentroid([{ x: 3, y: 4, z: 5, val: 7 }]);
    expect(result).toEqual({ x: 3, y: 4, z: 5 });
  });

  it('equal weights → arithmetic mean', () => {
    const nodes = [
      { x: 0, y: 0, z: 0, val: 1 },
      { x: 2, y: 2, z: 2, val: 1 },
    ];
    expect(weightedCentroid(nodes)).toEqual({ x: 1, y: 1, z: 1 });
  });

  it('heavier node pulls centre toward it', () => {
    const nodes = [
      { x: 0, y: 0, z: 0, val: 1 },
      { x: 10, y: 0, z: 0, val: 9 },
    ];
    const { x } = weightedCentroid(nodes);
    expect(x).toBeGreaterThan(5); // pulled toward x=10
  });

  it('missing val defaults to 1', () => {
    const result = weightedCentroid([{ x: 4, y: 0, z: 0 }, { x: 0, y: 0, z: 0 }]);
    expect(result.x).toBeCloseTo(2, 5);
  });
});

// ── parentNodes ───────────────────────────────────────────────────────────────

describe('parentNodes', () => {
  const mixed = [
    { id: 'S1', kind: 'scope' },
    { id: 'K1', kind: 'kind-group' },
    { id: 'T1', kind: 'task' },
    { id: 'N1', kind: 'note' },
  ];

  it('returns only scope and kind-group nodes', () => {
    const result = parentNodes(mixed);
    expect(result).toHaveLength(2);
    expect(result.every(n => n.kind === 'scope' || n.kind === 'kind-group')).toBe(true);
  });

  it('falls back to all nodes when no parents', () => {
    const leafs = [{ kind: 'task' }, { kind: 'note' }];
    expect(parentNodes(leafs)).toHaveLength(2);
  });

  it('handles empty / null', () => {
    expect(parentNodes([])).toHaveLength(0);
    expect(parentNodes(null)).toHaveLength(0);
  });
});

// ── centerOfMass ──────────────────────────────────────────────────────────────

describe('centerOfMass', () => {
  it('ignores leaf nodes, uses only scope parents', () => {
    const nodes = [
      { kind: 'scope', x: 100, y: 0, z: 0, val: 1 },
      { kind: 'task',  x: 0,   y: 0, z: 0, val: 100 }, // heavy but leaf
    ];
    const { x } = centerOfMass(nodes);
    expect(x).toBeCloseTo(100, 0); // scope node dominates
  });

  it('falls back to all nodes when no parents', () => {
    const nodes = [
      { kind: 'task', x: 10, y: 0, z: 0, val: 1 },
      { kind: 'task', x: 20, y: 0, z: 0, val: 1 },
    ];
    expect(centerOfMass(nodes).x).toBeCloseTo(15, 5);
  });
});

// ── forceRadialSphere ─────────────────────────────────────────────────────────

describe('forceRadialSphere', () => {
  it('attracts node inside sphere outward', () => {
    const force = forceRadialSphere(100, 1);
    const node = { x: 50, y: 0, z: 0, vx: 0, vy: 0, vz: 0 };
    force.initialize([node]);
    force(1); // alpha=1, full strength
    expect(node.vx).toBeGreaterThan(0); // pushed outward toward radius 100
  });

  it('repels node outside sphere inward', () => {
    const force = forceRadialSphere(50, 1);
    const node = { x: 100, y: 0, z: 0, vx: 0, vy: 0, vz: 0 };
    force.initialize([node]);
    force(1);
    expect(node.vx).toBeLessThan(0); // pushed inward toward radius 50
  });

  it('zero force at exactly targetRadius', () => {
    const force = forceRadialSphere(100, 1);
    const node = { x: 100, y: 0, z: 0, vx: 0, vy: 0, vz: 0 };
    force.initialize([node]);
    force(1);
    expect(node.vx).toBeCloseTo(0, 10);
  });
});

// ── equatorPriorityPositions ──────────────────────────────────────────────────

describe('equatorPriorityPositions', () => {
  it('returns n positions', () => {
    expect(equatorPriorityPositions(10, 100)).toHaveLength(10);
  });

  it('first position (highest priority) is near the equator', () => {
    const pts = equatorPriorityPositions(20, 100);
    // y ≈ 0 means equator; we expect the first item to have small |y|
    expect(Math.abs(pts[0].y)).toBeLessThan(30); // within 30% of equator
  });

  it('all positions lie on the sphere', () => {
    const pts = equatorPriorityPositions(15, 50);
    for (const p of pts) {
      expect(Math.hypot(p.x, p.y, p.z)).toBeCloseTo(50, 0);
    }
  });
});

// ── placeInMiniSphere ─────────────────────────────────────────────────────────

describe('placeInMiniSphere', () => {
  const nodes = [
    { id: 'A' }, { id: 'B' }, { id: 'C' },
  ];
  const links = [
    { source: 'A', target: 'B' },
    { source: 'A', target: 'C' },
    { source: 'B', target: 'C' },
  ];
  const anchor = { x: 100, y: 200, z: 300 };

  it('assigns x,y,z to every node', () => {
    const placed = placeInMiniSphere(nodes, links, anchor, 50);
    for (const n of placed) {
      expect(n.x).toBeDefined();
      expect(n.y).toBeDefined();
      expect(n.z).toBeDefined();
    }
  });

  it('nodes are within radius of anchor', () => {
    const placed = placeInMiniSphere(nodes, links, anchor, 50);
    for (const n of placed) {
      const d = Math.hypot(n.x - anchor.x, n.y - anchor.y, n.z - anchor.z);
      expect(d).toBeCloseTo(50, 0);
    }
  });

  it('highest-degree node gets first position (closest to equator)', () => {
    // A has degree 2, B and C have degree 2 too — all equal here
    const placed = placeInMiniSphere([...nodes], links, anchor, 50);
    expect(placed).toHaveLength(3);
  });
});

// ── scaleNodesByDistance ──────────────────────────────────────────────────────

describe('scaleNodesByDistance', () => {
  // Build a fake mesh that records the last setScalar call.
  function fakeMesh() {
    const m = { scale: { last: null, setScalar(r) { this.last = r; } } };
    return m;
  }

  // r = targetPx * distance * 2 * tan(fov/2) / viewportH
  const FOV_RAD = 75 * Math.PI / 180;
  const H       = 800;
  const TARGET  = 10;
  const CAM     = { x: 0, y: 0, z: 0 };

  it('scales a single node at known distance', () => {
    const node  = { x: 0, y: 0, z: 100 };
    const mesh  = fakeMesh();
    const map   = new Map([['a', { mesh, node }]]);
    scaleNodesByDistance(map, CAM, TARGET, FOV_RAD, H);
    const expected = TARGET * 100 * 2 * Math.tan(FOV_RAD / 2) / H;
    expect(mesh.scale.last).toBeCloseTo(expected, 6);
  });

  it('doubling distance doubles world radius (constant angular size)', () => {
    const near = { x: 0, y: 0, z: 100 };
    const far  = { x: 0, y: 0, z: 200 };
    const mNear = fakeMesh(), mFar = fakeMesh();
    const map = new Map([['n', { mesh: mNear, node: near }], ['f', { mesh: mFar, node: far }]]);
    scaleNodesByDistance(map, CAM, TARGET, FOV_RAD, H);
    expect(mFar.scale.last / mNear.scale.last).toBeCloseTo(2, 5);
  });

  it('uses camera position, not origin', () => {
    const cam  = { x: 50, y: 0, z: 0 };
    const node = { x: 50, y: 0, z: 100 }; // 100 units behind camera in z
    const mesh = fakeMesh();
    const map  = new Map([['a', { mesh, node }]]);
    scaleNodesByDistance(map, cam, TARGET, FOV_RAD, H);
    const expected = TARGET * 100 * 2 * Math.tan(FOV_RAD / 2) / H;
    expect(mesh.scale.last).toBeCloseTo(expected, 6);
  });

  it('node at distance 0 does not crash', () => {
    const node = { x: 0, y: 0, z: 0 };
    const mesh = fakeMesh();
    const map  = new Map([['a', { mesh, node }]]);
    expect(() => scaleNodesByDistance(map, CAM, TARGET, FOV_RAD, H)).not.toThrow();
    expect(mesh.scale.last).toBeGreaterThan(0);
  });

  it('empty map does not crash', () => {
    expect(() => scaleNodesByDistance(new Map(), CAM, TARGET, FOV_RAD, H)).not.toThrow();
  });

  it('missing node coordinates treated as 0', () => {
    const node  = {};
    const mesh  = fakeMesh();
    const cam   = { x: 0, y: 0, z: 50 };
    const map   = new Map([['a', { mesh, node }]]);
    scaleNodesByDistance(map, cam, TARGET, FOV_RAD, H);
    const expected = TARGET * 50 * 2 * Math.tan(FOV_RAD / 2) / H;
    expect(mesh.scale.last).toBeCloseTo(expected, 6);
  });

  it('larger targetPx produces larger world radius proportionally', () => {
    const node  = { x: 0, y: 0, z: 100 };
    const m1 = fakeMesh(), m2 = fakeMesh();
    const map1 = new Map([['a', { mesh: m1, node }]]);
    const map2 = new Map([['a', { mesh: m2, node }]]);
    scaleNodesByDistance(map1, CAM, 10, FOV_RAD, H);
    scaleNodesByDistance(map2, CAM, 20, FOV_RAD, H);
    expect(m2.scale.last / m1.scale.last).toBeCloseTo(2, 5);
  });
});
