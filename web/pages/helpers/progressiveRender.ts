export function getInitialVisibleCount(total: number, chunkSize: number): number {
  if (!Number.isFinite(total) || total <= 0) return 0;
  const normalizedChunk = Number.isFinite(chunkSize) && chunkSize > 0 ? Math.trunc(chunkSize) : 1;
  return Math.min(Math.trunc(total), normalizedChunk);
}

export function getNextVisibleCount(currentVisible: number, total: number, chunkSize: number): number {
  const normalizedTotal = Number.isFinite(total) && total > 0 ? Math.trunc(total) : 0;
  if (normalizedTotal <= 0) return 0;

  const normalizedCurrent = Number.isFinite(currentVisible) && currentVisible > 0
    ? Math.trunc(currentVisible)
    : 0;
  const normalizedChunk = Number.isFinite(chunkSize) && chunkSize > 0 ? Math.trunc(chunkSize) : 1;

  return Math.min(normalizedTotal, normalizedCurrent + normalizedChunk);
}
