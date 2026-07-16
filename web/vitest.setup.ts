import { vi } from 'vitest';
import type { ReactElement, ReactNode } from 'react';
import type {
  ReactTestRenderer,
  TestRendererOptions,
} from 'react-test-renderer';

// React 19 concurrent mode needs an act-enabled environment; without it RTR
// trees unmount before tests can read `.root`.
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// react-test-renderer cannot host ReactDOM portals. Under jsdom, production
// portal helpers would otherwise mix renderers and crash with
// `parentInstance.children.indexOf is not a function`. Keep portal children
// inline in the RTR tree so existing component assertions keep working.
vi.mock('react-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-dom')>();
  return {
    ...actual,
    createPortal: ((children: ReactNode) => children) as typeof actual.createPortal,
  };
});

// Auto-wrap create() in act so effects/state flush before assertions. Existing
// suites call create() without act; React 19 otherwise unmounts immediately.
// ESM exports are not assignable, so mock the module instead of mutating it.
vi.mock('react-test-renderer', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-test-renderer')>();
  const { act, create: originalCreate } = actual;

  function createWithAct(
    element: ReactElement,
    options?: TestRendererOptions,
  ): ReactTestRenderer {
    let renderer!: ReactTestRenderer;
    act(() => {
      renderer = originalCreate(element, options);
    });
    return renderer;
  }

  const actualRecord = actual as typeof actual & {
    default?: { create?: typeof actual.create };
  };
  const patched = {
    ...actual,
    create: createWithAct,
  };

  if (actualRecord.default && typeof actualRecord.default === 'object') {
    return {
      ...patched,
      default: {
        ...actualRecord.default,
        create: createWithAct,
      },
    };
  }

  return patched;
});


// Dashboard charts pull @visactor/* ESM that breaks under vitest/jsdom.
// Provide lightweight stubs so page tests can render without native chart deps.
vi.mock('@visactor/react-vchart', () => ({
  VChart: () => null,
  default: () => null,
}))
vi.mock('@visactor/vchart', () => ({
  default: class VChartStub {},
  VChart: class VChartStub {},
}))
