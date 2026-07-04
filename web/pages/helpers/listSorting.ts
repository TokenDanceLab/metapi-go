export type SortMode = 'custom' | 'balance-desc' | 'balance-asc';

type SortableBase = {
  id: number;
  isPinned?: boolean | null;
  sortOrder?: number | null;
};

export function sortItemsForDisplay<T extends SortableBase>(
  items: T[],
  mode: SortMode,
  getBalance: (item: T) => number,
): T[] {
  const list = [...items];
  const customComparator = (a: T, b: T) => {
    const aPinned = a.isPinned ? 1 : 0;
    const bPinned = b.isPinned ? 1 : 0;
    if (aPinned !== bPinned) return bPinned - aPinned;

    const aOrder = Number.isFinite(a.sortOrder as number) ? Number(a.sortOrder) : Number.MAX_SAFE_INTEGER;
    const bOrder = Number.isFinite(b.sortOrder as number) ? Number(b.sortOrder) : Number.MAX_SAFE_INTEGER;
    if (aOrder !== bOrder) return aOrder - bOrder;
    return a.id - b.id;
  };

  if (mode === 'custom') {
    return list.sort(customComparator);
  }

  return list.sort((a, b) => {
    const aPinned = a.isPinned ? 1 : 0;
    const bPinned = b.isPinned ? 1 : 0;
    if (aPinned !== bPinned) return bPinned - aPinned;

    const aBalance = Number.isFinite(getBalance(a)) ? getBalance(a) : 0;
    const bBalance = Number.isFinite(getBalance(b)) ? getBalance(b) : 0;
    if (aBalance !== bBalance) {
      return mode === 'balance-desc' ? bBalance - aBalance : aBalance - bBalance;
    }

    return customComparator(a, b);
  });
}

export function buildCustomReorderUpdates<T extends SortableBase>(
  items: T[],
  targetId: number,
  direction: 'up' | 'down',
): Array<{ id: number; sortOrder: number }> {
  const sorted = sortItemsForDisplay(items, 'custom', () => 0);
  const target = sorted.find((item) => item.id === targetId);
  if (!target) return [];

  const targetPinned = !!target.isPinned;
  const group = sorted.filter((item) => !!item.isPinned === targetPinned);
  const index = group.findIndex((item) => item.id === targetId);
  if (index < 0) return [];

  const swapIndex = direction === 'up' ? index - 1 : index + 1;
  if (swapIndex < 0 || swapIndex >= group.length) return [];

  const next = [...group];
  const temp = next[index];
  next[index] = next[swapIndex];
  next[swapIndex] = temp;

  const updates: Array<{ id: number; sortOrder: number }> = [];
  next.forEach((item, idx) => {
    const prev = Number.isFinite(item.sortOrder as number) ? Number(item.sortOrder) : Number.MAX_SAFE_INTEGER;
    if (prev !== idx) {
      updates.push({ id: item.id, sortOrder: idx });
    }
  });

  return updates;
}
