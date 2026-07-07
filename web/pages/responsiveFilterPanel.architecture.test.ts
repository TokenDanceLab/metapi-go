import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const pages = [
  'pages/Accounts.tsx',
  'pages/CheckinLog.tsx',
  'pages/DownstreamKeys.tsx',
  'pages/Models.tsx',
  'pages/ProgramLogs.tsx',
  'pages/ProxyLogs.tsx',
  'pages/Sites.tsx',
  'pages/TokenRoutes.tsx',
  'pages/Tokens.tsx',
];

describe('ResponsiveFilterPanel adoption', () => {
  it('routes page-level filter sheets through the shared scaffold component', () => {
    for (const page of pages) {
      const source = readFileSync(resolve(process.cwd(), page), 'utf8').replace(/\r\n/g, '\n');

      expect(source, page).toMatch(/import\s+ResponsiveFilterPanel\s+from\s+['"]\.\.\/components\/ResponsiveFilterPanel\.js['"]/);
      expect(source, page).not.toContain("import MobileFilterSheet from '../components/MobileFilterSheet.js'");
      expect(source, page).not.toContain('<MobileFilterSheet');
    }
  });
});
