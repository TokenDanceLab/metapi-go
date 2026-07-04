import { describe, expect, it } from 'vitest';
import { getInitialVisibleCount, getNextVisibleCount } from './progressiveRender.js';

describe('progressiveRender', () => {
  it('returns bounded initial visible count', () => {
    expect(getInitialVisibleCount(0, 40)).toBe(0);
    expect(getInitialVisibleCount(10, 40)).toBe(10);
    expect(getInitialVisibleCount(120, 40)).toBe(40);
  });

  it('returns next visible count without exceeding total', () => {
    expect(getNextVisibleCount(0, 120, 40)).toBe(40);
    expect(getNextVisibleCount(40, 120, 40)).toBe(80);
    expect(getNextVisibleCount(80, 120, 40)).toBe(120);
    expect(getNextVisibleCount(200, 120, 40)).toBe(120);
  });
});
