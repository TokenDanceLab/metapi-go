# P4 #184 — Real system-proxy test + brand list from registry

**Date**: 2026-07-17
**Issue**: #184
**Branch**: `feat/p4-settings-proxy-impl`

## Problem

- `POST /api/settings/system-proxy/test` always returned fake success (`reachable: true`, fixed `latencyMs: 100`, status 204) without performing an HTTP probe.
- `GET /api/settings/brand-list` returned a hardcoded 5-name stub (`new-api`, `one-api`, `veloera`, `lobechat`, `openwebui`) instead of registered platforms.

## Solution

### System proxy test

Aligned with the TypeScript reference (`metapi/src/server/routes/api/settings.ts`):

| Field | Behavior |
|-------|----------|
| Probe URL | `https://www.gstatic.com/generate_204` |
| Timeout | 15s |
| Proxy source | request `proxyUrl` if present, else `config.SystemProxyUrl` |
| Transport | `platform.DoWithProxy` with explicit `ProxyConfig.ProxyURL` |
| Success body | `{ success, proxyUrl, probeUrl, finalUrl, reachable, ok, statusCode, latencyMs }` |
| Failure | `400` missing/invalid proxy; `502` probe error with Chinese operator message |

`reachable` means a response was received through the proxy. `ok` is HTTP 2xx. Non-2xx responses still return HTTP 200 with `ok: false` (connectivity worked; probe target rejected).

Tests inject `systemProxyTestTransport` (fake `http.RoundTripper`) so unit tests do not hit the network.

### Brand list

`platform.ListRegisteredPlatformNames()` returns canonical adapter names from the platform registry, ordered by `orderedPlatformNames` (openai → … → one-api). The settings handler returns `{ brands: [...] }` from that helper.

## Verify

```bash
go test ./handler/admin -count=1 -run 'Proxy|Brand'
```

## Files

- `handler/admin/settings.go`
- `handler/admin/settings_proxy_brand_test.go`
- `platform/registry.go` (`ListRegisteredPlatformNames`)
- `docs/analysis/p4-settings-proxy-test.md`
