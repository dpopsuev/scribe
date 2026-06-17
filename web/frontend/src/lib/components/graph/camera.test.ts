import { describe, it, expect } from 'vitest';
import {
  createFocusLock, userTakeLock, checkIdle,
  systemCanMove, isTrackingNode, fitBounds, IDLE_TIMEOUT_MS,
} from './camera';

describe('FocusLock state machine', () => {
  it('starts with system owning the lock', () => {
    const lock = createFocusLock();
    expect(lock.owner).toBe('system');
    expect(systemCanMove(lock)).toBe(true);
  });

  it('user takes lock on interaction', () => {
    const lock = userTakeLock(createFocusLock());
    expect(lock.owner).toBe('user');
    expect(systemCanMove(lock)).toBe(false);
  });

  it('idle timeout returns lock to system', () => {
    let lock = userTakeLock(createFocusLock());
    // Simulate time passing beyond idle threshold
    lock = { ...lock, lastInteraction: Date.now() - IDLE_TIMEOUT_MS - 1 };
    lock = checkIdle(lock);
    expect(lock.owner).toBe('system');
    expect(systemCanMove(lock)).toBe(true);
  });

  it('no idle timeout before threshold', () => {
    let lock = userTakeLock(createFocusLock());
    lock = { ...lock, lastInteraction: Date.now() - 1000 }; // 1s ago
    lock = checkIdle(lock);
    expect(lock.owner).toBe('user'); // still user
  });

  it('shift+click sets focus node', () => {
    const lock = userTakeLock(createFocusLock(), 'node-42');
    expect(isTrackingNode(lock)).toBe(true);
    expect(lock.focusNodeId).toBe('node-42');
  });

  it('idle clears focus node', () => {
    let lock = userTakeLock(createFocusLock(), 'node-42');
    lock = { ...lock, lastInteraction: Date.now() - IDLE_TIMEOUT_MS - 1 };
    lock = checkIdle(lock);
    expect(lock.focusNodeId).toBeNull();
    expect(isTrackingNode(lock)).toBe(false);
  });

  it('user can retake lock after idle', () => {
    let lock = userTakeLock(createFocusLock());
    lock = { ...lock, lastInteraction: Date.now() - IDLE_TIMEOUT_MS - 1 };
    lock = checkIdle(lock);
    expect(lock.owner).toBe('system');
    lock = userTakeLock(lock);
    expect(lock.owner).toBe('user');
  });

  it('system lock is idempotent under checkIdle', () => {
    const lock = createFocusLock();
    const checked = checkIdle(lock);
    expect(checked.owner).toBe('system');
  });
});

describe('fitBounds', () => {
  it('centers on node cluster', () => {
    const nodes = [
      { x: -10, y: -10, _size: 5 },
      { x: 10, y: 10, _size: 5 },
    ];
    const cam = fitBounds(nodes, 800, 600);
    expect(cam).not.toBeNull();
    expect(cam!.x).toBeCloseTo(0);
    expect(cam!.y).toBeCloseTo(0);
  });

  it('uses center of mass not bounding box center', () => {
    const nodes = [
      { x: 0, y: 0, _size: 20 },   // heavy node at origin
      { x: 100, y: 0, _size: 1 },   // light outlier far away
    ];
    const cam = fitBounds(nodes, 800, 600);
    // Bounding box center = 50, center of mass = 100/21 ≈ 4.76
    expect(cam!.x).toBeCloseTo(100 / 21, 1);
    expect(cam!.x).toBeLessThan(10);
  });

  it('returns null for empty nodes', () => {
    expect(fitBounds([], 800, 600)).toBeNull();
  });

  it('zoom fits nodes in viewport', () => {
    const nodes = [
      { x: 0, y: 0, _size: 5 },
      { x: 100, y: 0, _size: 5 },
    ];
    const cam = fitBounds(nodes, 800, 600);
    // Span is ~115 (100 + 2*5 + pad), zoom = 800/115 ≈ 6.3
    expect(cam!.zoom).toBeGreaterThan(4);
    expect(cam!.zoom).toBeLessThan(10);
  });
});
