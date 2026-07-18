import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, create, type ReactTestInstance, type ReactTestRenderer } from 'react-test-renderer';
import { MemoryRouter } from 'react-router-dom';
import { ToastProvider } from '../components/Toast.js';
import TokenRoutes from './TokenRoutes.js';

const { apiMock, getBrandMock } = vi.hoisted(() => ({
  apiMock: {
    getRoutesSummary: vi.fn(),
    getRouteChannels: vi.fn(),
    getModelTokenCandidates: vi.fn(),
    getRouteDecisionsBatch: vi.fn(),
    getRouteWideDecisionsBatch: vi.fn(),
    updateRoute: vi.fn(),
    addRoute: vi.fn(),
  },
  getBrandMock: vi.fn(),
}));

vi.mock('../api.js', () => ({
  api: apiMock,
}));

vi.mock('../components/BrandIcon.js', () => ({
  BrandGlyph: ({ brand, icon, model }: { brand?: { name?: string } | null; icon?: string | null; model?: string | null }) => (
    <span>{brand?.name || icon || model || ''}</span>
  ),
  InlineBrandIcon: ({ model }: { model: string }) => model ? <span>{model}</span> : null,
  getBrand: (...args: unknown[]) => getBrandMock(...args),
  hashColor: () => 'linear-gradient(135deg,#4f46e5,#818cf8)',
  normalizeBrandIconKey: (icon: string) => icon,
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

describe('TokenRoutes contextLength hydrate/save/clear', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getBrandMock.mockReset();
    getBrandMock.mockReturnValue(null);
    apiMock.getRoutesSummary.mockResolvedValue([
      {
        id: 21,
        modelPattern: 'gpt-4o',
        displayName: 'gpt-4o group',
        displayIcon: null,
        routeMode: 'explicit_group',
        sourceRouteIds: [1],
        modelMapping: null,
        routingStrategy: 'weighted',
        contextLength: 128000,
        enabled: true,
        channelCount: 2,
        enabledChannelCount: 2,
        siteNames: ['site-a'],
        decisionSnapshot: null,
        decisionRefreshedAt: null,
      },
    ]);
    apiMock.getRouteChannels.mockResolvedValue([]);
    apiMock.getModelTokenCandidates.mockResolvedValue({ models: {} });
    apiMock.getRouteDecisionsBatch.mockResolvedValue({ decisions: {} });
    apiMock.getRouteWideDecisionsBatch.mockResolvedValue({ decisions: {} });
    apiMock.updateRoute.mockResolvedValue({});
    apiMock.addRoute.mockResolvedValue({});
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('hydrates contextLength into the editor and saves null when cleared', async () => {
    let root!: ReactTestRenderer;
    await act(async () => {
      root = create(
        <MemoryRouter initialEntries={['/routes']}>
          <ToastProvider>
            <TokenRoutes />
          </ToastProvider>
        </MemoryRouter>,
      );
    });
    await flushMicrotasks();

    // Expand so edit button is available (expanded header has 编辑群组 for group routes).
    const expandButton = root.root.find((node) => (
      node.type === 'div'
      && String(node.props.className || '').includes('route-card-collapsed')
    ));
    await act(async () => {
      expandButton.props.onClick();
    });
    await flushMicrotasks();

    const editButton = root.root.find((node) => (
      node.type === 'button'
      && collectText(node).includes('编辑群组')
    ));
    await act(async () => {
      editButton.props.onClick();
    });
    await flushMicrotasks();

    const input = root.root.find((node) => (
      node.type === 'input'
      && node.props['data-testid'] === 'route-context-length-input'
    ));
    expect(input.props.value).toBe('128000');

    await act(async () => {
      input.props.onChange({ target: { value: '' } });
    });
    await flushMicrotasks();

    const saveButton = root.root.find((node) => (
      node.type === 'button'
      && collectText(node).includes('保存群组')
    ));
    await act(async () => {
      saveButton.props.onClick();
    });
    await flushMicrotasks();

    expect(apiMock.updateRoute).toHaveBeenCalledWith(
      21,
      expect.objectContaining({
        contextLength: null,
      }),
    );
  });

  it('rejects non-integer contextLength before submit', async () => {
    let root!: ReactTestRenderer;
    await act(async () => {
      root = create(
        <MemoryRouter initialEntries={['/routes']}>
          <ToastProvider>
            <TokenRoutes />
          </ToastProvider>
        </MemoryRouter>,
      );
    });
    await flushMicrotasks();

    const expandButton = root.root.find((node) => (
      node.type === 'div'
      && String(node.props.className || '').includes('route-card-collapsed')
    ));
    await act(async () => {
      expandButton.props.onClick();
    });
    await flushMicrotasks();

    const editButton = root.root.find((node) => (
      node.type === 'button'
      && collectText(node).includes('编辑群组')
    ));
    await act(async () => {
      editButton.props.onClick();
    });
    await flushMicrotasks();

    const input = root.root.find((node) => (
      node.type === 'input'
      && node.props['data-testid'] === 'route-context-length-input'
    ));
    await act(async () => {
      input.props.onChange({ target: { value: '-12' } });
    });
    await flushMicrotasks();

    const saveButton = root.root.find((node) => (
      node.type === 'button'
      && collectText(node).includes('保存群组')
    ));
    await act(async () => {
      saveButton.props.onClick();
    });
    await flushMicrotasks();

    expect(apiMock.updateRoute).not.toHaveBeenCalled();
  });
});
