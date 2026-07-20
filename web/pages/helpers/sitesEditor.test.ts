import { describe, expect, it } from 'vitest';
import {
  buildSiteSaveAction,
  emptySiteApiEndpoint,
  emptySiteCustomHeader,
  emptySiteForm,
  formatSiteMaxConcurrency,
  parseSiteMaxConcurrency,
  serializeSiteApiEndpoints,
  serializeSiteCustomHeaders,
  siteFormFromSite,
} from './sitesEditor.js';

describe('buildSiteSaveAction', () => {
  it('returns add action in add mode', () => {
    const action = buildSiteSaveAction(
      { mode: 'add' },
      {
        name: 'site-a',
        url: 'https://a.example.com/',
        externalCheckinUrl: 'https://checkin.a.example.com',
        platform: 'new-api',
        proxyUrl: 'socks5://127.0.0.1:1080',
        apiEndpoints: [
          { url: 'https://api-a.example.com', enabled: true, sortOrder: 0 },
          { url: 'https://api-b.example.com', enabled: false, sortOrder: 1 },
        ],
        customHeaders: '{"x-site-token":"alpha"}',
        customHeadersOverrideRequestHeaders: false,
        useSystemProxy: false,
        globalWeight: 1.2,
        maxConcurrency: 0,
        postRefreshProbeEnabled: true,
        postRefreshProbeModel: 'gpt-4o',
        postRefreshProbeScope: 'single',
        postRefreshProbeLatencyThresholdMs: 2500,
      },
    );

    expect(action).toEqual({
      kind: 'add',
      payload: {
        name: 'site-a',
        url: 'https://a.example.com/',
        externalCheckinUrl: 'https://checkin.a.example.com',
        platform: 'new-api',
        proxyUrl: 'socks5://127.0.0.1:1080',
        apiEndpoints: [
          { url: 'https://api-a.example.com', enabled: true, sortOrder: 0 },
          { url: 'https://api-b.example.com', enabled: false, sortOrder: 1 },
        ],
        customHeaders: '{"x-site-token":"alpha"}',
        customHeadersOverrideRequestHeaders: false,
        useSystemProxy: false,
        globalWeight: 1.2,
        maxConcurrency: 0,
        postRefreshProbeEnabled: true,
        postRefreshProbeModel: 'gpt-4o',
        postRefreshProbeScope: 'single',
        postRefreshProbeLatencyThresholdMs: 2500,
      },
    });
  });

  it('returns update action in edit mode with site id', () => {
    const action = buildSiteSaveAction(
      { mode: 'edit', editingSiteId: 12 },
      {
        name: 'site-b',
        url: 'https://b.example.com',
        externalCheckinUrl: '',
        platform: 'one-api',
        proxyUrl: '',
        useSystemProxy: true,
        apiEndpoints: [],
        customHeaders: '',
        globalWeight: 0.8,
        maxConcurrency: 8,
      },
    );

    expect(action).toEqual({
      kind: 'update',
      id: 12,
      payload: {
        name: 'site-b',
        url: 'https://b.example.com',
        externalCheckinUrl: '',
        platform: 'one-api',
        proxyUrl: '',
        useSystemProxy: true,
        apiEndpoints: [],
        customHeaders: '',
        globalWeight: 0.8,
        maxConcurrency: 8,
      },
    });
  });

  it('throws when edit mode has no site id', () => {
    expect(() =>
      buildSiteSaveAction(
        { mode: 'edit' } as unknown as Parameters<typeof buildSiteSaveAction>[0],
        {
          name: 'site-c',
          url: 'https://c.example.com',
          externalCheckinUrl: '',
          platform: '',
          proxyUrl: '',
          useSystemProxy: false,
          apiEndpoints: [],
          customHeaders: '',
          globalWeight: 1,
          maxConcurrency: 0,
        },
      ),
    ).toThrow('editingSiteId is required in edit mode');
  });

  it('does not expose deprecated apiKey in site editor state', () => {
    const legacySite = {
      name: 'site-d',
      url: 'https://d.example.com',
      externalCheckinUrl: null,
      platform: 'new-api',
      proxyUrl: 'http://127.0.0.1:8080',
      apiEndpoints: [
        {
          url: 'https://api.example.com',
          enabled: false,
          cooldownUntil: '2026-04-01T00:05:00.000Z',
          lastFailureReason: 'HTTP 502',
        },
      ],
      customHeaders: '{"x-site-token":"alpha"}',
      globalWeight: 1,
      maxConcurrency: 4,
      apiKey: 'sk-legacy-site-key',
    } as unknown as Parameters<typeof siteFormFromSite>[0];

    expect(emptySiteForm()).not.toHaveProperty('apiKey');
    expect(emptySiteForm().maxConcurrency).toBe('0');
    expect(emptySiteForm().customHeaders).toEqual([emptySiteCustomHeader()]);
    expect(emptySiteForm().apiEndpoints).toEqual([emptySiteApiEndpoint()]);
    expect(emptySiteForm().proxyUrl).toBe('');
    expect(siteFormFromSite(legacySite)).not.toHaveProperty('apiKey');
    expect(siteFormFromSite(legacySite).maxConcurrency).toBe('4');
    expect(siteFormFromSite({
      proxyUrl: 'http://127.0.0.1:8080',
    }).proxyUrl).toBe('http://127.0.0.1:8080');
    expect(siteFormFromSite(legacySite).apiEndpoints).toEqual([
      {
        url: 'https://api.example.com',
        enabled: false,
        cooldownUntil: '2026-04-01T00:05:00.000Z',
        lastFailureReason: 'HTTP 502',
      },
    ]);
  });

  it('defaults maxConcurrency to 0 when missing or invalid on hydrate', () => {
    expect(siteFormFromSite({}).maxConcurrency).toBe('0');
    expect(siteFormFromSite({ maxConcurrency: -3 }).maxConcurrency).toBe('0');
    expect(siteFormFromSite({ maxConcurrency: '12.9' }).maxConcurrency).toBe('12');
  });

  it('parses and formats maxConcurrency for save/list', () => {
    expect(parseSiteMaxConcurrency('0')).toEqual({ valid: true, value: 0 });
    expect(parseSiteMaxConcurrency('')).toEqual({ valid: true, value: 0 });
    expect(parseSiteMaxConcurrency('16')).toEqual({ valid: true, value: 16 });
    expect(parseSiteMaxConcurrency('-1')).toEqual({
      valid: false,
      value: 0,
      error: '最大并发必须是非负整数（0 = 不限制）',
    });
    expect(parseSiteMaxConcurrency('1.5')).toEqual({
      valid: false,
      value: 0,
      error: '最大并发必须是非负整数（0 = 不限制）',
    });
    expect(formatSiteMaxConcurrency(0)).toBe('不限制');
    expect(formatSiteMaxConcurrency(null)).toBe('不限制');
    expect(formatSiteMaxConcurrency(8)).toBe('8');
  });

  it('parses custom headers json into key value rows', () => {
    expect(siteFormFromSite({
      name: 'site-e',
      customHeaders: '{"x-site-token":"alpha","cf-access-client-id":"beta"}',
    }).customHeaders).toEqual([
      { key: 'x-site-token', value: 'alpha' },
      { key: 'cf-access-client-id', value: 'beta' },
    ]);
  });

  it('serializes key value rows into json', () => {
    expect(serializeSiteCustomHeaders([
      { key: 'x-site-token', value: 'alpha' },
      { key: 'cf-access-client-id', value: 'beta' },
      emptySiteCustomHeader(),
    ])).toEqual({
      valid: true,
      customHeaders: '{"x-site-token":"alpha","cf-access-client-id":"beta"}',
    });
  });

  it('rejects duplicate custom header names case-insensitively', () => {
    expect(serializeSiteCustomHeaders([
      { key: 'Authorization', value: 'Bearer a' },
      { key: 'authorization', value: 'Bearer b' },
    ])).toEqual({
      valid: false,
      customHeaders: '',
      error: '请求头 "authorization" 重复了',
    });
  });

  it('serializes api endpoint rows into ordered payloads', () => {
    expect(serializeSiteApiEndpoints([
      { url: 'https://api-a.example.com/', enabled: true },
      { url: 'https://api-b.example.com', enabled: false },
      emptySiteApiEndpoint(),
    ])).toEqual({
      valid: true,
      apiEndpoints: [
        { url: 'https://api-a.example.com', enabled: true, sortOrder: 0 },
        { url: 'https://api-b.example.com', enabled: false, sortOrder: 1 },
      ],
    });
  });

  it('rejects duplicate api endpoints after normalization', () => {
    expect(serializeSiteApiEndpoints([
      { url: 'https://api.example.com/', enabled: true },
      { url: 'https://api.example.com', enabled: true },
    ])).toEqual({
      valid: false,
      apiEndpoints: [],
      error: 'API 请求地址 "https://api.example.com" 重复了',
    });
  });
});

describe('customHeadersOverrideRequestHeaders (#584)', () => {
  it('defaults false in emptySiteForm', () => {
    expect(emptySiteForm().customHeadersOverrideRequestHeaders).toBe(false);
  });

  it('reads override flag from site', () => {
    const form = siteFormFromSite({
      name: 's',
      url: 'https://example.com',
      customHeadersOverrideRequestHeaders: true,
    });
    expect(form.customHeadersOverrideRequestHeaders).toBe(true);
  });

  it('round-trips override flag in save payload', () => {
    const action = buildSiteSaveAction(
      { mode: 'add' },
      {
        name: 'site-a',
        url: 'https://a.example.com/',
        externalCheckinUrl: '',
        platform: 'new-api',
        proxyUrl: '',
        apiEndpoints: [],
        customHeaders: '',
        customHeadersOverrideRequestHeaders: true,
        useSystemProxy: false,
        globalWeight: 1,
        maxConcurrency: 0,
      },
    );
    expect(action.kind).toBe('add');
    if (action.kind === 'add') {
      expect(action.payload.customHeadersOverrideRequestHeaders).toBe(true);
    }
  });
});
