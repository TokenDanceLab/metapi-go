import { useEffect, type RefObject } from 'react';

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function listFocusable(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter((el) => {
    if (el.getAttribute('aria-hidden') === 'true') return false;
    if (el.tabIndex < 0) return false;
    // Offset parent null often means display:none; still allow fixed-position elements.
    const style = window.getComputedStyle(el);
    if (style.visibility === 'hidden' || style.display === 'none') return false;
    return true;
  });
}

/**
 * Trap Tab focus inside `containerRef` while `active` is true.
 * Restores focus to the previously focused element on deactivate.
 * Also focuses the first focusable (or container) when activated.
 */
export function useFocusTrap(
  active: boolean,
  containerRef: RefObject<HTMLElement | null>,
  options?: { restoreFocus?: boolean },
) {
  const restoreFocus = options?.restoreFocus !== false;

  useEffect(() => {
    if (!active || typeof document === 'undefined') return;
    const container = containerRef.current;
    if (!container) return;

    const previouslyFocused =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;

    const focusInitial = () => {
      const focusables = listFocusable(container);
      const target = focusables[0] ?? container;
      if (!target.hasAttribute('tabindex') && target === container) {
        container.tabIndex = -1;
      }
      try {
        target.focus({ preventScroll: true });
      } catch {
        /* jsdom / incomplete CSSOM */
      }
    };

    // Defer one frame so portal content is mounted.
    const raf = window.requestAnimationFrame(focusInitial);

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Tab') return;
      const focusables = listFocusable(container);
      if (focusables.length === 0) {
        event.preventDefault();
        try {
          container.focus({ preventScroll: true });
        } catch {
          /* ignore */
        }
        return;
      }
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const current = document.activeElement as HTMLElement | null;
      if (event.shiftKey) {
        if (!current || current === first || !container.contains(current)) {
          event.preventDefault();
          last.focus();
        }
      } else if (!current || current === last || !container.contains(current)) {
        event.preventDefault();
        first.focus();
      }
    };

    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      window.cancelAnimationFrame(raf);
      document.removeEventListener('keydown', onKeyDown, true);
      if (restoreFocus && previouslyFocused) {
        try {
          previouslyFocused.focus({ preventScroll: true });
        } catch {
          /* ignore */
        }
      }
    };
  }, [active, containerRef, restoreFocus]);
}

export { listFocusable, FOCUSABLE_SELECTOR };
