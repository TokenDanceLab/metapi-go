import { describe, expect, it, vi } from 'vitest';

vi.mock('../../components/BrandIcon.js', () => ({
  getBrand: () => null,
  normalizeBrandIconKey: (icon: string) => icon.trim().toLowerCase(),
}));

import {
  ROUTE_ICON_NONE_VALUE,
  contextLengthFormValue,
  formatRouteContextLength,
  normalizeRouteDisplayIconValue,
  parseRouteContextLength,
  resolveRouteIcon,
} from './utils.js';

describe('token route icon helpers', () => {
  it('preserves the explicit no-icon sentinel during normalization', () => {
    expect(normalizeRouteDisplayIconValue(ROUTE_ICON_NONE_VALUE)).toBe(ROUTE_ICON_NONE_VALUE);
  });

  it('treats the explicit no-icon sentinel as no icon', () => {
    expect(resolveRouteIcon({ displayIcon: ROUTE_ICON_NONE_VALUE })).toEqual({ kind: 'none' });
  });
});

describe('route contextLength helpers', () => {
  it('hydrates positive contextLength into form string and clears unknown values', () => {
    expect(contextLengthFormValue(128000)).toBe('128000');
    expect(contextLengthFormValue(null)).toBe('');
    expect(contextLengthFormValue(undefined)).toBe('');
    expect(contextLengthFormValue(0)).toBe('');
    expect(contextLengthFormValue(-1)).toBe('');
  });

  it('parses save payload: empty/0 → null, positive int accepted, rejects non-integer/negative', () => {
    expect(parseRouteContextLength('')).toEqual({ valid: true, value: null });
    expect(parseRouteContextLength('   ')).toEqual({ valid: true, value: null });
    expect(parseRouteContextLength('0')).toEqual({ valid: true, value: null });
    expect(parseRouteContextLength('128000')).toEqual({ valid: true, value: 128000 });
    expect(parseRouteContextLength('-1').valid).toBe(false);
    expect(parseRouteContextLength('1.5').valid).toBe(false);
    expect(parseRouteContextLength('abc').valid).toBe(false);
    expect(parseRouteContextLength('12e3').valid).toBe(false);
  });

  it('formats list/card labels only when set', () => {
    expect(formatRouteContextLength(null)).toBeNull();
    expect(formatRouteContextLength(0)).toBeNull();
    expect(formatRouteContextLength(undefined)).toBeNull();
    expect(formatRouteContextLength(128000)).toBe('128k');
    expect(formatRouteContextLength(200000)).toBe('200k');
    expect(formatRouteContextLength(8192)).toBe('8192');
  });
});
