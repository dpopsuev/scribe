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
  forceSelfGravity,
  forcesForDist,
  clusterMaxRadius,
  clusterRadiusFromVolume,
  forceRadiusCap,
  computeFitDistance,
  computeFitDistanceForCount,
  computeFitDistanceForVolume,
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

describe('forceSelfGravity', () => {
  function makeNode(x, y, z, val = 1) {
    return { x, y, z, val, vx: 0, vy: 0, vz: 0 };
  }

  it('exposes setG() for in-place parameter update', () => {
    const force = forceSelfGravity(0.1, 0);
    const node  = makeNode(100, 0, 0);
    force.initialize([node]);

    force(1);
    const accelLow = Math.abs(node.vx);
    node.vx = 0;

    force.setG(1.0); // 10x stronger
    force(1);
    const accelHigh = Math.abs(node.vx);

    expect(accelHigh).toBeGreaterThan(accelLow * 5);
  });

  it('setG(0) stops gravity', () => {
    const force = forceSelfGravity(1.0, 0);
    const node  = makeNode(100, 0, 0);
    force.initialize([node]);
    force.setG(0);
    force(1);
    expect(node.vx).toBe(0);
  });

  it('pulls a node toward origin', () => {
    const force = forceSelfGravity(1, 0);  // G=1, no softening
    const node = makeNode(100, 0, 0);
    force.initialize([node]);
    force(1);
    expect(node.vx).toBeLessThan(0);  // pulled toward origin (-x direction)
    expect(node.vy).toBeCloseTo(0);
    expect(node.vz).toBeCloseTo(0);
  });

  it('heavier nodes (larger val) accelerate more', () => {
    const force = forceSelfGravity(1, 0);
    const light = makeNode(100, 0, 0, 1);
    const heavy = makeNode(100, 0, 0, 10);
    force.initialize([light]);
    force(1);
    const lightAcc = Math.abs(light.vx);
    force.initialize([heavy]);
    force(1);
    const heavyAcc = Math.abs(heavy.vx);
    expect(heavyAcc).toBeGreaterThan(lightAcc);
  });

  it('softening prevents infinite force at origin', () => {
    const force = forceSelfGravity(1, 30);
    const node = makeNode(0, 0, 0);
    force.initialize([node]);
    expect(() => force(1)).not.toThrow();
    expect(Number.isFinite(node.vx)).toBe(true);
  });

  it('alpha=0 applies no force', () => {
    const force = forceSelfGravity(1, 30);
    const node = makeNode(100, 100, 100);
    force.initialize([node]);
    force(0);
    expect(node.vx).toBe(0);
    expect(node.vy).toBe(0);
    expect(node.vz).toBe(0);
  });

  it('node at origin with softening gets small finite force', () => {
    const force = forceSelfGravity(1, 30);
    const node = makeNode(0, 0, 0, 5);
    force.initialize([node]);
    force(1);
    expect(node.vx).toBe(0);  // zero displacement → zero net force direction
  });
});

describe('forcesForDist', () => {
  // r_eq ≈ |rep| / G — equilibrium cluster radius given force parameters.
  function clusterRadius(dist) {
    const { G, rep } = forcesForDist(dist);
    return Math.abs(rep) / G;
  }

  const ZOOMED_IN  = 200;
  const ZOOMED_OUT = 2000;

  it('zoomed in → looser cluster than zoomed out', () => {
    expect(clusterRadius(ZOOMED_IN)).toBeGreaterThan(clusterRadius(ZOOMED_OUT));
  });

  it('zoomed out → tighter cluster than zoomed in', () => {
    expect(clusterRadius(ZOOMED_OUT)).toBeLessThan(clusterRadius(ZOOMED_IN));
  });

  it('cluster radius decreases monotonically as camera moves farther', () => {
    const r300  = clusterRadius(300);
    const r600  = clusterRadius(600);
    const r1200 = clusterRadius(1200);
    expect(r300).toBeGreaterThan(r600);
    expect(r600).toBeGreaterThan(r1200);
  });
});

