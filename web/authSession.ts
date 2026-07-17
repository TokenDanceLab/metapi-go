const AUTH_TOKEN_STORAGE_KEY = 'auth_token';
const AUTH_TOKEN_EXPIRES_AT_STORAGE_KEY = 'auth_token_expires_at';
export const AUTH_SESSION_DURATION_MS = 12 * 60 * 60 * 1000;

/** Must match handler/admin/monitor.go monitorAuthCookie. */
export const MONITOR_AUTH_COOKIE_NAME = 'meta_monitor_auth';

/**
 * Must match handler/admin/monitor.go monitorCookiePath (#407).
 * document.cookie can only clear non-HttpOnly cookies; HttpOnly is cleared
 * via DELETE /api/monitor/session. This helper is a best-effort residual
 * clear for any non-HttpOnly twin and documents the Path contract in tests.
 */
export const MONITOR_AUTH_COOKIE_PATH = '/monitor-proxy/';

type StorageLike = {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
  removeItem: (key: string) => void;
};

type CookieWriter = {
  /** Assign to document.cookie (or a test double). */
  cookie?: string;
};

function resolveStorage(storage?: StorageLike | null): StorageLike | null {
  if (storage) return storage;
  if (typeof localStorage !== 'undefined') return localStorage;
  return null;
}

/**
 * Best-effort document.cookie clear for meta_monitor_auth.
 * Path must match createSession (/monitor-proxy/) or the browser ignores it.
 * Also clears legacy Path=/ in case an older mint used it.
 * Prefer DELETE /api/monitor/session for the HttpOnly cookie.
 */
export function clearMonitorAuthCookie(
  doc: CookieWriter | null | undefined = typeof document !== 'undefined' ? document : null,
): void {
  if (!doc) return;
  try {
    const expire = 'Max-Age=0; expires=Thu, 01 Jan 1970 00:00:00 GMT';
    doc.cookie = `${MONITOR_AUTH_COOKIE_NAME}=; Path=${MONITOR_AUTH_COOKIE_PATH}; ${expire}`;
    // Legacy Path=/ residual from pre-#407.
    doc.cookie = `${MONITOR_AUTH_COOKIE_NAME}=; Path=/; ${expire}`;
  } catch {
    // document may be restricted (sandboxed); ignore.
  }
}

export function clearAuthSession(storage?: StorageLike | null): void {
  const target = resolveStorage(storage);
  if (!target) return;
  target.removeItem(AUTH_TOKEN_STORAGE_KEY);
  target.removeItem(AUTH_TOKEN_EXPIRES_AT_STORAGE_KEY);
  // Residual non-HttpOnly clear; HttpOnly still needs backend DELETE.
  clearMonitorAuthCookie();
}

export function persistAuthSession(
  storage: StorageLike | null | undefined,
  token: string,
  ttlMs = AUTH_SESSION_DURATION_MS,
  nowMs = Date.now(),
): void {
  const target = resolveStorage(storage);
  if (!target) return;

  const cleanToken = (token || '').trim();
  if (!cleanToken) {
    clearAuthSession(target);
    return;
  }

  const expiresAt = nowMs + Math.max(1, Math.trunc(ttlMs));
  target.setItem(AUTH_TOKEN_STORAGE_KEY, cleanToken);
  target.setItem(AUTH_TOKEN_EXPIRES_AT_STORAGE_KEY, String(expiresAt));
}

export function getAuthToken(storage?: StorageLike | null, nowMs = Date.now()): string | null {
  const target = resolveStorage(storage);
  if (!target) return null;

  const token = (target.getItem(AUTH_TOKEN_STORAGE_KEY) || '').trim();
  if (!token) return null;

  const expiresAtRaw = target.getItem(AUTH_TOKEN_EXPIRES_AT_STORAGE_KEY);
  if (!expiresAtRaw) {
    // Legacy migration: set a default TTL the first time we read an old session.
    persistAuthSession(target, token, AUTH_SESSION_DURATION_MS, nowMs);
    return token;
  }

  const expiresAt = Number(expiresAtRaw);
  if (!Number.isFinite(expiresAt) || expiresAt <= nowMs) {
    clearAuthSession(target);
    return null;
  }

  return token;
}

export function hasValidAuthSession(storage?: StorageLike | null, nowMs = Date.now()): boolean {
  return !!getAuthToken(storage, nowMs);
}
