import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const here = dirname(fileURLToPath(import.meta.url));
const indexHtml = readFileSync(join(here, 'index.html'), 'utf8');

describe('FOUC bootstrap (index.html)', () => {
  it('includes a head-level theme_mode-first blocking script', () => {
    const headMatch = indexHtml.match(/<head[\s\S]*?<\/head>/i);
    expect(headMatch).not.toBeNull();
    const head = headMatch![0];

    expect(head).toContain("localStorage.getItem('theme_mode')");
    expect(head).toContain("localStorage.getItem('theme')");
    expect(head).toContain("prefers-color-scheme: dark");
    expect(head).toContain("setAttribute('data-theme'");
    expect(head).toContain('colorScheme');
    expect(head).toContain('#202124');
    expect(head).toContain('#f8f9fa');
    expect(head).toMatch(/<meta\s+name=["']color-scheme["']\s+content=["']light dark["']\s*\/?>/i);
  });

  it('places FOUC bootstrap early in head and does not load external font CDN', () => {
    const scriptIdx = indexHtml.indexOf("localStorage.getItem('theme_mode')");
    const headEnd = indexHtml.toLowerCase().indexOf('</head>');
    expect(scriptIdx).toBeGreaterThan(-1);
    expect(headEnd).toBeGreaterThan(scriptIdx);
    // System stack only — no Google Fonts / third-party font CDN
    expect(indexHtml).not.toMatch(/fonts\.googleapis\.com/i);
    expect(indexHtml).not.toMatch(/fonts\.gstatic\.com/i);
  });
});
