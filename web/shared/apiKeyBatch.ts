export function parseBatchApiKeys(value: string | null | undefined): string[] {
  const seen = new Set<string>();
  const items = String(value || '')
    .split(/[\s,;]+/g)
    .map((item) => item.trim())
    .filter(Boolean);

  return items.filter((item) => {
    if (seen.has(item)) return false;
    seen.add(item);
    return true;
  });
}

export function buildBatchApiKeyConnectionName(baseName: string, index: number, total: number): string {
  const name = String(baseName || '').trim() || 'API Key';
  if (total <= 1) return name;
  return `${name} ${index + 1}`;
}
