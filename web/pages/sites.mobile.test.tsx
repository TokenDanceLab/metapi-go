import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

describe('Sites mobile layout', () => {
  it('includes mobile-card usage in Sites page', () => {
    const source = readFileSync(resolve(process.cwd(), 'src/web/pages/Sites.tsx'), 'utf8');
    expect(source).toContain('mobile-card');
  });
});