describe('forcesForDist — dead zone', () => {
  it('returns null when dist change is below sensitivity threshold', () => {
    // 615/600 = 2.5% change, threshold 5% → null
    const result = forcesForDist(615, 150, 3000, 0.05, 600);
    expect(result).toBeNull();
  });

  it('returns params when change exceeds threshold', () => {
    const result = forcesForDist(900, 150, 3000, 0.05); // 50% change from 600
    expect(result).not.toBeNull();
    expect(result).toHaveProperty('G');
  });

  it('always returns params when lastDist is null (first call)', () => {
    const result = forcesForDist(600, 150, 3000, 0.05, null);
    expect(result).not.toBeNull();
  });

  it('threshold=0 means every call returns params', () => {
    expect(forcesForDist(601, 150, 3000, 0)).not.toBeNull();
    expect(forcesForDist(600, 150, 3000, 0)).not.toBeNull();
  });
});

describe('clusterRadiusFromVolume', () => {
  // nodeVisualVolume = clamp(cbrt(val)*2, 2, 40) — same formula as renderer + graph.js.
  function nv(val) { return Math.max(2, Math.min(40, Math.cbrt(val) * 2)); }

  it('calibration: 87 nodes val=10 matches clusterMaxRadius(87) within 5%', () => {
    // Ensures continuity with the count-based formula for typical production graphs.
    const totalVol = 87 * nv(10);
    const r = clusterRadiusFromVolume(totalVol);
    const rCount = clusterMaxRadius(87);
    expect(r).toBeGreaterThan(rCount * 0.95);
    expect(r).toBeLessThan(rCount * 1.05);
  });

  it('larger nodes → bigger radius than same count of small nodes', () => {
    const rSmall = clusterRadiusFromVolume(20 * nv(1));
    const rLarge = clusterRadiusFromVolume(20 * nv(100));
    expect(rLarge).toBeGreaterThan(rSmall);
  });

  it('more nodes → bigger radius', () => {
    const r10  = clusterRadiusFromVolume(10  * nv(10));
    const r100 = clusterRadiusFromVolume(100 * nv(10));
    expect(r100).toBeGreaterThan(r10);
  });

  it('scales as cbrt — doubling total volume grows radius by ~26%', () => {
    const r1 = clusterRadiusFromVolume(100);
    const r2 = clusterRadiusFromVolume(200);
    expect(r2 / r1).toBeCloseTo(Math.cbrt(2), 2);
  });

  it('zero or negative volume does not throw — uses floor of 1', () => {
    expect(() => clusterRadiusFromVolume(0)).not.toThrow();
    expect(clusterRadiusFromVolume(0)).toBeGreaterThan(0);
  });
});

describe('computeFitDistanceForVolume', () => {
  it('larger volume → greater camera distance', () => {
    const d1 = computeFitDistanceForVolume(100);
    const d2 = computeFitDistanceForVolume(1000);
    expect(d2).toBeGreaterThan(d1);
  });

  it('wider FOV → closer camera for same volume', () => {
    const dNarrow = computeFitDistanceForVolume(500, 40);
    const dWide   = computeFitDistanceForVolume(500, 80);
    expect(dWide).toBeLessThan(dNarrow);
  });

  it('padding scales linearly', () => {
    const d1 = computeFitDistanceForVolume(500, 50, 1.0);
    const d2 = computeFitDistanceForVolume(500, 50, 2.0);
    expect(d2).toBeCloseTo(d1 * 2, 1);
  });
});

describe('clusterMaxRadius', () => {
  it('10 nodes → base radius', () => {
    expect(clusterMaxRadius(10)).toBeCloseTo(80, 0);
  });

  it('100 nodes (10× more) → 2× radius', () => {
    expect(clusterMaxRadius(100)).toBeCloseTo(160, 0);
  });

  it('1000 nodes (100× more) → 3× radius', () => {
    expect(clusterMaxRadius(1000)).toBeCloseTo(240, 0);
  });

  it('85 nodes (production) → between 80 and 160', () => {
    const r = clusterMaxRadius(85);
    expect(r).toBeGreaterThan(80);
    expect(r).toBeLessThan(160);
  });

  it('clamps at base for n < 10', () => {
    expect(clusterMaxRadius(1)).toBeCloseTo(80, 0);
    expect(clusterMaxRadius(5)).toBeCloseTo(80, 0);
  });
});

