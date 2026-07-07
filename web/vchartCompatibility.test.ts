import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const chartFiles = [
  'components/charts/SiteDistributionChart.tsx',
  'components/charts/SiteTrendChart.tsx',
  'components/ModelAnalysisPanel.tsx',
];

describe('VChart compatibility guards', () => {
  it.each(chartFiles)('%s should not use function formatter syntax', (filePath) => {
    const content = readFileSync(resolve(process.cwd(), filePath), 'utf8');
    expect(content).not.toMatch(/formatter\s*:\s*\(/);
  });
});
