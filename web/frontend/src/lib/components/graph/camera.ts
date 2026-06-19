// camera.ts — Camera state machine with focus lock and idle detection.
//
// State machine:
//   [system] → user interacts → [user]
//   [user]   → idle timeout   → [system]
//   [user]   → shift+click    → [tracking node]
//
// The system can auto-fit the camera only when it holds the lock.
// The user takes the lock on any drag, zoom, or shift+click.
// After IDLE_MS of no interaction, the lock returns to the system.

export interface CameraState {
  x: number;
  y: number;
  zoom: number;
}

export interface FocusLock {
  owner: 'system' | 'user';
  focusNodeId: string | null;
  pinned: boolean;
  lastInteraction: number; // timestamp
}

const IDLE_MS = 5000;

export function createFocusLock(): FocusLock {
  return { owner: 'system', focusNodeId: null, pinned: false, lastInteraction: 0 };
}

export function userTakeLock(lock: FocusLock, focusNodeId?: string): FocusLock {
  return {
    owner: 'user',
    focusNodeId: focusNodeId ?? lock.focusNodeId,
    pinned: lock.pinned,
    lastInteraction: Date.now(),
  };
}

export function pinToggle(lock: FocusLock, nodeId: string): FocusLock {
  if (lock.pinned && lock.focusNodeId === nodeId) {
    return { owner: 'system', focusNodeId: null, pinned: false, lastInteraction: Date.now() };
  }
  return { owner: 'user', focusNodeId: nodeId, pinned: true, lastInteraction: Date.now() };
}

export function checkIdle(lock: FocusLock, now: number = Date.now()): FocusLock {
  if (lock.pinned) return lock;
  if (lock.owner === 'user' && now - lock.lastInteraction >= IDLE_MS) {
    return { owner: 'system', focusNodeId: null, pinned: false, lastInteraction: lock.lastInteraction };
  }
  return lock;
}

export function systemCanMove(lock: FocusLock): boolean {
  return lock.owner === 'system';
}

export function isTrackingNode(lock: FocusLock): boolean {
  return lock.owner === 'user' && lock.focusNodeId !== null;
}

export function fitBounds(
  nodes: Array<{ x: number; y: number; _size: number }>,
  viewWidth: number,
  viewHeight: number,
): CameraState | null {
  if (nodes.length === 0) return null;
  let totalMass = 0, cx = 0, cy = 0;
  let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
  for (const n of nodes) {
    const mass = n._size || 5;
    cx += (n.x || 0) * mass;
    cy += (n.y || 0) * mass;
    totalMass += mass;
    if (n.x - mass < minX) minX = n.x - mass;
    if (n.x + mass > maxX) maxX = n.x + mass;
    if (n.y - mass < minY) minY = n.y - mass;
    if (n.y + mass > maxY) maxY = n.y + mass;
  }
  const pad = 1.15;
  const spanX = (maxX - minX) * pad || 100;
  const spanY = (maxY - minY) * pad || 100;
  return {
    x: cx / totalMass,
    y: cy / totalMass,
    zoom: Math.min(viewWidth / spanX, viewHeight / spanY),
  };
}

// Smooth camera transition — Perlin's smootherstep (C² continuous).
// Zero velocity AND zero acceleration at both ends: soft start, builds
// momentum through the middle, gentle deceleration approaching target.
// 800ms is the sweet spot per Material Design / NN/g research for
// camera-class transitions that require user attention.

export const TRANSITION_MS = 800;

export interface CameraTransition {
  fromX: number; fromY: number; fromZoom: number;
  toX: number; toY: number; toZoom: number;
  startTime: number;
  duration: number;
}

function smootherstep(t: number): number {
  return t * t * t * (t * (6 * t - 15) + 10);
}

export function startTransition(
  from: CameraState, to: CameraState, duration: number = TRANSITION_MS,
): CameraTransition {
  return {
    fromX: from.x, fromY: from.y, fromZoom: from.zoom,
    toX: to.x, toY: to.y, toZoom: to.zoom,
    startTime: Date.now(),
    duration,
  };
}

export function tickTransition(
  tr: CameraTransition, now: number = Date.now(),
): { cam: CameraState; done: boolean } {
  const elapsed = now - tr.startTime;
  if (elapsed >= tr.duration) {
    return { cam: { x: tr.toX, y: tr.toY, zoom: tr.toZoom }, done: true };
  }
  const p = smootherstep(elapsed / tr.duration);
  const logFrom = Math.log(tr.fromZoom);
  const logTo = Math.log(tr.toZoom);
  return {
    cam: {
      x: tr.fromX + (tr.toX - tr.fromX) * p,
      y: tr.fromY + (tr.toY - tr.fromY) * p,
      zoom: Math.exp(logFrom + (logTo - logFrom) * p),
    },
    done: false,
  };
}

export const IDLE_TIMEOUT_MS = IDLE_MS;
