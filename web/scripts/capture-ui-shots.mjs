/**
 * Capture UI acceptance screenshots into docs/analysis/ui-shots/ (#534 / #538).
 *
 * Always captures:
 *   - login-{light|dark}-win32.png
 *   - gallery-{light|dark}-win32.png  (full design gallery)
 *   - shell-{dashboard|sites|settings}-{light|dark}-win32.png  (gallery shell mock)
 *
 * Optional real shell pages (need a valid admin token against a live API proxy):
 *   METAPI_UI_AUTH_TOKEN=<bearer>
 *   METAPI_UI_AUTH_COOKIE=<cookie header value>   # optional extra Cookie header
 *   METAPI_UI_SHOT_BASE=http://127.0.0.1:3000    # optional; skip local vite preview
 *
 * Auth storage keys match web/authSession.ts:
 *   localStorage.auth_token + auth_token_expires_at
 *
 * Run from web/:
 *   node scripts/capture-ui-shots.mjs
 *   METAPI_UI_AUTH_TOKEN=... node scripts/capture-ui-shots.mjs
 */
import { chromium } from '@playwright/test';
import path from 'path';
import { spawn } from 'child_process';
import http from 'http';
import fs from 'fs';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(__dirname, '..');
const outDir = path.resolve(root, '../docs/analysis/ui-shots');
const previewPort = Number(process.env.METAPI_UI_SHOT_PORT || 4181);
const externalBase = (process.env.METAPI_UI_SHOT_BASE || '').trim().replace(/\/$/, '');
const authToken = (process.env.METAPI_UI_AUTH_TOKEN || process.env.METAPI_AUTH_TOKEN || '').trim();
const authCookie = (process.env.METAPI_UI_AUTH_COOKIE || '').trim();
const modes = ['light', 'dark'];
const shellPages = [
  { id: 'dashboard', label: 'Dashboard' },
  { id: 'sites', label: 'Sites' },
  { id: 'settings', label: 'Settings' },
];
const realRoutes = [
  { id: 'dashboard', path: '/', wait: '[class*="page"], .main-content, .greeting, .page-title' },
  { id: 'sites', path: '/sites', wait: '.page-title, .data-table, .main-content' },
  { id: 'settings', path: '/settings', wait: '.page-title, .main-content' },
];

const wait = (ms) => new Promise((r) => setTimeout(r, ms));

fs.mkdirSync(outDir, { recursive: true });

function platformTag() {
  if (process.platform === 'win32') return 'win32';
  if (process.platform === 'darwin') return 'darwin';
  return 'linux';
}

const plat = platformTag();

async function ready(base) {
  for (let i = 0; i < 90; i++) {
    try {
      await new Promise((resolve, reject) => {
        const req = http.get(base, (res) => {
          res.resume();
          resolve();
        });
        req.on('error', reject);
        req.setTimeout(1000, () => req.destroy(new Error('timeout')));
      });
      return;
    } catch {
      await wait(500);
    }
  }
  throw new Error(`preview not ready: ${base}`);
}

async function applyTheme(page, mode) {
  await page.addInitScript((themeMode) => {
    try {
      localStorage.setItem('theme_mode', themeMode);
      localStorage.setItem('theme', themeMode);
      localStorage.setItem('metapi_design_gallery', '1');
    } catch {
      /* ignore */
    }
  }, mode);
}

async function applyAuth(page) {
  if (!authToken && !authCookie) return;
  const expiresAt = Date.now() + 12 * 60 * 60 * 1000;
  await page.addInitScript(
    ({ token, expires }) => {
      try {
        if (token) {
          localStorage.setItem('auth_token', token);
          localStorage.setItem('auth_token_expires_at', String(expires));
        }
      } catch {
        /* ignore */
      }
    },
    { token: authToken, expires: expiresAt },
  );
  if (authCookie) {
    // Cookie string form: "name=value; other=val" — set via context when base known later.
  }
}

async function shot(page, name) {
  const file = path.join(outDir, `${name}-${plat}.png`);
  await page.screenshot({ path: file, fullPage: false });
  console.log('wrote', file);
  return file;
}

