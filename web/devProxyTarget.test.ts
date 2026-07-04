import { describe, expect, it } from 'vitest';
import { resolveDevProxyTarget } from './devProxyTarget.js';

describe('resolveDevProxyTarget', () => {
  it('falls back to localhost:4000 when no env is provided', () => {
    expect(resolveDevProxyTarget({})).toBe('http://localhost:4000');
  });

  it('prefers explicit backend target env var', () => {
    expect(resolveDevProxyTarget({ DEV_PROXY_TARGET: 'http://127.0.0.1:4567' })).toBe('http://127.0.0.1:4567');
  });

  it('uses PORT env when provided', () => {
    expect(resolveDevProxyTarget({ PORT: '4100' })).toBe('http://localhost:4100');
  });
});
