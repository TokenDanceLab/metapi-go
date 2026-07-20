/**
 * Theme FOUC bootstrap helpers (#535).
 * Pure functions shared by unit tests; index.html inlines the same resolution rules.
 */

export const THEME_MODE_KEY = 'theme_mode';
export const LEGACY_THEME_KEY = 'theme';

export type ThemeMode = 'system' | 'light' | 'dark';
export type DataTheme = 'light' | 'dark';

export type ThemeStorageGetItem = (key: string) => string | null;

/** cloud-ops canvas (tokendance-design/styles/cloud-ops) — keep FOUC in sync with tokens.css */
const CANVAS_BG_LIGHT = '#f8f9fa';
const CANVAS_BG_DARK = '#202124';

function isDataTheme(value: string | null | undefined): value is DataTheme {
  return value === 'light' || value === 'dark';
}

/**
 * Resolve the initial `data-theme` value before React hydrates.
 * Priority: theme_mode (light|dark) → theme_mode=system + prefersDark → legacy theme → prefersDark.
 */
export function resolveInitialDataTheme(
  getItem: ThemeStorageGetItem,
  prefersDark: boolean,
): DataTheme {
  const mode = getItem(THEME_MODE_KEY);
  if (isDataTheme(mode)) return mode;
  if (mode === 'system') return prefersDark ? 'dark' : 'light';

  const legacy = getItem(LEGACY_THEME_KEY);
  if (isDataTheme(legacy)) return legacy;

  return prefersDark ? 'dark' : 'light';
}

/** Solid canvas color applied before CSS tokens load (prevents white flash). */
export function canvasBackgroundForTheme(theme: DataTheme): string {
  return theme === 'dark' ? CANVAS_BG_DARK : CANVAS_BG_LIGHT;
}
