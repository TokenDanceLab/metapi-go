import { describe, expect, it } from 'vitest';
import { FOCUSABLE_SELECTOR, listFocusable } from './useFocusTrap.js';

describe('useFocusTrap helpers', () => {
  it('exports a stable focusable selector covering buttons and fields', () => {
    expect(FOCUSABLE_SELECTOR).toContain('button:not([disabled])');
    expect(FOCUSABLE_SELECTOR).toContain('input:not([disabled])');
    expect(FOCUSABLE_SELECTOR).toContain('[tabindex]');
  });

  it('lists only enabled, visible-ish focusables in document order', () => {
    document.body.innerHTML = `
      <div id="root">
        <button type="button">one</button>
        <button type="button" disabled>skip</button>
        <input type="text" value="two" />
        <input type="hidden" value="hidden" />
        <a href="#x">three</a>
        <div tabindex="-1">not tabbable</div>
      </div>
    `;
    const root = document.getElementById('root')!;
    const items = listFocusable(root);
    expect(items.map((el) => el.textContent || (el as HTMLInputElement).value)).toEqual([
      'one',
      'two',
      'three',
    ]);
  });
});
