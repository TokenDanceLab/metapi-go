import { defineConfig, devices } from '@playwright/test';

const previewHost = '127.0.0.1';
const previewPort = 4173;
const baseURL = `http://${previewHost}:${previewPort}`;

/**
 * Visual + UX e2e harness for MetAPI admin UI (#534 / #536).
 * Serves the production Vite build via `vite preview` so screenshots match
 * embedded static assets rather than HMR dev chrome.
 *
 * Pitfall: reuseExistingServer reuses whatever is already on :4173.
 * If a foreign server occupies that port, gallery/theme specs soft-skip or
 * fail with confusing diffs. Locally prefer a free :4173, or force a clean
 * preview with METAPI_PW_FORCE_SERVER=1 (disables reuse).
 */
const forceServer =
  process.env.METAPI_PW_FORCE_SERVER === '1'
  || process.env.METAPI_PW_FORCE_SERVER === 'true';

export default defineConfig({
  testDir: './e2e',
  outputDir: './test-results',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI
    ? [['list'], ['html', { open: 'never', outputFolder: 'playwright-report' }]]
    : [['list']],
  timeout: 60_000,
  expect: {
    timeout: 10_000,
    toHaveScreenshot: {
      // Allow minor AA / font hinting drift across CI runners.
      maxDiffPixelRatio: 0.02,
      animations: 'disabled',
    },
  },
  use: {
    baseURL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'off',
    colorScheme: 'light',
    locale: 'zh-CN',
    viewport: { width: 1280, height: 800 },
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: `npm run build:web && npx vite preview --host ${previewHost} --port ${previewPort} --strictPort`,
    url: baseURL,
    // CI always boots fresh. Locally reuse unless METAPI_PW_FORCE_SERVER=1.
    reuseExistingServer: !process.env.CI && !forceServer,
    timeout: 180_000,
  },
});
