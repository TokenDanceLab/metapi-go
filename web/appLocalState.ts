import { clearAuthSession } from './authSession.js';

export const THEME_MODE_STORAGE_KEY = 'theme_mode';
export const LEGACY_THEME_STORAGE_KEY = 'theme';
export const USER_PROFILE_STORAGE_KEY = 'user_profile';
export const FIRST_USE_DOC_REMINDER_KEY = 'metapi_first_use_docs_reminder_seen_v1';

type StorageLike = {
  getItem?: (key: string) => string | null;
  setItem?: (key: string, value: string) => void;
  removeItem: (key: string) => void;
};

function resolveStorage(storage?: StorageLike | null): StorageLike | null {
  if (storage) return storage;
  if (typeof localStorage !== 'undefined') return localStorage;
  return null;
}

export function clearAppInstallationState(storage?: StorageLike | null): void {
  const target = resolveStorage(storage);
  if (!target) return;
  clearAuthSession(target as never);
  target.removeItem(THEME_MODE_STORAGE_KEY);
  target.removeItem(LEGACY_THEME_STORAGE_KEY);
  target.removeItem(USER_PROFILE_STORAGE_KEY);
  target.removeItem(FIRST_USE_DOC_REMINDER_KEY);
}