async function captureShellMocks(page, base, mode) {
  await page.goto(`${base}/__design__`, { waitUntil: 'networkidle' });
  await page.waitForSelector('[data-testid="shell-chrome-mock"]', { timeout: 15_000 });
  await page.waitForTimeout(300);

  for (const pageDef of shellPages) {
    const btn = page.getByRole('button', { name: pageDef.label, exact: true }).first();
    if (await btn.count()) {
      await btn.click();
    } else {
      // Fallback: sidebar label (zh) or data attribute switch via evaluate.
      await page.evaluate((id) => {
        const root = document.querySelector('[data-testid="shell-chrome-mock"]');
        if (!root) return;
        const map = { dashboard: '仪表盘', sites: '站点管理', settings: '设置' };
        const label = map[id];
        const candidate = Array.from(document.querySelectorAll('.sidebar-item, button'))
          .find((el) => (el.textContent || '').trim() === label);
        if (candidate instanceof HTMLElement) candidate.click();
      }, pageDef.id);
    }
    await page.waitForSelector(`[data-testid="shell-chrome-mock"][data-shell-page="${pageDef.id}"]`, {
      timeout: 8_000,
    }).catch(() => {});
    await page.waitForTimeout(250);
    const shell = page.locator('[data-testid="shell-chrome-mock"]');
    const file = path.join(outDir, `shell-${pageDef.id}-${mode}-${plat}.png`);
    await shell.screenshot({ path: file });
    console.log('wrote', file);
  }
}

async function captureRealPages(page, base, mode) {
  if (!authToken) {
    console.log('skip real shell pages: set METAPI_UI_AUTH_TOKEN to capture / /sites /settings');
    return;
  }

  for (const route of realRoutes) {
    await page.goto(`${base}${route.path}`, { waitUntil: 'networkidle' });
    // If auth failed we land on login; detect and skip with a note.
    const onLogin = await page.locator('.login-shell, [data-testid="login-form"], input[type="password"]').first()
      .isVisible()
      .catch(() => false);
    if (onLogin) {
      console.warn(`skip real ${route.id}: still on login (token rejected or API unavailable)`);
      continue;
    }
    await page.waitForSelector(route.wait, { timeout: 12_000 }).catch(() => {});
    await page.waitForTimeout(400);
    await shot(page, `page-${route.id}-${mode}`);
  }
}

let preview = null;
let base = externalBase || `http://127.0.0.1:${previewPort}`;

try {
  if (!externalBase) {
    preview = spawn(
      'npx',
      ['vite', 'preview', '--host', '127.0.0.1', '--port', String(previewPort), '--strictPort'],
      { cwd: root, stdio: 'pipe', shell: true },
    );
    preview.stdout.on('data', (d) => process.stdout.write(d));
    preview.stderr.on('data', (d) => process.stderr.write(d));
    await ready(base);
  }

  const browser = await chromium.launch();
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
  });

  if (authCookie) {
    // Best-effort: parse simple "a=b; c=d" into Playwright cookies for the base host.
    try {
      const url = new URL(base);
      const cookies = authCookie.split(';').map((part) => {
        const [name, ...rest] = part.trim().split('=');
        return {
          name: name.trim(),
          value: rest.join('=').trim(),
          domain: url.hostname,
          path: '/',
        };
      }).filter((c) => c.name && c.value);
      if (cookies.length) await context.addCookies(cookies);
    } catch (err) {
      console.warn('failed to apply METAPI_UI_AUTH_COOKIE:', err?.message || err);
    }
  }

  for (const mode of modes) {
    const page = await context.newPage();
    await applyTheme(page, mode);
    await applyAuth(page);

    await page.goto(`${base}/`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(500);
    // When authed, root is dashboard not login — still write under login only if login visible.
    const loginVisible = await page.locator('.login-shell, .login-surface').first().isVisible().catch(() => false);
    if (loginVisible) {
      await shot(page, `login-${mode}`);
    } else {
      console.log(`skip login-${mode}: already authed (token present)`);
    }

    await page.goto(`${base}/__design__`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(500);
    const galleryPath = path.join(outDir, `gallery-${mode}-${plat}.png`);
    await page.screenshot({ path: galleryPath, fullPage: true });
    console.log('wrote', galleryPath);

    await captureShellMocks(page, base, mode);
    await captureRealPages(page, base, mode);

    await page.close();
  }

  await browser.close();
} finally {
  if (preview) {
    preview.kill('SIGTERM');
  }
}
