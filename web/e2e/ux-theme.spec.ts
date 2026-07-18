import { expect, test } from '@playwright/test';

/**
 * UX smoke: theme persistence key `theme_mode` drives `data-theme` (#536).
 * Runs against Vite preview without a backend — login surface is enough.
 */
test.describe('ux theme', () => {
  test('theme_mode=dark sets html[data-theme=dark]', async ({ page }) => {
    await page.addInitScript(() => {
      try {
        localStorage.setItem('theme_mode', 'dark');
        // Keep legacy key in sync for the inline bootstrap path in index.html.
        localStorage.setItem('theme', 'dark');
      } catch {
        /* ignore */
      }
    });

    await page.goto('/', { waitUntil: 'domcontentloaded' });

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark', {
      timeout: 15_000,
    });

    const stored = await page.evaluate(() => localStorage.getItem('theme_mode'));
    expect(stored).toBe('dark');
  });

  test('theme_mode=light sets html[data-theme=light]', async ({ page }) => {
    await page.addInitScript(() => {
      try {
        localStorage.setItem('theme_mode', 'light');
        localStorage.setItem('theme', 'light');
      } catch {
        /* ignore */
      }
    });

    await page.goto('/', { waitUntil: 'domcontentloaded' });

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light', {
      timeout: 15_000,
    });
  });

  test('login surface renders without hard console errors', async ({ page }) => {
    const hardErrors: string[] = [];
    page.on('pageerror', (err) => {
      hardErrors.push(String(err));
    });
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const text = msg.text();
        // Ignore expected API noise when preview has no backend.
        if (/Failed to fetch|net::ERR_|404|401|NetworkError/i.test(text)) return;
        hardErrors.push(text);
      }
    });

    await page.addInitScript(() => {
      try {
        localStorage.setItem('theme_mode', 'dark');
        localStorage.setItem('theme', 'dark');
      } catch {
        /* ignore */
      }
    });

    await page.goto('/', { waitUntil: 'networkidle' }).catch(async () => {
      await page.goto('/', { waitUntil: 'domcontentloaded' });
    });

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    // Login shell or app root must mount.
    await expect(page.locator('#root')).toBeAttached();
    expect(hardErrors, `unexpected console/page errors:\n${hardErrors.join('\n')}`).toEqual([]);
  });
});
