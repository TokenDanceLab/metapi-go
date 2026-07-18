import { describe, expect, it } from 'vitest';
import {
  formatDownstreamKeyMaxRpm,
  formatDownstreamKeyMaxTpm,
  formatDownstreamKeyRateLimits,
  hasDownstreamKeyRateLimit,
  normalizeQuotaIntInput,
  parseQuotaIntOrNull,
} from './downstreamKeyRateLimit.js';

describe('downstreamKeyRateLimit', () => {
  it('normalizes empty / null input', () => {
    expect(normalizeQuotaIntInput(null)).toBe('');
    expect(normalizeQuotaIntInput(undefined)).toBe('');
    expect(normalizeQuotaIntInput('  ')).toBe('');
    expect(normalizeQuotaIntInput(60)).toBe('60');
    expect(normalizeQuotaIntInput('  100  ')).toBe('100');
  });

  it('parses empty/0/negative as unlimited null (backend clear contract)', () => {
    expect(parseQuotaIntOrNull('')).toEqual({ valid: true, value: null });
    expect(parseQuotaIntOrNull('   ')).toEqual({ valid: true, value: null });
    expect(parseQuotaIntOrNull(null)).toEqual({ valid: true, value: null });
    expect(parseQuotaIntOrNull('0')).toEqual({ valid: true, value: null });
    expect(parseQuotaIntOrNull('00')).toEqual({ valid: true, value: null });
    expect(parseQuotaIntOrNull('-1')).toEqual({ valid: true, value: null });
    expect(parseQuotaIntOrNull(-5)).toEqual({ valid: true, value: null });
  });

  it('accepts positive integers', () => {
    expect(parseQuotaIntOrNull('60')).toEqual({ valid: true, value: 60 });
    expect(parseQuotaIntOrNull('  100000  ')).toEqual({ valid: true, value: 100000 });
    expect(parseQuotaIntOrNull(42)).toEqual({ valid: true, value: 42 });
  });

  it('rejects non-integer garbage instead of silently clearing', () => {
    expect(parseQuotaIntOrNull('abc').valid).toBe(false);
    expect(parseQuotaIntOrNull('12.5').valid).toBe(false);
    expect(parseQuotaIntOrNull('1e3').valid).toBe(false);
    expect(parseQuotaIntOrNull('60rpm').valid).toBe(false);
    expect(parseQuotaIntOrNull('abc').error).toContain('正整数');
  });

  it('formats compact RPM/TPM badges only when set', () => {
    expect(hasDownstreamKeyRateLimit(null)).toBe(false);
    expect(hasDownstreamKeyRateLimit(0)).toBe(false);
    expect(hasDownstreamKeyRateLimit(60)).toBe(true);

    expect(formatDownstreamKeyMaxRpm(null)).toBeNull();
    expect(formatDownstreamKeyMaxRpm(0)).toBeNull();
    expect(formatDownstreamKeyMaxRpm(60)).toBe('RPM 60');
    expect(formatDownstreamKeyMaxRpm(1500)).toBe('RPM 1.5k');

    expect(formatDownstreamKeyMaxTpm(null)).toBeNull();
    expect(formatDownstreamKeyMaxTpm(100_000)).toBe('TPM 100k');
    expect(formatDownstreamKeyMaxTpm(1_500_000)).toBe('TPM 1.5M');

    expect(formatDownstreamKeyRateLimits(null, null)).toBeNull();
    expect(formatDownstreamKeyRateLimits(60, null)).toBe('RPM 60');
    expect(formatDownstreamKeyRateLimits(null, 100_000)).toBe('TPM 100k');
    expect(formatDownstreamKeyRateLimits(60, 100_000)).toBe('RPM 60 · TPM 100k');
  });
});
