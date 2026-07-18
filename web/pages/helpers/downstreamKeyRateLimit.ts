/**
 * Downstream key maxRpm / maxTpm helpers (learn #116 / #475).
 * Matches backend normalizeQuotaIntOrNull:
 *   empty/null/0/negative → null (unlimited)
 *   positive integer → stored value
 * Client rejects non-integer garbage so operators get a clear error
 * instead of a silent clear.
 */

export type QuotaIntParseResult =
  | { valid: true; value: number | null }
  | { valid: false; value: null; error: string };

const INVALID_QUOTA_INT_ERROR = '速率上限必须是正整数；留空或 0 表示不限';

export function normalizeQuotaIntInput(raw: string | number | null | undefined): string {
  if (raw === null || raw === undefined) return '';
  return String(raw).trim();
}

/**
 * Parse optional rate-limit field for create/update payloads.
 * Empty / 0 / negative → null (unlimited, same as backend clear contract).
 * Positive whole numbers → value. Non-integers / non-numeric → invalid.
 */
export function parseQuotaIntOrNull(
  raw: string | number | null | undefined,
  fieldLabel = '速率上限',
): QuotaIntParseResult {
  const trimmed = normalizeQuotaIntInput(raw);
  if (!trimmed) {
    return { valid: true, value: null };
  }

  // Explicit signed zero / negative: clear to unlimited (backend <=0 → NULL).
  if (/^-\d+$/.test(trimmed) || trimmed === '0' || /^0+$/.test(trimmed)) {
    return { valid: true, value: null };
  }

  // Positive integer only — reject decimals / scientific / garbage.
  if (!/^\d+$/.test(trimmed)) {
    return {
      valid: false,
      value: null,
      error: `${fieldLabel}必须是正整数；留空或 0 表示不限`,
    };
  }

  const value = Number(trimmed);
  if (!Number.isFinite(value) || value <= 0) {
    return { valid: true, value: null };
  }

  // Guard against values that overflow safe integer range for JSON.
  if (value > Number.MAX_SAFE_INTEGER) {
    return {
      valid: false,
      value: null,
      error: INVALID_QUOTA_INT_ERROR,
    };
  }

  return { valid: true, value: Math.trunc(value) };
}

export function hasDownstreamKeyRateLimit(value?: number | null): boolean {
  const n = Number(value);
  return Number.isFinite(n) && n > 0;
}

/** Compact token-style suffix: 1000 → 1k, 100000 → 100k, 1_500_000 → 1.5M */
function formatCompactCount(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0';
  const n = Math.trunc(value);
  if (n >= 1_000_000_000) {
    const scaled = n / 1_000_000_000;
    return `${Number.isInteger(scaled) ? scaled.toFixed(0) : scaled.toFixed(1).replace(/\.0$/, '')}B`;
  }
  if (n >= 1_000_000) {
    const scaled = n / 1_000_000;
    return `${Number.isInteger(scaled) ? scaled.toFixed(0) : scaled.toFixed(1).replace(/\.0$/, '')}M`;
  }
  if (n >= 1_000) {
    const scaled = n / 1_000;
    return `${Number.isInteger(scaled) ? scaled.toFixed(0) : scaled.toFixed(1).replace(/\.0$/, '')}k`;
  }
  return String(n);
}

/**
 * Compact list/detail badge when maxRpm is set.
 * Returns null when unlimited.
 */
export function formatDownstreamKeyMaxRpm(value?: number | null): string | null {
  if (!hasDownstreamKeyRateLimit(value)) return null;
  return `RPM ${formatCompactCount(Number(value))}`;
}

/**
 * Compact list/detail badge when maxTpm is set.
 * Returns null when unlimited. Example: TPM 100k
 */
export function formatDownstreamKeyMaxTpm(value?: number | null): string | null {
  if (!hasDownstreamKeyRateLimit(value)) return null;
  return `TPM ${formatCompactCount(Number(value))}`;
}

/** Join set rate badges for list cells; empty when both unlimited. */
export function formatDownstreamKeyRateLimits(
  maxRpm?: number | null,
  maxTpm?: number | null,
): string | null {
  const parts = [
    formatDownstreamKeyMaxRpm(maxRpm),
    formatDownstreamKeyMaxTpm(maxTpm),
  ].filter(Boolean) as string[];
  return parts.length > 0 ? parts.join(' · ') : null;
}
