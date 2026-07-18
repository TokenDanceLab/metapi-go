/**
 * Downstream key per-key egress proxy helpers (KEY-578 / #466).
 * Empty/null = inherit site/account/system proxy chain.
 */

const SUPPORTED_PROXY_SCHEMES = [
  'http://',
  'https://',
  'socks://',
  'socks4://',
  'socks4a://',
  'socks5://',
  'socks5h://',
] as const;

export function normalizeDownstreamKeyProxyUrl(raw: string | null | undefined): string {
  return String(raw ?? '').trim();
}

export function hasCustomDownstreamKeyProxy(value?: string | null): boolean {
  return normalizeDownstreamKeyProxyUrl(value).length > 0;
}

/**
 * Client-side validate + normalize for create/update payloads.
 * Empty/whitespace → null (inherit). Non-empty must use a supported scheme.
 */
export function parseDownstreamKeyProxyUrl(raw: string | null | undefined): {
  valid: boolean;
  value: string | null;
  error?: string;
} {
  const trimmed = normalizeDownstreamKeyProxyUrl(raw);
  if (!trimmed) {
    return { valid: true, value: null };
  }
  const lower = trimmed.toLowerCase();
  const supported = SUPPORTED_PROXY_SCHEMES.some((scheme) => lower.startsWith(scheme));
  if (!supported) {
    return {
      valid: false,
      value: null,
      error: '代理地址必须以 http://、https:// 或 socks 代理 scheme 开头',
    };
  }
  return { valid: true, value: trimmed };
}

/**
 * Compact list/detail indicator when a custom proxy is set.
 * Redacts userinfo; prefers `hostname:port`, falls back to `已设置`.
 * Returns null when inheriting (empty/null).
 */
export function formatDownstreamKeyProxyIndicator(value?: string | null): string | null {
  const text = normalizeDownstreamKeyProxyUrl(value);
  if (!text) return null;

  try {
    const parsed = new URL(text);
    const host = (parsed.hostname || '').trim();
    if (!host) return '已设置';
    const port = parsed.port ? `:${parsed.port}` : '';
    return `${host}${port}`;
  } catch {
    // Strip credentials-ish segments without relying on URL parser.
    const withoutUserinfo = text.replace(/\/\/[^/@\s]+@/, '//');
    const hostPortMatch = withoutUserinfo.match(/\/\/([^/?#]+)/);
    if (hostPortMatch?.[1]) {
      const candidate = hostPortMatch[1].trim();
      if (candidate && !candidate.includes('@')) return candidate;
    }
    return '已设置';
  }
}
