import { describe, expect, it } from 'vitest';
import { getAccountsAddPanelStyle } from './accountsPanelStyle.js';

describe('getAccountsAddPanelStyle', () => {
  it('preserves spacing and raises stacking context so select dropdown is clickable', () => {
    const style = getAccountsAddPanelStyle();

    expect(style.padding).toBe(20);
    expect(style.marginBottom).toBe(16);
    expect(style.position).toBe('relative');
    expect(style.zIndex).toBeGreaterThan(0);
    expect(style.overflow).toBe('visible');
  });
});
