# Admin API Error Model (B3 / #19)

**Date:** 2026-07-17  
**Lane:** backend-core  
**SSOT code:** `handler/shared/errors.go`, admin wrappers in `handler/admin/response_errors.go`

## Goal

Unify admin mutation error responses so clients never need to treat **HTTP 200 + error body** as failure.

## Wire format (camelCase JSON)

```json
{"error":"human-readable message"}
```

Optional classifier:

```json
{"error":"API key 已存在","detail":"duplicate_key"}
```

| Field | JSON key | Notes |
|:------|:---------|:------|
| Message | `error` | Safe public string |
| Detail | `detail` | Optional subtype / machine hint |
| HTTP status | response code | Always **≥ 400** for failures |
| Internal | (not serialized) | Logged server-side only |

Helpers: `shared.WriteError`, `shared.WriteErrorDetail`, `shared.WriteAPIError`, admin `writeError` / `writeErrorDetail`.

## Classification boundaries

| Class | Typical status | Examples |
|:------|:---------------|:---------|
| **validation** | 400 | Invalid payload, empty name/url/token, bad enums |
| **auth (admin session)** | 401 / 403 | Missing/invalid admin bearer (auth middleware) |
| **auth (upstream login/verify)** | 401 / 502 | Login rejected by upstream; verify transport failure |
| **not found** | 404 | Site / account / key missing |
| **conflict** | 409 | Duplicate site URL or downstream key |
| **upstream** | 502 | Login/verify/create-token upstream adapter failure |
| **internal** | 500 | DB / encryption / unexpected handler failure |

Upstream **proxy** OpenAI-shaped errors remain nested (`{"error":{"message","type"}}`) in `handler/proxy` — that surface is for model clients, not the admin SPA. Admin uses the flat `{"error":"..."}` model above.

Related: account-expired classification for upstream token health is documented in `docs/analysis/error-classification.md` (R0) and is orthogonal to HTTP response shaping.

## High-traffic mutation paths covered

| Resource | Create / update | Silent 200 forbidden |
|:---------|:----------------|:---------------------|
| Sites | `POST /api/sites`, `PUT /api/sites/{id}` | Yes — validation/conflict/internal use 4xx/5xx + `error` |
| Accounts | `POST /api/accounts`, `PUT /api/accounts/{id}` | Yes |
| Accounts login/verify | `POST /api/accounts/login`, `.../verify-token` | Yes — failures no longer return 200 + `success:false` |
| Downstream keys | `POST /api/downstream-keys`, `PUT /api/downstream-keys/{id}` | Yes |

Batch **partial** success responses may still return 200 with per-item results when at least one item succeeds. All-failed batch create returns **400** with structured items (not a silent success).

## Client notes

`web/api.ts` already:

1. Throws on `!res.ok`, reading `json.error` or `json.message`.
2. Uses try/catch for mutations — non-2xx failures surface as thrown `Error` messages.

Login / verify UIs that previously branched on `result.success` still work: success stays 200 + `success:true`; failures throw into the existing `catch` path.

## Tests

- `handler/shared/errors_test.go` — status, camelCase body, no silent 200
- `handler/admin/error_model_test.go` — sites / accounts / keys create-update status codes
