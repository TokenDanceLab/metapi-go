import { act, create, type ReactTestRenderer, type TestRendererOptions } from 'react-test-renderer';
import type { ReactElement } from 'react';

/**
 * Shared React 19 RTR helper: create + act so concurrent updates flush before
 * assertions. Prefer this for new suites; vitest.setup also patches create().
 */
export async function renderWithAct(
  element: ReactElement,
  options?: TestRendererOptions,
): Promise<ReactTestRenderer> {
  let renderer!: ReactTestRenderer;
  await act(async () => {
    renderer = create(element, options);
  });
  return renderer;
}

export async function updateWithAct(
  renderer: ReactTestRenderer,
  element: ReactElement,
): Promise<void> {
  await act(async () => {
    renderer.update(element);
  });
}

export async function unmountWithAct(renderer: ReactTestRenderer): Promise<void> {
  await act(async () => {
    renderer.unmount();
  });
}
