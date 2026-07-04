import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

describe('TooltipLayer component', () => {
  it('uses a portal-based fixed tooltip layer and is mounted by App', () => {
    const source = readFileSync(resolve(process.cwd(), 'src/web/components/TooltipLayer.tsx'), 'utf8');
    const appSource = readFileSync(resolve(process.cwd(), 'src/web/App.tsx'), 'utf8');

    expect(source).toContain('createPortal');
    expect(source).toContain('[data-tooltip]');
    expect(source).toContain("position: 'fixed'");
    expect(source).toContain('document.body.dataset.tooltipPortal');
    expect(appSource).toContain("import TooltipLayer from './components/TooltipLayer.js'");
    expect(appSource).toContain('<TooltipLayer />');
  });
});
