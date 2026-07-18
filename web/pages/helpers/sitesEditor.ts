export type SiteCustomHeaderField = {
  key: string;
  value: string;
};

export type SiteApiEndpointField = {
  draftId?: string;
  url: string;
  enabled: boolean;
  cooldownUntil?: string | null;
  lastFailureReason?: string | null;
};

export type SiteForm = {
  name: string;
  url: string;
  externalCheckinUrl: string;
  platform: string;
  proxyUrl: string;
  useSystemProxy: boolean;
  apiEndpoints: SiteApiEndpointField[];
  customHeaders: SiteCustomHeaderField[];
  globalWeight: string;
  /** Site concurrent upstream cap; string for controlled input. 0 = unlimited. */
  maxConcurrency: string;
};

export type SiteEditorState =
  | { mode: 'add' }
  | { mode: 'edit'; editingSiteId: number };

export type SiteSavePayload = {
  name: string;
  url: string;
  externalCheckinUrl: string;
  platform: string;
  initializationPresetId?: string | null;
  proxyUrl: string;
  useSystemProxy: boolean;
  apiEndpoints: Array<{
    url: string;
    enabled: boolean;
    sortOrder: number;
  }>;
  customHeaders: string;
  globalWeight: number;
  /** Caps concurrent upstream calls for this site (0 = unlimited). */
  maxConcurrency: number;
  postRefreshProbeEnabled?: boolean;
  postRefreshProbeModel?: string;
  postRefreshProbeScope?: 'single' | 'all';
  postRefreshProbeLatencyThresholdMs?: number;
};

type SiteSaveAction =
  | { kind: 'add'; payload: SiteSavePayload }
  | { kind: 'update'; id: number; payload: SiteSavePayload };

export function emptySiteCustomHeader(): SiteCustomHeaderField {
  return { key: '', value: '' };
}

export function emptySiteApiEndpoint(): SiteApiEndpointField {
  return {
    url: '',
    enabled: true,
    cooldownUntil: null,
    lastFailureReason: null,
  };
}

function ensureSiteCustomHeaderRows(rows: SiteCustomHeaderField[]): SiteCustomHeaderField[] {
  return rows.length > 0 ? rows : [emptySiteCustomHeader()];
}

export function emptySiteForm(): SiteForm {
  return {
    name: '',
    url: '',
    externalCheckinUrl: '',
    platform: '',
    proxyUrl: '',
    useSystemProxy: false,
    apiEndpoints: [emptySiteApiEndpoint()],
    customHeaders: [emptySiteCustomHeader()],
    globalWeight: '1',
    maxConcurrency: '0',
  };
}

function ensureSiteApiEndpointRows(rows: SiteApiEndpointField[]): SiteApiEndpointField[] {
  return rows.length > 0 ? rows : [emptySiteApiEndpoint()];
}

function parseCustomHeadersForEditor(raw: unknown): SiteCustomHeaderField[] {
  if (typeof raw !== 'string') {
    return ensureSiteCustomHeaderRows([]);
  }
  const trimmed = raw.trim();
  if (!trimmed) {
    return ensureSiteCustomHeaderRows([]);
  }

  try {
    const parsed = JSON.parse(trimmed);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return ensureSiteCustomHeaderRows([]);
    }
    return ensureSiteCustomHeaderRows(
      Object.entries(parsed as Record<string, unknown>).map(([key, value]) => ({
        key,
        value: typeof value === 'string' ? value : String(value ?? ''),
      })),
    );
  } catch {
    return ensureSiteCustomHeaderRows([]);
  }
}

function parseApiEndpointsForEditor(raw: unknown): SiteApiEndpointField[] {
  if (!Array.isArray(raw)) {
    return ensureSiteApiEndpointRows([]);
  }

  const rows: SiteApiEndpointField[] = [];
  for (const item of raw) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) continue;
    const row = item as Record<string, unknown>;
    rows.push({
      url: typeof row.url === 'string' ? row.url : '',
      enabled: row.enabled !== false,
      cooldownUntil: typeof row.cooldownUntil === 'string' ? row.cooldownUntil : null,
      lastFailureReason: typeof row.lastFailureReason === 'string' ? row.lastFailureReason : null,
    });
  }
  return ensureSiteApiEndpointRows(rows);
}

