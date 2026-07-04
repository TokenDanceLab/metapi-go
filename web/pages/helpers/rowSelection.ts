export function shouldIgnoreRowSelectionClick(target: EventTarget | null): boolean {
  const element = target as { closest?: (selector: string) => unknown } | null;
  return !!element?.closest?.('button, a, input, textarea, select, label, [role="button"]');
}
