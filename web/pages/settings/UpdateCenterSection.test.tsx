import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { ToastProvider } from '../../components/Toast.js';

import UpdateCenterSection from './UpdateCenterSection.js';

const { apiMock } = vi.hoisted(() => ({
  apiMock: {
    getUpdateCenterStatus: vi.fn(),
  },
}));

vi.mock('../../api.js', () => ({
  api: apiMock,
}));

function collectText(node: ReactTestInstance): string {
  return (node.children || []).map((child) => {
    if (typeof child === 'string') return child;
    return collectText(child);
  }).join('');
}

async function flushMicrotasks() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe('UpdateCenterSection (UC-1 external)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.getUpdateCenterStatus.mockResolvedValue({
      currentVersion: '0.0.0',
      latestVersion: '0.0.0',
      updateAvailable: false,
      residual: 'external deploy only; no remote registry/helper polling or in-app version discovery',
      mode: 'external',
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders honesty card without deploy controls', async () => {
    let root!: ReactTestRenderer;
    await act(async () => {
      root = create(
        <ToastProvider>
          <UpdateCenterSection />
        </ToastProvider>,
      );
    });
    await flushMicrotasks();

    const text = collectText(root.root);
    expect(text).toContain('更新与部署');
    expect(text).toContain('外置部署');
    expect(text).toContain('GHCR');
    expect(text).toContain('GitHub Releases');
    expect(text).toContain('external deploy only');
    expect(text).not.toContain('启用更新中心');
    expect(text).not.toContain('检查更新');
    expect(text).not.toContain('保存更新中心配置');
    expect(text).not.toContain('helperBaseUrl');
    expect(apiMock.getUpdateCenterStatus).toHaveBeenCalled();
  });
});
