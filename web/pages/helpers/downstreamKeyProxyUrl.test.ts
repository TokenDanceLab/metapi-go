import { describe, expect, it } from 'vitest';
import {
  formatDownstreamKeyProxyIndicator,
  hasCustomDownstreamKeyProxy,
  normalizeDownstreamKeyProxyUrl,
  parseDownstreamKeyProxyUrl,
} from './downstreamKeyProxyUrl.js';

describe('downstreamKeyProxyUrl', () => {
  it('trims and treats empty as inherit', () => {
    expect(normalizeDownstreamKeyProxyUrl(null)).toBe('');
    expect(normalizeDownstreamKeyProxyUrl(undefined)).toBe('');
    expect(normalizeDownstreamKeyProxyUrl('  ')).toBe('');
    expect(normalizeDownstreamKeyProxyUrl('  http://127.0.0.1:7890  ')).toBe('http://127.0.0.1:7890');

    expect(parseDownstreamKeyProxyUrl('')).toEqual({ valid: true, value: null });
    expect(parseDownstreamKeyProxyUrl('   ')).toEqual({ valid: true, value: null });
    expect(parseDownstreamKeyProxyUrl(null)).toEqual({ valid: true, value: null });
    expect(hasCustomDownstreamKeyProxy(null)).toBe(false);
    expect(hasCustomDownstreamKeyProxy('')).toBe(false);
    expect(hasCustomDownstreamKeyProxy('  ')).toBe(false);
  });

  it('accepts http/https/socks schemes and rejects others', () => {
    expect(parseDownstreamKeyProxyUrl('http://proxy.example:8080')).toEqual({
      valid: true,
      value: 'http://proxy.example:8080',
    });
    expect(parseDownstreamKeyProxyUrl('HTTPS://proxy.example:443')).toEqual({
      valid: true,
      value: 'HTTPS://proxy.example:443',
    });
    expect(parseDownstreamKeyProxyUrl('socks5://127.0.0.1:1080')).toEqual({
      valid: true,
      value: 'socks5://127.0.0.1:1080',
    });
    expect(parseDownstreamKeyProxyUrl('socks5h://user:pass@10.0.0.1:1080')).toEqual({
      valid: true,
      value: 'socks5h://user:pass@10.0.0.1:1080',
    });
    expect(parseDownstreamKeyProxyUrl('ftp://bad.example')).toEqual({
      valid: false,
      value: null,
      error: '代理地址必须以 http://、https:// 或 socks 代理 scheme 开头',
    });
    expect(parseDownstreamKeyProxyUrl('proxy.example:8080')).toEqual({
      valid: false,
      value: null,
      error: '代理地址必须以 http://、https:// 或 socks 代理 scheme 开头',
    });
    expect(hasCustomDownstreamKeyProxy('http://127.0.0.1:7890')).toBe(true);
  });

  it('formats compact indicator and redacts userinfo passwords', () => {
    expect(formatDownstreamKeyProxyIndicator(null)).toBeNull();
    expect(formatDownstreamKeyProxyIndicator('')).toBeNull();
    expect(formatDownstreamKeyProxyIndicator('http://127.0.0.1:7890')).toBe('127.0.0.1:7890');
    expect(formatDownstreamKeyProxyIndicator('socks5://proxy.example.com:1080')).toBe('proxy.example.com:1080');
    expect(formatDownstreamKeyProxyIndicator('http://user:s3cret@proxy.example.com:8080')).toBe('proxy.example.com:8080');
    expect(formatDownstreamKeyProxyIndicator('socks5h://alice:hunter2@10.0.0.9:1080')).toBe('10.0.0.9:1080');
    // Unparseable values still must not leak credentials.
    expect(formatDownstreamKeyProxyIndicator('not-a-url://user:pass@host')).toBe('host');
    expect(formatDownstreamKeyProxyIndicator('not-a-url://user:pass@host')).not.toContain('pass');
    expect(formatDownstreamKeyProxyIndicator('garbage')).toBe('已设置');
  });
});
