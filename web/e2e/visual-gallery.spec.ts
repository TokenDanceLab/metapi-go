import { expect, test, type Page } from '@playwright/test';

const GALLERY_PATH = '/__design__';

async function galleryAvailable(page: Page): Promise<boolean> {
  const response = await page.goto(GALLERY_PATH, { waitUntil: 'domcontentloaded' });
  if (!response) return false;
  // Vite SPA fallback serves index.html with 200 for unknown paths; treat a
  // missing gallery as either real 404 or a non-gallery shell without marker.
  if (response.status() === 404) return false;
  const marker = page.locator(
    '[data-testid="design-system-gallery"], [data-design-gallery], #design-gallery, [data-testid="design-gallery"]',
  );
  try {
    await marker.first().waitFor({ state: 'visible', timeout: 8_000 });
    return true;
  } catch {
    // Soft presence: title/body text used by design-system scaffold when present.
    const bodyText = (await page.locator('body').innerText().catch(() => '')).toLowerCase();
    if (bodyText.includes('design system') || bodyText.includes('design gallery')) {
      return true;
    }
    // If the app just rendered the login/shell for an unknown route, skip.
    return false;
  }
}

async function applyTheme(page: Page, mode: 'light' | 'dark') {
  await page.addInitScript((themeMode) => {
    try {
      localStorage.setItem('theme_mode', themeMode);
      localStorage.setItem('theme', themeMode);
      // Production preview builds gate /__design__ behind this flag (#533).
      localStorage.setItem('metapi_design_gallery', '1');
    } catch {
      /* ignore */
    }
  }, mode);
}

test.describe('visual gallery @visual', () => {
  test.describe.configure({ mode: 'serial' });

  for (const mode of ['light', 'dark'] as const) {
    test(`/__design__ ${mode} baseline`, async ({ page }) => {
      await applyTheme(page, mode);
      const available = await galleryAvailable(page);
      test.skip(!available, 'design gallery route /__design__ not available (404 or not scaffolded)');

      await page.emulateMedia({ colorScheme: mode });
      await expect(page.locator('html')).toHaveAttribute('data-theme', mode, {
        timeout: 15_000,
      });

      // Stabilize fonts/layout before snapshot.
      await page.waitForTimeout(300);
      await expect(page).toHaveScreenshot(`design-gallery-${mode}.png`, {
        fullPage: true,
      });
    });
  }
});
