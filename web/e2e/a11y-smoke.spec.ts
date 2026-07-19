import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

/**
 * Minimal axe-core smoke (#546).
 *
 * Surfaces:
 * - `/` login shell (preview without backend)
 * - `/__design__` design gallery (requires metapi_design_gallery=1)
 *
 * Gate: fail only on impact serious|critical after allowlist filtering.
 * Moderate/minor are logged but do not fail CI.
 *
 * Allowlist (gallery only — documented residuals, not product fixes in #546):
 * - aria-hidden-focus: static drawer/modal mocks keep focusable form controls
 *   while the mock chrome is aria-hidden (demo only; not a real open overlay).
 * - color-contrast: shell/gallery token contrast residuals (checklist follow-ups;
 *   do not redesign DesignSystemGallery in this smoke issue).
 *
 * Login surface has no allowlist.
 */

const GALLERY_PATH = '/__design__';

/** Rule IDs allowed only on the design gallery surface. */
const GALLERY_ALLOWLIST_RULE_IDS = new Set(['aria-hidden-focus', 'color-contrast']);

type AxeImpact = 'minor' | 'moderate' | 'serious' | 'critical';

function formatViolations(
  violations: Array<{
    id: string;
    impact?: string | null;
    help: string;
    nodes: Array<{ target: unknown[]; failureSummary?: string }>;
  }>,
): string {
  return violations
    .map((v) => {
      const targets = v.nodes
        .slice(0, 5)
        .map((n) => `    - ${JSON.stringify(n.target)}${n.failureSummary ? `: ${n.failureSummary}` : ''}`)
        .join('\n');
      return `[${v.impact ?? 'unknown'}] ${v.id}: ${v.help}\n${targets}`;
    })
    .join('\n\n');
}

async function runSeriousCriticalAxe(
  page: Page,
  label: string,
  allowlist: ReadonlySet<string> = new Set(),
) {
  const results = await new AxeBuilder({ page })
    // WCAG 2.1 A/AA is enough for a smoke gate.
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();

  const seriousOrCritical = results.violations.filter((v) => {
    const impact = (v.impact ?? '') as AxeImpact | '';
    return impact === 'serious' || impact === 'critical';
  });

  const allowlisted = seriousOrCritical.filter((v) => allowlist.has(v.id));
  const blocking = seriousOrCritical.filter((v) => !allowlist.has(v.id));

  const soft = results.violations.filter((v) => {
    const impact = (v.impact ?? '') as AxeImpact | '';
    return impact === 'moderate' || impact === 'minor';
  });

  if (allowlisted.length > 0) {
    // eslint-disable-next-line no-console
    console.warn(
      `[a11y-smoke] ${label}: ${allowlisted.length} allowlisted serious/critical rule(s):\n${formatViolations(allowlisted)}`,
    );
  }

  if (soft.length > 0) {
    // eslint-disable-next-line no-console
    console.warn(
      `[a11y-smoke] ${label}: ${soft.length} moderate/minor violation(s) (not failing):\n${formatViolations(soft)}`,
    );
  }

  expect(
    blocking,
    `[a11y-smoke] ${label}: serious/critical violations:\n${formatViolations(blocking)}`,
  ).toEqual([]);
}

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
    const bodyText = (await page.locator('body').innerText().catch(() => '')).toLowerCase();
    if (bodyText.includes('design system') || bodyText.includes('design gallery')) {
      return true;
    }
    return false;
  }
}

test.describe('a11y smoke (axe)', () => {
  test('login surface has no serious/critical axe violations', async ({ page }) => {
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

    await expect(page.locator('#root')).toBeAttached({ timeout: 15_000 });
    // Give React a beat to paint the login shell before axe scans.
    await page.waitForTimeout(300);

    await runSeriousCriticalAxe(page, 'login /');
  });

  test('design gallery has no serious/critical axe violations (allowlisted residuals)', async ({
    page,
  }) => {
    await page.addInitScript(() => {
      try {
        localStorage.setItem('theme_mode', 'light');
        localStorage.setItem('theme', 'light');
        // Production preview builds gate /__design__ behind this flag (#533).
        localStorage.setItem('metapi_design_gallery', '1');
      } catch {
        /* ignore */
      }
    });

    const available = await galleryAvailable(page);
    test.skip(!available, 'design gallery route /__design__ not available (404 or not scaffolded)');

    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light', {
      timeout: 15_000,
    });
    await page.waitForTimeout(300);

    await runSeriousCriticalAxe(page, 'gallery /__design__', GALLERY_ALLOWLIST_RULE_IDS);
  });
});