export function siteFormFromSite(site: Partial<Omit<SiteForm, 'apiEndpoints' | 'customHeaders' | 'globalWeight' | 'maxConcurrency' | 'externalCheckinUrl' | 'proxyUrl' | 'useSystemProxy'>> & {
  externalCheckinUrl?: string | null;
  proxyUrl?: string | null;
  useSystemProxy?: boolean | null;
  apiEndpoints?: Array<{
    url?: string | null;
    enabled?: boolean | null;
    cooldownUntil?: string | null;
    lastFailureReason?: string | null;
  }> | null;
  customHeaders?: string | null;
  globalWeight?: number | string | null;
  maxConcurrency?: number | string | null;
}): SiteForm {
  const globalWeightRaw = Number(site.globalWeight);
  const globalWeight = Number.isFinite(globalWeightRaw) && globalWeightRaw > 0 ? String(globalWeightRaw) : '1';
  const maxConcurrencyRaw = Number(site.maxConcurrency);
  const maxConcurrency = Number.isFinite(maxConcurrencyRaw) && maxConcurrencyRaw >= 0
    ? String(Math.trunc(maxConcurrencyRaw))
    : '0';
  return {
    name: site.name ?? '',
    url: site.url ?? '',
    externalCheckinUrl: site.externalCheckinUrl ?? '',
    platform: site.platform ?? '',
    proxyUrl: site.proxyUrl ?? '',
    useSystemProxy: !!site.useSystemProxy,
    apiEndpoints: parseApiEndpointsForEditor(site.apiEndpoints),
    customHeaders: parseCustomHeadersForEditor(site.customHeaders),
    globalWeight,
    maxConcurrency,
  };
}

/**
 * Parse maxConcurrency from form input.
 * 0 = unlimited; rejects non-integers and negatives (backend also 400s negatives).
 */
export function parseSiteMaxConcurrency(raw: string): {
  valid: boolean;
  value: number;
  error?: string;
} {
  const trimmed = raw.trim();
  if (!trimmed) {
    return { valid: true, value: 0 };
  }
  if (!/^\d+$/.test(trimmed)) {
    return {
      valid: false,
      value: 0,
      error: '最大并发必须是非负整数（0 = 不限制）',
    };
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < 0) {
    return {
      valid: false,
      value: 0,
      error: '最大并发必须是非负整数（0 = 不限制）',
    };
  }
  return { valid: true, value: Math.trunc(value) };
}

/** Compact list/detail label: 0 → 不限制. */
export function formatSiteMaxConcurrency(value?: number | null): string {
  const n = Number(value);
  if (!Number.isFinite(n) || n <= 0) return '不限制';
  return String(Math.trunc(n));
}

// Keep this in sync with normalizeSiteApiEndpointBaseUrl in
// src/server/services/siteApiEndpointService.ts.
function normalizeSiteApiEndpointUrl(raw: string): string {
  const trimmed = raw.trim();
  if (!trimmed) return '';
  try {
    const parsed = new URL(trimmed);
    parsed.search = '';
    parsed.hash = '';
    return parsed.toString().replace(/\/+$/, '');
  } catch {
    return trimmed.replace(/\/+$/, '');
  }
}

export function serializeSiteCustomHeaders(fields: SiteCustomHeaderField[]): {
  valid: boolean;
  customHeaders: string;
  error?: string;
} {
  const headers: Record<string, string> = {};
  const seen = new Set<string>();

  for (const field of fields) {
    const key = field.key.trim();
    const value = field.value;
    const hasAnyInput = key.length > 0 || value.trim().length > 0;
    if (!hasAnyInput) continue;
    if (!key) {
      return { valid: false, customHeaders: '', error: '请求头名称不能为空' };
    }
    const normalizedKey = key.toLowerCase();
    if (seen.has(normalizedKey)) {
      return { valid: false, customHeaders: '', error: `请求头 "${key}" 重复了` };
    }
    seen.add(normalizedKey);
    headers[key] = value;
  }

  return {
    valid: true,
    customHeaders: Object.keys(headers).length > 0 ? JSON.stringify(headers) : '',
  };
}

export function serializeSiteApiEndpoints(fields: SiteApiEndpointField[]): {
  valid: boolean;
  apiEndpoints: Array<{
    url: string;
    enabled: boolean;
    sortOrder: number;
  }>;
  error?: string;
} {
  const apiEndpoints: Array<{
    url: string;
    enabled: boolean;
    sortOrder: number;
  }> = [];
  const seen = new Set<string>();

  for (const field of fields) {
    const rawUrl = field.url.trim();
    if (!rawUrl) continue;
    const normalizedUrl = normalizeSiteApiEndpointUrl(rawUrl);
    if (!normalizedUrl) continue;
    if (seen.has(normalizedUrl)) {
      return {
        valid: false,
        apiEndpoints: [],
        error: `API 请求地址 "${normalizedUrl}" 重复了`,
      };
    }
    seen.add(normalizedUrl);
    apiEndpoints.push({
      url: normalizedUrl || rawUrl,
      enabled: field.enabled !== false,
      sortOrder: apiEndpoints.length,
    });
  }

  return {
    valid: true,
    apiEndpoints,
  };
}

export function buildSiteSaveAction(editor: SiteEditorState, form: SiteSavePayload): SiteSaveAction {
  if (editor.mode === 'edit') {
    if (!Number.isFinite(editor.editingSiteId)) {
      throw new Error('editingSiteId is required in edit mode');
    }
    return { kind: 'update', id: editor.editingSiteId, payload: form };
  }
  return { kind: 'add', payload: form };
}
