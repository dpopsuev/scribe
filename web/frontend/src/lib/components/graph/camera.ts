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
  lastInteraction: number; // timestamp
}

const IDLE_MS = 5000;

export function createFocusLock(): FocusLock {
  return { owner: 'system', focusNodeId: null, lastInteraction: 0 };
}

export function userTakeLock(lock: FocusLock, focusNodeId?: string): FocusLock {
  return {
    owner: 'user',
    focusNodeId: focusNodeId ?? lock.focusNodeId,
    lastInteraction: Date.now(),
  };
}

export function checkIdle(lock: FocusLock, now: number = Date.now()): FocusLock {
  if (lock.owner === 'user' && now - lock.lastInteraction >= IDLE_MS) {
    return { owner: 'system', focusNodeId: null, lastInteraction: lock.lastInteraction };
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
  let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
  for (const n of nodes) {
    const s = n._size || 5;
    if (n.x - s < minX) minX = n.x - s;
    if (n.x + s > maxX) maxX = n.x + s;
    if (n.y - s < minY) minY = n.y - s;
    if (n.y + s > maxY) maxY = n.y + s;
  }
  const pad = 1.15;
  const spanX = (maxX - minX) * pad || 100;
  const spanY = (maxY - minY) * pad || 100;
  return {
    x: (minX + maxX) / 2,
    y: (minY + maxY) / 2,
    zoom: Math.min(viewWidth / spanX, viewHeight / spanY),
  };
}

export const IDLE_TIMEOUT_MS = IDLE_MS;
