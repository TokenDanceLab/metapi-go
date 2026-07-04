import { useEffect, useState } from 'react';

function readThemeLabelColor(): string | null {
  if (typeof document === 'undefined') return null;

  const root = document.documentElement;
  if (!root || typeof globalThis.getComputedStyle !== 'function') return null;

  const color = globalThis.getComputedStyle(root).getPropertyValue('--color-text-secondary').trim();
  return color || null;
}

export function useThemeLabelColor(fallback = '#9ca3af'): string {
  const [labelColor, setLabelColor] = useState(fallback);

  useEffect(() => {
    const root = typeof document !== 'undefined' ? document.documentElement : null;
    const read = () => {
      const color = readThemeLabelColor();
      if (color) setLabelColor(color);
    };

    read();

    if (!root || typeof globalThis.MutationObserver !== 'function') {
      return undefined;
    }

    const observer = new globalThis.MutationObserver(read);
    observer.observe(root, { attributes: true, attributeFilter: ['data-theme'] });
    return () => observer.disconnect();
  }, []);

  return labelColor;
}
