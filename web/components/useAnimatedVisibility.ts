import { useEffect, useState } from 'react';

export function useAnimatedVisibility(visible: boolean, durationMs = 220) {
  const [shouldRender, setShouldRender] = useState(visible);
  const [isVisible, setIsVisible] = useState(visible);

  useEffect(() => {
    if (visible) {
      setShouldRender(true);
      if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
        const rafId = window.requestAnimationFrame(() => setIsVisible(true));
        return () => {
          if (typeof window.cancelAnimationFrame === 'function') {
            window.cancelAnimationFrame(rafId);
          }
        };
      }
      setIsVisible(true);
      return undefined;
    }

    setIsVisible(false);
    if (durationMs <= 0) {
      setShouldRender(false);
      return undefined;
    }

    const timerId = globalThis.setTimeout(() => setShouldRender(false), durationMs);
    return () => globalThis.clearTimeout(timerId);
  }, [visible, durationMs]);

  return { shouldRender, isVisible };
}
