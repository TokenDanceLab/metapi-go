import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { MemoryRouter } from 'react-router-dom';

import About from './About.js';

const { apiMock } = vi.hoisted(() => ({
  apiMock: {
    getUpdateCenterStatus: vi.fn(),
  },
}));

vi.mock('../api.js', () => ({
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

describe('About update center (UC-1 external)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiMock.getUpdateCenterStatus.mockResolvedValue({
      currentVersion: '0.0.0',
      latestVersion: '0.0.0',
      updateAvailable: false,
      mode: 'external',
      residual: 'external deploy only',
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('does not invent a new-version theater from local residual stub', async () => {
    let root!: ReactTestRenderer;
    await act(async () => {
      root = create(
        <MemoryRouter>
          <About />
        </MemoryRouter>,
      );
    });
    await flushMicrotasks();

    const text = collectText(root.root);
    expect(text).toContain('更新与部署');
    expect(text).toContain('设置 · 更新与部署说明');
    expect(text).not.toContain('发现新版本');
    expect(text).not.toContain('前往更新中心');
    expect(apiMock.getUpdateCenterStatus).toHaveBeenCalled();
  });
});
