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
    expect(head).toContain('#0b0f14');
    expect(head).toContain('#f4f6f8');
    expect(head).toMatch(/<meta\s+name=["']color-scheme["']\s+content=["']light dark["']\s*\/?>/i);
  });

  it('places FOUC bootstrap before Google Fonts stylesheet', () => {
    const scriptIdx = indexHtml.indexOf("localStorage.getItem('theme_mode')");
    const fontsIdx = indexHtml.indexOf('fonts.googleapis.com/css2');
    expect(scriptIdx).toBeGreaterThan(-1);
    expect(fontsIdx).toBeGreaterThan(-1);
    expect(scriptIdx).toBeLessThan(fontsIdx);
  });
});
