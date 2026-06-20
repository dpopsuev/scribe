import { describe, it, expect } from 'vitest';

function molecularChildSize(parentSize: number, childCount: number, emptyFraction = 0.4): number {
  return Math.max(0.8, Math.sqrt(parentSize ** 2 * (1 - emptyFraction) / childCount));
}

describe('molecular packing', () => {
  it('10 children in size-18 parent', () => {
    const cs = molecularChildSize(18, 10);
    const totalChildArea = 10 * Math.PI * cs ** 2;
    const parentArea = Math.PI * 18 ** 2;
    expect(totalChildArea / parentArea).toBeCloseTo(0.6, 1);
    expect(cs).toBeGreaterThan(1);
    expect(cs).toBeLessThan(18);
  });

  it('50 children → smaller than 10 children', () => {
    const cs10 = molecularChildSize(18, 10);
    const cs50 = molecularChildSize(18, 50);
    expect(cs50).toBeLessThan(cs10);
  });

  it('1 child → nearly fills parent', () => {
    const cs = molecularChildSize(18, 1);
    expect(cs).toBeCloseTo(18 * Math.sqrt(0.6), 0);
  });

  it('100 children → each is tiny but above minimum', () => {
    const cs = molecularChildSize(18, 100);
    expect(cs).toBeGreaterThanOrEqual(0.8);
    expect(cs).toBeLessThan(3);
  });

  it('small parent with many children clamps to minimum', () => {
    const cs = molecularChildSize(3, 50);
    expect(cs).toBe(0.8);
  });

  it('children area never exceeds parent area', () => {
    for (const n of [1, 5, 10, 50, 100, 500]) {
      const cs = molecularChildSize(18, n);
      const totalChildArea = n * Math.PI * cs ** 2;
      const parentArea = Math.PI * 18 ** 2;
      expect(totalChildArea).toBeLessThanOrEqual(parentArea * 1.01);
    }
  });
});
