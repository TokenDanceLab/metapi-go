# Client/tool credential export adapters (#120)

**Date:** 2026-07-17  
**Backlog:** [#120](https://github.com/TokenDanceLab/metapi-go/issues/120) · competitive learn L11  
**Peers:** all-api-hub one-click export integrations (browser extension — not product shape)  
**Code:** `handler/admin/downstream_keys_export.go`

## Goal

Give operators a **server-side, admin-auth** way to export a downstream key as ready-to-paste config snippets for common clients/tools (OpenAI-compatible env, Cherry Studio / CCR-style provider JSON, generic JSON). Reduces copy-paste errors without browser sniffing, WebDAV rewrites, or reverse-engineering proprietary client binaries.

## API

| Method | Path | Auth |
|--------|------|------|
| GET | `/api/downstream-keys/{id}/export` | Admin (`/api/*` group) |

### Query params

| Param | Required | Notes |
|-------|----------|-------|
| `profile` | no | `openai` · `cherry` · `generic`. Omit to return all profiles. |
| `baseUrl` | no* | Explicit public origin for clients. Wins over env/settings/host. |

\* If omitted, resolution order is:

1. `?baseUrl=`
2. `PUBLIC_BASE_URL` env
3. settings table key `public_base_url` (plain or JSON-encoded string)
4. request host (`X-Forwarded-Host` / `Host` + `X-Forwarded-Proto` / TLS)

### Response shape

```json
{
  "success": true,
  "formatVersion": "1",
  "keyId": 12,
  "keyName": "ops-bot",
  "keyMasked": "sk-e****cdef",
  "baseUrl": "https://metapi.example.com",
  "profiles": [
    {
      "id": "openai",
      "label": "OpenAI-compatible env",
      "format": "env",
      "description": "…",
      "content": {
        "OPENAI_API_KEY": "sk-…",
        "OPENAI_BASE_URL": "https://metapi.example.com/v1"
      },
      "snippet": "OPENAI_API_KEY=sk-…\nOPENAI_BASE_URL=https://metapi.example.com/v1\n"
    }
  ]
}
```

| Field | Meaning |
|-------|---------|
| `formatVersion` | Adapter contract version. Bump on non-compatible profile shape changes. |
| `baseUrl` | Normalized origin (`http(s)://host[:port]`, no path). |
| `profiles[]` | One or more export adapters. Each has structured `content` + copyable `snippet`. |
| `keyMasked` | Display-safe mask; full key only in profile content (admin already can read it). |

## Profiles (formatVersion `1`)

| id | format | Payload |
|----|--------|---------|
| `openai` | `env` | `OPENAI_API_KEY` + `OPENAI_BASE_URL` (`baseUrl` + `/v1`) |
| `cherry` | `json` | Cherry Studio / CCR-style OpenAI-compatible provider block (`apiKey`, `apiHost`/`baseUrl`) |
| `generic` | `json` | Neutral JSON: `apiKey`, `baseUrl`, `openaiBaseUrl`, auth header hints |

## Security / non-goals

- **Only** re-emits the selected downstream key value that admin list/overview already expose.
- Never invents upstream account tokens, OAuth secrets, or passwords.
- No browser page sniffing, no WebDAV extension sync, no proprietary binary formats.
- Export is admin-only; do not expose on unauthenticated proxy paths.

## Future adapters

Keep `formatVersion` stable while adding new profile `id`s. Breaking field renames inside an existing profile require a formatVersion bump and dual-read notes in this document.
