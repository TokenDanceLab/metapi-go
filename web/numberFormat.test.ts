import { describe, expect, it } from 'vitest';
import { formatCompactTokenMetric } from './numberFormat.js';

describe('formatCompactTokenMetric', () => {
  it('keeps small values uncompressed', () => {
    expect(formatCompactTokenMetric(950)).toBe('950');
  });

  it('formats thousands with K suffix', () => {
    expect(formatCompactTokenMetric(7_974)).toBe('8K');
  });

  it('formats millions with M suffix', () => {
    expect(formatCompactTokenMetric(1_800_000)).toBe('1.8M');
    expect(formatCompactTokenMetric(611_540_335)).toBe('611.5M');
  });
});
