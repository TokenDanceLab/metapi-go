import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

/**
 * Shell chrome mock must list the same 14 product nav labels as App.sidebarGroups.
 * Truncated mock nav was misread as "product sidebar deleted" in UI review.
 */
const requiredLabels = [
  '仪表盘',
  '站点管理',
  '站点公告',
  '连接管理',
  'OAuth 管理',
  '下游密钥',
  '签到记录',
  '路由',
  '使用日志',
  '可用性监控',
  '设置',
  '程序日志',
  '导入/导出',
  '通知设置',
] as const;

describe('DesignSystemGallery shell mock nav', () => {
  it('mirrors full production sidebar labels (14 items)', () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const source = readFileSync(join(here, 'DesignSystemGallery.tsx'), 'utf8');
    const shellStart = source.indexOf('function ShellChromeMock');
    expect(shellStart).toBeGreaterThan(-1);
    const shellSlice = source.slice(shellStart, shellStart + 12_000);
    for (const label of requiredLabels) {
      expect(shellSlice, `missing shell nav label: ${label}`).toContain(`label: '${label}'`);
    }
    expect(shellSlice).toContain('模型操练场');
  });
});
