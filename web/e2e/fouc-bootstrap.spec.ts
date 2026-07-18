import { expect, test } from '@playwright/test';

/**
 * FOUC / theme bootstrap contract (#535 + #536).
 *
 * `theme_mode=dark` in localStorage before first paint must yield
 * `html[data-theme="dark"]` from the blocking head script (not only after
 * React hydration). See web/themeBootstrap.ts + web/index.html.
 */
test.describe('fouc bootstrap', () => {
  test('theme_mode=dark yields data-theme=dark after boot', async ({ page }) => {
    await page.addInitScript(() => {
      try {
        localStorage.setItem('theme_mode', 'dark');
        // Do not set legacy `theme` here — contract under test is theme_mode.
      } catch {
        /* ignore */
      }
    });

    await page.goto('/', { waitUntil: 'domcontentloaded' });

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark', {
      timeout: 15_000,
    });

    const mode = await page.evaluate(() => localStorage.getItem('theme_mode'));
    expect(mode).toBe('dark');
  });

  test('theme_mode=dark is set by head bootstrap before load event', async ({ page }) => {
    await page.addInitScript(() => {
      try {
        localStorage.setItem('theme_mode', 'dark');
      } catch {
        /* ignore */
      }
    });

    // Capture data-theme at load event — FOUC-1 requires dark already.
    await page.addInitScript(() => {
      (window as unknown as { __metapiThemeAtLoad?: string | null }).__metapiThemeAtLoad = null;
      window.addEventListener(
        'load',
        () => {
          (window as unknown as { __metapiThemeAtLoad?: string | null }).__metapiThemeAtLoad =
            document.documentElement.getAttribute('data-theme');
        },
        { once: true },
      );
    });

    await page.goto('/', { waitUntil: 'load' });

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark', {
      timeout: 15_000,
    });

    const atLoad = await page.evaluate(
      () => (window as unknown as { __metapiThemeAtLoad?: string | null }).__metapiThemeAtLoad,
    );

    // FOUC Phase 1: head bootstrap must have applied dark before load.
    expect(atLoad).toBe('dark');
  });
});
