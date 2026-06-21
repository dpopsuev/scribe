// packing.ts — Sunflower golden-angle packing with bottom-up sizing.
//
// Invariants:
// 1. childSize < parentSize (always)
// 2. Children don't overlap each other
// 3. Children fit inside the parent ring (0.85 × parentSize)
// 4. Leaf nodes are at least MIN_CHILD_SIZE

export const PACKING_K = 1.3;
// Minimum child size guarantees shape visibility. Labels are handled by
// zoom-scaled rendering, so children can be small — the user zooms in.
export const MIN_CHILD_SIZE = 0.8;

export interface PackResult {
  childSize: number;
  orbitRadius: number;
  parentSize: number;
}

export function computePacking(parentSize: number, childCount: number): PackResult {
  // Parent grows at most 2× to avoid visual explosion over neighbors
  const maxParent = parentSize * 2;
  const ringInner = maxParent * 0.85;

  // Child size computed from the max-allowed parent ring
  const childSize = Math.max(MIN_CHILD_SIZE, ringInner / (1 + Math.sqrt(childCount) * PACKING_K));
  const orbitRadius = childSize * Math.sqrt(childCount) * PACKING_K;
  const neededSize = (orbitRadius + childSize) / 0.85;

  // Clamp parent, then scale orbit to fit
  const finalParent = Math.min(Math.max(parentSize, neededSize), maxParent);
  const finalOrbit = finalParent * 0.85 - childSize;

  return {
    childSize,
    orbitRadius: finalOrbit,
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
