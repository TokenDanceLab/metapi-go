type ProxyEnv = Record<string, string | undefined>;

function normalizeTarget(raw: string): string {
  return raw.trim().replace(/\/+$/, '');
}

export function resolveDevProxyTarget(env: ProxyEnv): string {
  const explicit = normalizeTarget(env.DEV_PROXY_TARGET || env.VITE_DEV_PROXY_TARGET || '');
  if (explicit) return explicit;

  const port = (env.PORT || env.VITE_BACKEND_PORT || '').trim();
  if (port) return `http://localhost:${port}`;

  return 'http://localhost:4000';
}
