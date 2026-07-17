import { describe, expect, it } from 'vitest';
import {
  AUTH_SESSION_DURATION_MS,
  clearAuthSession,
  clearMonitorAuthCookie,
  getAuthToken,
  hasValidAuthSession,
  MONITOR_AUTH_COOKIE_NAME,
  MONITOR_AUTH_COOKIE_PATH,
  persistAuthSession,
} from './authSession.js';

function createMemoryStorage() {
  const store = new Map<string, string>();
  return {
    getItem(key: string) {
      return store.has(key) ? store.get(key)! : null;
    },
    setItem(key: string, value: string) {
      store.set(key, value);
    },
    removeItem(key: string) {
      store.delete(key);
    },
  };
}

describe('authSession', () => {
  it('persists token with expiration and reads it before expiry', () => {
    const storage = createMemoryStorage();
    persistAuthSession(storage, 'token-1', 60_000, 1_000);

    expect(getAuthToken(storage, 10_000)).toBe('token-1');
    expect(hasValidAuthSession(storage, 10_000)).toBe(true);
  });

  it('clears expired session automatically', () => {
    const storage = createMemoryStorage();
    persistAuthSession(storage, 'token-2', 60_000, 1_000);

    expect(getAuthToken(storage, 100_000)).toBeNull();
    expect(hasValidAuthSession(storage, 100_000)).toBe(false);
  });

  it('supports explicit logout', () => {
    const storage = createMemoryStorage();
    persistAuthSession(storage, 'token-3', AUTH_SESSION_DURATION_MS, 1_000);

    clearAuthSession(storage);

    expect(getAuthToken(storage, 2_000)).toBeNull();
  });

  it('clearMonitorAuthCookie expires meta_monitor_auth with Path=/monitor-proxy/', () => {
    const written: string[] = [];
    const doc = {
      set cookie(value: string) {
        written.push(value);
      },
      get cookie() {
        return written.join('; ');
      },
    };

    clearMonitorAuthCookie(doc);

    expect(written.length).toBe(2);
    expect(written[0]).toContain(`${MONITOR_AUTH_COOKIE_NAME}=`);
    expect(written[0]).toContain(`Path=${MONITOR_AUTH_COOKIE_PATH}`);
    expect(written[0]).toContain('Max-Age=0');
    expect(written[1]).toContain('Path=/');
    expect(written[1]).toContain('Max-Age=0');
    // Contract with handler/admin/monitor.go after #407.
    expect(MONITOR_AUTH_COOKIE_PATH).toBe('/monitor-proxy/');
  });
});
