/**
 * #554 — Sites weight-formula banner is deferred until sites.length > 0.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { MemoryRouter } from 'react-router-dom';
import { ToastProvider } from '../components/Toast.js';
import Sites from './Sites.js';

const { apiMock } = vi.hoisted(() => ({
  apiMock: {
    getSites: vi.fn(),
  },
}));

vi.mock('../api.js', () => ({
  api: apiMock,
}));

function collectText(node: any): string {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  const children = node?.children || node?.props?.children;
  if (children == null) return '';
  if (Array.isArray(children)) return children.map(collectText).join('');
  return collectText(children);
}

async function flushMicrotasks() {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe('Sites weight banner (#554)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('hides weight formula banner when sites list is empty', async () => {
    apiMock.getSites.mockResolvedValue([]);

    let root!: ReactTestRenderer;
    try {
      await act(async () => {
        root = create(
          <MemoryRouter initialEntries={['/sites']}>
            <ToastProvider>
              <Sites />
            </ToastProvider>
          </MemoryRouter>,
        );
      });
      await flushMicrotasks();

      const serialized = JSON.stringify(root.toJSON());
      expect(serialized).toContain('暂无站点');
      expect(serialized).not.toContain('站点权重说明');
      expect(serialized).not.toContain('了解站点权重');
      expect(() => root.root.find((node) => node.props['data-testid'] === 'sites-weight-tip')).toThrow();
    } finally {
      root.unmount();
    }
  });

  it('shows collapsible 了解站点权重 after first site exists', async () => {
    apiMock.getSites.mockResolvedValue([
      {
        id: 1,
        name: 'Site A',
        url: 'https://a.example.com',
        platform: 'new-api',
        status: 'active',
        globalWeight: 1,
      },
    ]);

    let root!: ReactTestRenderer;
    try {
      await act(async () => {
        root = create(
          <MemoryRouter initialEntries={['/sites']}>
            <ToastProvider>
              <Sites />
            </ToastProvider>
          </MemoryRouter>,
        );
      });
      await flushMicrotasks();

      const tip = root.root.find((node) => node.props['data-testid'] === 'sites-weight-tip');
      expect(tip.type).toBe('details');
      const serialized = JSON.stringify(root.toJSON());
      expect(serialized).toContain('了解站点权重');
      expect(serialized).toContain('站点权重说明');
      expect(serialized).toContain('Site A');
    } finally {
      root.unmount();
    }
  });
});
