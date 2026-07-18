import { chromium } from '@playwright/test';
import path from 'path';
import { spawn } from 'child_process';
import http from 'http';
import fs from 'fs';

const root = process.cwd();
const outDir = path.resolve(root, '../docs/analysis/ui-shots');
const port = 4181;
const base = `http://127.0.0.1:${port}`;
const wait = (ms) => new Promise((r) => setTimeout(r, ms));

fs.mkdirSync(outDir, { recursive: true });

async function ready() {
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
  throw new Error('preview not ready');
}

const preview = spawn(
  'npx',
  ['vite', 'preview', '--host', '127.0.0.1', '--port', String(port), '--strictPort'],
  { cwd: root, stdio: 'pipe', shell: true },
);
preview.stdout.on('data', (d) => process.stdout.write(d));
preview.stderr.on('data', (d) => process.stderr.write(d));

try {
  await ready();
  const browser = await chromium.launch();

  for (const mode of ['light', 'dark']) {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    await page.addInitScript((themeMode) => {
      localStorage.setItem('theme_mode', themeMode);
      localStorage.setItem('theme', themeMode);
      localStorage.setItem('metapi_design_gallery', '1');
    }, mode);

    await page.goto(`${base}/`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(500);
    const loginPath = path.join(outDir, `login-${mode}-win32.png`);
    await page.screenshot({ path: loginPath, fullPage: false });
    console.log('wrote', loginPath);

    await page.goto(`${base}/__design__`, { waitUntil: 'networkidle' });
    await page.waitForTimeout(500);
    const galleryPath = path.join(outDir, `gallery-${mode}-win32.png`);
    await page.screenshot({ path: galleryPath, fullPage: true });
    console.log('wrote', galleryPath);

    await page.close();
  }

  await browser.close();
} finally {
  preview.kill('SIGTERM');
}
