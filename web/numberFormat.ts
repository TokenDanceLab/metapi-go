function toSafeNumber(value: unknown): number {
  if (typeof value !== 'number' || Number.isNaN(value) || !Number.isFinite(value)) return 0;
  return value;
}

function trimTrailingZero(value: string): string {
  return value.replace(/\.0$/, '');
}

export function formatCompactTokenMetric(value: number): string {
  const normalized = Math.round(toSafeNumber(value));
  const absoluteValue = Math.abs(normalized);

  if (absoluteValue >= 1_000_000_000) {
    return `${trimTrailingZero((normalized / 1_000_000_000).toFixed(1))}B`;
  }

  if (absoluteValue >= 1_000_000) {
    return `${trimTrailingZero((normalized / 1_000_000).toFixed(1))}M`;
  }

  if (absoluteValue >= 1_000) {
    return `${trimTrailingZero((normalized / 1_000).toFixed(1))}K`;
  }

  return normalized.toLocaleString();
}
