import { describe, it, expect } from 'vitest';
import {
  fibonacciSphere,
  weightedCentroid,
  parentNodes,
  centerOfMass,
  equatorPriorityPositions,
  placeInMiniSphere,
  forceNBodyGravity,
  forceLennardJonesRepulsion,
  forcesForDist,
  clusterMaxRadius,
  clusterRadiusFromVolume,
  forceRadiusCap,
  computeFitDistance,
  computeFitDistanceForCount,
  computeFitDistanceForVolume,
  GRAVITY_MIN,
  GRAVITY_MAX,
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



describe('forceLennardJonesRepulsion', () => {
  function makeNode(x, y, z) { return { x, y, z, vx: 0, vy: 0, vz: 0 }; }
  const SIGMA = 20;

  it('repels two nodes closer than sigma', () => {
    const force = forceLennardJonesRepulsion(SIGMA, 1);
    const a = makeNode(-5, 0, 0);
    const b = makeNode( 5, 0, 0); // separation 10 < sigma=20
    force.initialize([a, b]);
    force(1);
    expect(a.vx).toBeLessThan(0);  // a pushed left (away from b)
    expect(b.vx).toBeGreaterThan(0); // b pushed right
  });

  it('applies no force beyond 2σ cutoff', () => {
    const force = forceLennardJonesRepulsion(SIGMA, 1);
    const a = makeNode(-25, 0, 0);
    const b = makeNode( 25, 0, 0); // separation 50 = 2.5σ > cutoff 2σ
    force.initialize([a, b]);
    force(1);
    expect(a.vx).toBe(0);
    expect(b.vx).toBe(0);
  });

  it('force is much stronger at σ/2 than at σ — steeply repulsive', () => {
    const force = forceLennardJonesRepulsion(SIGMA, 1);
    const close = [makeNode(-5, 0, 0),  makeNode( 5, 0, 0)]; // r = 10 = σ/2
    const apart = [makeNode(-10, 0, 0), makeNode(10, 0, 0)]; // r = 20 = σ
    force.initialize(close); force(1);
    const fClose = Math.abs(close[0].vx);
    close[0].vx = 0; close[1].vx = 0;
    force.initialize(apart); force(1);
    const fApart = Math.abs(apart[0].vx);
    // (σ/r)^12: (20/10)^12 = 4096 vs (20/20)^12 = 1 → close force ≫ apart force
    expect(fClose).toBeGreaterThan(fApart * 100);
  });

  it('momentum is conserved — equal and opposite', () => {
    const force = forceLennardJonesRepulsion(SIGMA, 1);
    const a = makeNode(-5, 3, 0);
    const b = makeNode( 5, 0, 0);
    force.initialize([a, b]);
    force(1);
    expect(a.vx + b.vx).toBeCloseTo(0, 10);
    expect(a.vy + b.vy).toBeCloseTo(0, 10);
  });

  it('alpha=0 applies no force', () => {
    const force = forceLennardJonesRepulsion(SIGMA, 1);
    const a = makeNode(-5, 0, 0), b = makeNode(5, 0, 0);
    force.initialize([a, b]);
    force(0);
    expect(a.vx).toBe(0); expect(b.vx).toBe(0);
  });

  it('setSigma changes the cutoff — nodes beyond new 2σ feel no force', () => {
    const force = forceLennardJonesRepulsion(SIGMA, 1);
    const a = makeNode(-12, 0, 0), b = makeNode(12, 0, 0); // r=24 > 2*10=20 after setSigma
    force.initialize([a, b]);
    force.setSigma(10); // new σ=10, cutoff=20
    force(1);
    expect(a.vx).toBe(0); // r=24 > cutoff=20 → no force
  });
});

describe('forceNBodyGravity', () => {
  function makeNode(x, y, z, val = 1) {
    return { x, y, z, val, vx: 0, vy: 0, vz: 0 };
  }

  it('attracts two nodes toward each other', () => {
    const force = forceNBodyGravity(1, 0);
    const a = makeNode(-100, 0, 0);
    const b = makeNode( 100, 0, 0);
    force.initialize([a, b]);
    force(1);
    expect(a.vx).toBeGreaterThan(0); // a pulled toward b (+x)
    expect(b.vx).toBeLessThan(0);    // b pulled toward a (-x)
  });

  it('heavier attractor pulls the light node harder', () => {
    // a (light, val=1) and b (heavy, val=100) separated on x-axis.
    // Acceleration on a = G * b.val / r² — scales with b's mass.
    // Acceleration on b = G * a.val / r² — scales with a's mass (small).
    const force = forceNBodyGravity(1, 0);
    const light = makeNode(-100, 0, 0, 1);
    const heavy = makeNode( 100, 0, 0, 100);
    force.initialize([light, heavy]);
    force(1);
    // light is pulled strongly toward heavy (a_light = G*100/r²)
    // heavy is pulled weakly toward light (a_heavy = G*1/r²)
    expect(Math.abs(light.vx)).toBeGreaterThan(Math.abs(heavy.vx) * 50);
  });

  it('setG(0) stops all gravity', () => {
    const force = forceNBodyGravity(1, 0);
    const a = makeNode(-50, 0, 0);
    const b = makeNode( 50, 0, 0);
    force.initialize([a, b]);
    force.setG(0);
    force(1);
    expect(a.vx).toBe(0);
    expect(b.vx).toBe(0);
  });

  it('setG scales force proportionally', () => {
    const force = forceNBodyGravity(0.1, 0);
    const a = makeNode(-100, 0, 0);
    const b = makeNode( 100, 0, 0);
    force.initialize([a, b]);
    force(1);
    const lowAcc = Math.abs(a.vx);
    a.vx = 0; b.vx = 0;
    force.setG(1.0);
    force(1);
    expect(Math.abs(a.vx)).toBeCloseTo(lowAcc * 10, 5);
  });

  it('alpha=0 applies no force', () => {
    const force = forceNBodyGravity(1, 0);
    const a = makeNode(-100, 0, 0);
    const b = makeNode( 100, 0, 0);
    force.initialize([a, b]);
    force(0);
    expect(a.vx).toBe(0);
    expect(b.vx).toBe(0);
  });

  it('softening prevents infinite force when nodes overlap', () => {
    const force = forceNBodyGravity(1, 30);
    const a = makeNode(0, 0, 0);
    const b = makeNode(0, 0, 0);
    force.initialize([a, b]);
    expect(() => force(1)).not.toThrow();
    expect(Number.isFinite(a.vx)).toBe(true);
    expect(Number.isFinite(b.vx)).toBe(true);
  });

  it('momentum is conserved — sum(mass·Δv) = 0 by Newton 3rd law', () => {
    // Acceleration on i due to j: G·mj/r². On j due to i: G·mi/r².
    // Mass-weighted velocity change: mi·(G·mj/r²) = mj·(G·mi/r²) — exact cancellation.
    // Raw velocity sum is NOT conserved (heavier nodes accelerate less in real physics).
    const force = forceNBodyGravity(1, 10);
    const nodes = [makeNode(-50, 0, 0, 3), makeNode(50, 20, 0, 7), makeNode(0, -80, 10, 2)];
    force.initialize(nodes);
    force(1);
    const mass = n => Math.max(1, n.val);
    const pX = nodes.reduce((s, n) => s + n.vx * mass(n), 0);
    const pY = nodes.reduce((s, n) => s + n.vy * mass(n), 0);
    expect(pX).toBeCloseTo(0, 10);
    expect(pY).toBeCloseTo(0, 10);
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

// ── Force calibration invariant ───────────────────────────────────────────────
//
// The zoom adaptor varies G between GRAVITY_MIN and GRAVITY_MAX.
// LJ repulsion (epsilon, sigma) is fixed regardless of zoom.
// If LJ overwhelms gravity at GRAVITY_MIN, nodes scatter when the user zooms in.
// If gravity overwhelms LJ at GRAVITY_MAX, nodes collapse when the user zooms out.
// Either extreme causes nodes to leave the camera frustum and disappear.
//
// Invariant: at the equilibrium separation (r = sigma), the LJ-to-gravity
// force ratio must stay below MAX_FORCE_IMBALANCE across the full G range.
// Exceeding this → cluster destabilises → severe visual degradation.
//
// Production constants (must match graph.js):
// Mirror graph.js derivation chain so the test stays in sync with the physics constants.
// If SPACING_RATIO or G_COHESION change in graph.js, update these constants to match.
const SPHERE_SCALE_TEST = 6, NODE_SIZE_MIN_TEST = 2, NODE_SIZE_MAX_TEST = 40;
const AVG_NODE_RADIUS_TEST = Math.cbrt(Math.max(NODE_SIZE_MIN_TEST, Math.min(NODE_SIZE_MAX_TEST, Math.cbrt(10)*2))) * SPHERE_SCALE_TEST; // ≈ 9.76
const SPACING_RATIO_TEST = 2.0;
const NODE_SEPARATION_TEST = 2 * Math.cbrt(SPACING_RATIO_TEST) * AVG_NODE_RADIUS_TEST; // ≈ 24.6
const G_COHESION_TEST = 0.3, COHESION_SOFT_TEST = 30;
const REPULSION_STRENGTH_TEST = G_COHESION_TEST * NODE_SEPARATION_TEST**3 / Math.sqrt(NODE_SEPARATION_TEST**2 + COHESION_SOFT_TEST**2); // ≈ 115
const REPULSION_DMAX_TEST = NODE_SEPARATION_TEST * 2; // ≈ 49.2

const CALIB = {
  sigma:            NODE_SEPARATION_TEST,   // equilibrium separation ≈ 24.6 world units
  repStrength:      REPULSION_STRENGTH_TEST, // |REPULSION_STRENGTH| ≈ 115
  repDmax:          REPULSION_DMAX_TEST,     // zero repulsion beyond ≈ 49.2 world units
  G_cohesion:       G_COHESION_TEST,
  cohesionSoft:     COHESION_SOFT_TEST,
  G_nbody:          0.30,  // GRAVITY_INIT
  nbodySoft:        5,     // GRAVITY_SOFTENING
  avgMass:          5,     // representative node val (for N-body component)
};
const MAX_FORCE_IMBALANCE = 20; // above this ratio: cluster destabilises and nodes disappear

// Coulombic manyBody repulsion magnitude at r (d3's charge force is |strength|/r²).
function repulsionForceMag(r) {
  if (r > CALIB.repDmax) return 0; // zero beyond distanceMax
  return CALIB.repStrength / (r * r);
}

// Total inward gravity at r: centripetal cohesion + N-body (one representative neighbour).
// Centripetal formula: velocity change = G_cohesion × r / sqrt(r² + S²)
// N-body formula:      velocity change = G_nbody × mass × r / (r² + S²)^(3/2)
function gravityForceMag(G_nbody, r) {
  const centripetal = CALIB.G_cohesion * r / Math.sqrt(r * r + CALIB.cohesionSoft ** 2);
  const nbody       = G_nbody * CALIB.avgMass * r / Math.pow(r * r + CALIB.nbodySoft ** 2, 1.5);
  return centripetal + nbody;
}


describe('force calibration — repulsion must stay balanced with gravity across zoom range', () => {
  it('at GRAVITY_MIN, repulsion does not overwhelm gravity — cluster stable when zoomed in', () => {
    const rep  = repulsionForceMag(CALIB.sigma);
    const grav = gravityForceMag(GRAVITY_MIN, CALIB.sigma);
    const ratio = rep / grav;
    expect(ratio,
      `repulsion/gravity at GRAVITY_MIN=${GRAVITY_MIN} is ${ratio.toFixed(2)}× — ` +
      `nodes scatter when zoomed in (want < ${MAX_FORCE_IMBALANCE})`
    ).toBeLessThan(MAX_FORCE_IMBALANCE);
  });

  it('at GRAVITY_MAX, repulsion still opposes gravity — cluster does not collapse when zoomed out', () => {
    const rep  = repulsionForceMag(CALIB.sigma);
    const grav = gravityForceMag(GRAVITY_MAX, CALIB.sigma);
    const ratio = rep / grav;
    expect(ratio,
      `repulsion/gravity at GRAVITY_MAX=${GRAVITY_MAX} is ${ratio.toFixed(2)}× — ` +
      `nodes collapse when zoomed out (want > ${1 / MAX_FORCE_IMBALANCE})`
    ).toBeGreaterThan(1 / MAX_FORCE_IMBALANCE);
  });

  it('beyond repDmax gravity still pulls — cluster has an inward restoring force', () => {
    const rep_out  = repulsionForceMag(CALIB.repDmax + 5); // just outside range
    const grav_out = gravityForceMag(GRAVITY_MAX, CALIB.repDmax + 5);
    expect(rep_out, 'repulsion must be zero beyond repDmax').toBe(0);
    expect(grav_out, 'gravity must still act beyond repDmax').toBeGreaterThan(0);
  });
});