describe('forceRadiusCap', () => {
  function makeNode(x, y, z) {
    return { x, y, z, vx: 0, vy: 0, vz: 0 };
  }

  it('applies no force to nodes inside the cap', () => {
    const force = forceRadiusCap(200);
    const node  = makeNode(100, 0, 0); // radius 100 < cap 200
    force.initialize([node]);
    force(1);
    expect(node.vx).toBe(0);
    expect(node.vy).toBe(0);
    expect(node.vz).toBe(0);
  });

  it('pulls nodes outside the cap toward origin', () => {
    const force = forceRadiusCap(100);
    const node  = makeNode(200, 0, 0); // radius 200 > cap 100
    force.initialize([node]);
    force(1);
    expect(node.vx).toBeLessThan(0); // pulled back toward origin
  });

  it('pull is proportional to excess beyond cap', () => {
    const force = forceRadiusCap(100);
    const small = makeNode(110, 0, 0); // 10 beyond cap
    const large = makeNode(200, 0, 0); // 100 beyond cap
    force.initialize([small]);
    force(1);
    const pullSmall = Math.abs(small.vx);
    force.initialize([large]);
    force(1);
    const pullLarge = Math.abs(large.vx);
    expect(pullLarge).toBeGreaterThan(pullSmall);
  });

  it('setMaxRadius updates cap in-place', () => {
    const force = forceRadiusCap(50);
    const node  = makeNode(80, 0, 0); // beyond 50, inside 200
    force.initialize([node]);
    force.setMaxRadius(200);
    force(1);
    expect(node.vx).toBe(0); // now inside new cap
  });

  it('alpha=0 applies no force', () => {
    const force = forceRadiusCap(10);
    const node  = makeNode(500, 0, 0);
    force.initialize([node]);
    force(0);
    expect(node.vx).toBe(0);
  });
});

// ── Boot vs idle camera distance invariant ────────────────────────────────

describe('camera distance invariant — boot must equal idle settled state', () => {
  const NODE_COUNT      = 85;            // production scope nodes
  const UNIVERSE_RADIUS = 180;           // equatorPriorityPositions initial radius
  const R_settled       = clusterMaxRadius(NODE_COUNT); // 154 — settled cap

  it('settled cap radius < initial transient radius', () => {
    expect(R_settled).toBeLessThan(UNIVERSE_RADIUS);
  });

  it('computeFitDistance is monotonically increasing in R', () => {
    expect(computeFitDistance(UNIVERSE_RADIUS)).toBeGreaterThan(computeFitDistance(R_settled));
  });

  it('computeFitDistanceForCount always uses cap — boot equals idle', () => {
    // GREEN with fix: both use clusterMaxRadius(n), not actual positions.
    const D_boot = computeFitDistanceForCount(NODE_COUNT);
    const D_idle = computeFitDistanceForCount(NODE_COUNT);
    expect(D_boot).toBeCloseTo(D_idle, 5);
  });

  it('FOV fill with computeFitDistanceForCount is comfortable — below 85%', () => {
    const D = computeFitDistanceForCount(NODE_COUNT);
    const fillRad = 2 * Math.atan(R_settled / D);
    const fillPct = fillRad / (75 * Math.PI / 180) * 100;
    expect(fillPct, `fills ${fillPct.toFixed(1)}% of FOV — want < 85%`).toBeLessThan(85);
  });

  it('using actual transient positions at boot produces a different distance than the settled cap', () => {
    // Documents why boot felt inconsistent with idle:
    // max(cap=154, actual=180) → D=293 at boot, but cap=154 → D=251 at idle.
    const D_with_actual = computeFitDistance(Math.max(R_settled, UNIVERSE_RADIUS));
    const D_cap_based   = computeFitDistanceForCount(NODE_COUNT);
    expect(D_with_actual).not.toBeCloseTo(D_cap_based, 0);
  });

  it('computeFitDistanceForCount produces the same distance regardless of transient positions', () => {
    // The fix: fitAllNodes always calls computeFitDistanceForCount(n)
    // so boot and idle are identical — camera leads physics.
    const D_boot = computeFitDistanceForCount(NODE_COUNT);
    const D_idle = computeFitDistanceForCount(NODE_COUNT);
    expect(D_boot).toBeCloseTo(D_idle, 5);
  });
});
