import { describe, expect, it } from 'vitest';
import { buildCustomReorderUpdates, sortItemsForDisplay, type SortMode } from './listSorting.js';

type Item = {
  id: number;
  isPinned?: boolean | null;
  sortOrder?: number | null;
  balance?: number | null;
};

function ids(items: Item[]): number[] {
  return items.map((item) => item.id);
}

describe('sortItemsForDisplay', () => {
  const base: Item[] = [
    { id: 1, isPinned: false, sortOrder: 2, balance: 5 },
    { id: 2, isPinned: true, sortOrder: 1, balance: 1 },
    { id: 3, isPinned: false, sortOrder: 0, balance: 20 },
    { id: 4, isPinned: true, sortOrder: 0, balance: 10 },
  ];

  it('keeps pinned items first in custom mode', () => {
    const mode: SortMode = 'custom';
    const sorted = sortItemsForDisplay(base, mode, (item) => item.balance || 0);
    expect(ids(sorted)).toEqual([4, 2, 3, 1]);
  });

  it('sorts by balance desc while keeping pinned items first', () => {
    const sorted = sortItemsForDisplay(base, 'balance-desc', (item) => item.balance || 0);
    expect(ids(sorted)).toEqual([4, 2, 3, 1]);
  });

  it('sorts by balance asc while keeping pinned items first', () => {
    const sorted = sortItemsForDisplay(base, 'balance-asc', (item) => item.balance || 0);
    expect(ids(sorted)).toEqual([2, 4, 1, 3]);
  });
});

describe('buildCustomReorderUpdates', () => {
  const list: Item[] = [
    { id: 10, isPinned: true, sortOrder: 0 },
    { id: 11, isPinned: true, sortOrder: 1 },
    { id: 20, isPinned: false, sortOrder: 0 },
    { id: 21, isPinned: false, sortOrder: 1 },
  ];

  it('reorders only inside the same pinned group', () => {
    const updates = buildCustomReorderUpdates(list, 20, 'up');
    // First unpinned item cannot move above pinned group.
    expect(updates).toEqual([]);
  });

  it('returns normalized sortOrder updates after moving down', () => {
    const updates = buildCustomReorderUpdates(list, 20, 'down');
    expect(updates).toEqual([
      { id: 21, sortOrder: 0 },
      { id: 20, sortOrder: 1 },
    ]);
  });
});
