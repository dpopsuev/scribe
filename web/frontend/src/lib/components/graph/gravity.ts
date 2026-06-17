// gravity.ts — N-body gravitational force for d3-force simulation.
// F = G * m1 * m2 / r² with Plummer softening to prevent singularity.

export function forceGravity(G: number, softening: number) {
  let nodes: any[] = [];

  function force(alpha: number) {
    for (let i = 0; i < nodes.length; i++) {
      for (let j = i + 1; j < nodes.length; j++) {
        const a = nodes[i], b = nodes[j];
        const dx = (b.x || 0) - (a.x || 0);
        const dy = (b.y || 0) - (a.y || 0);
        const distSq = dx * dx + dy * dy + softening * softening;
        const dist = Math.sqrt(distSq);
        const massA = (a._size || 5) * 0.5;
        const massB = (b._size || 5) * 0.5;
        const F = G * massA * massB / distSq * alpha;
        const fx = F * dx / dist;
        const fy = F * dy / dist;
        a.vx = (a.vx || 0) + fx / massA;
        a.vy = (a.vy || 0) + fy / massA;
        b.vx = (b.vx || 0) - fx / massB;
        b.vy = (b.vy || 0) - fy / massB;
      }
    }
  }

  force.initialize = (n: any[]) => { nodes = n; };
  return force;
}
