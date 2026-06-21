// packing.ts — Sunflower golden-angle packing with bottom-up sizing.
//
// Invariants:
// 1. childSize < parentSize (always)
// 2. Children don't overlap each other
// 3. Children fit inside the parent ring (0.85 × parentSize)
// 4. Leaf nodes are at least MIN_CHILD_SIZE

export const PACKING_K = 1.3;
// Minimum child size guarantees label legibility: at default zoom (~2),
// a node needs ≥3 world units to produce ≥6px screen diameter.
export const MIN_CHILD_SIZE = 3;

export interface PackResult {
  childSize: number;
  orbitRadius: number;
  parentSize: number;
}

export function computePacking(parentSize: number, childCount: number): PackResult {
  // Step 1: compute child size from parent ring
  const ringInner = parentSize * 0.85;
  const childSize = Math.max(MIN_CHILD_SIZE, ringInner / (1 + Math.sqrt(childCount) * PACKING_K));

  // Step 2: orbit radius from golden-angle formula
  const orbitRadius = childSize * Math.sqrt(childCount) * PACKING_K;
  const orbitBasedSize = (orbitRadius + childSize) / 0.85;

  // Step 3: collision-aware area check — the d3-force collision formula
  // is radius = size * 1.6 + 4. The parent must enclose all collision
  // circles, not just node radii. Without this, the force simulation
  // pushes children outside the parent boundary.
  const collisionR = childSize * 1.6 + 4;
  const totalCollisionArea = childCount * Math.PI * collisionR * collisionR;
  const areaBasedSize = Math.sqrt(totalCollisionArea / (0.55 * Math.PI)) + collisionR;

  // Step 4: parent is the max of orbit-based and area-based, then
  // recompute orbit to fit inside the final parent
  const finalParent = Math.max(parentSize, orbitBasedSize, areaBasedSize);
  return {
    childSize,
    orbitRadius: finalParent * 0.85 - childSize,
    parentSize: finalParent,
  };
}

export interface PackedPosition {
  x: number;
  y: number;
  size: number;
}

export function layoutChildren(parentSize: number, childCount: number): PackedPosition[] {
  return layoutFromPack(computePacking(parentSize, childCount), childCount);
}

export function layoutFromPack(pack: PackResult, childCount: number): PackedPosition[] {
  const goldenAngle = 137.508 * Math.PI / 180;
  const positions: PackedPosition[] = [];
  for (let i = 0; i < childCount; i++) {
    const angle = i * goldenAngle;
    const r = pack.orbitRadius * Math.sqrt((i + 0.5) / childCount);
    positions.push({ x: r * Math.cos(angle), y: r * Math.sin(angle), size: pack.childSize });
  }
  return positions;
}

export function parentSizeForChildren(childSize: number, childCount: number): number {
  const orbitRadius = childSize * Math.sqrt(childCount) * PACKING_K;
  return (orbitRadius + childSize) / 0.85;
}
