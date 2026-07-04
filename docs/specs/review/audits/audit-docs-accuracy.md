# Doc Accuracy Audit: api.md + architecture.md vs Code

**Audit date**: 2026-07-05
**Scope**: `docs/api.md`, `docs/architecture.md` against `handler/admin/*.go` + `handler/proxy/router.go` + top-level package tree

---

## 1. api.md Endpoint Audit

### 1.1 ENDPOINTS IN CODE BUT MISSING FROM api.md

These routes are registered in Go handler files but have no corresponding entry in `docs/api.md`:

| # | Endpoint | Method | Source File |
|---|----------|--------|-------------|
| 1 | `/api/sites/batch` | POST | `sites.go` |
| 2 | `/api/sites/detect` | POST | `sites.go` |
| 3 | `/api/sites/{id}/disabled-models` | GET | `sites.go` |
| 4 | `/api/sites/{id}/disabled-models` | PUT | `sites.go` |
| 5 | `/api/sites/{id}/available-models` | GET | `sites.go` |
| 6 | `/api/sites/{id}/probe-now` | POST | `sites.go` |
| 7 | `/api/sites/{id}/probe-stream` | GET | `sites.go` |
| 8 | `/api/accounts/login` | POST | `accounts.go` |
| 9 | `/api/accounts/verify-token` | POST | `accounts.go` |
| 10 | `/api/accounts/{id}/rebind-session` | POST | `accounts.go` |
| 11 | `/api/accounts/batch` | POST | `accounts.go` |
| 12 | `/api/accounts/health/refresh` | POST | `accounts.go` |
| 13 | `/api/accounts/{id}/balance` | POST | `accounts.go` |
| 14 | `/api/accounts/{id}/models` | GET | `accounts.go` |
| 15 | `/api/accounts/{id}/models/manual` | POST | `accounts.go` |
| 16 | `/api/account-tokens/batch` | POST | `account_tokens.go` |
| 17 | `/api/account-tokens/{id}/default` | POST | `account_tokens.go` |
| 18 | `/api/account-tokens/{id}/value` | GET | `account_tokens.go` |
| 19 | `/api/account-tokens/sync/{accountId}` | POST | `account_tokens.go` |
| 20 | `/api/account-tokens/sync-all` | POST | `account_tokens.go` |
| 21 | `/api/account-tokens/groups/{accountId}` | GET | `account_tokens.go` |
| 22 | `/api/account-tokens/account/{accountId}/default` | GET | `account_tokens.go` |
| 23 | `/api/site-announcements/{id}/read` | POST | `site_announcements.go` |
| 24 | `/api/site-announcements/read-all` | POST | `site_announcements.go` |
| 25 | `/api/site-announcements` (delete all) | DELETE | `site_announcements.go` |
| 26 | `/api/site-announcements/sync` | POST | `site_announcements.go` |
| 27 | `/api/events` (delete all) | DELETE | `events.go` |
| 28 | `/api/downstream-keys/batch` | POST | `downstream_keys.go` |
| 29 | `/api/checkin/trigger/{id}` | POST | `checkin_routes.go` |
| 30 | `/api/checkin/logs` | GET | `checkin_routes.go` |
| 31 | `/api/update-center/config` | PUT | `update_center.go` |
| 32 | `/api/update-center/deploy` | POST | `update_center.go` |
| 33 | `/api/update-center/rollback` | POST | `update_center.go` |
| 34 | `/api/update-center/tasks/{id}/stream` | GET | `update_center.go` |
| 35 | `/api/monitor/config` | GET | `monitor.go` |
| 36 | `/api/monitor/config` | PUT | `monitor.go` |
| 37 | `/api/monitor/session` | POST | `monitor.go` |
| 38 | `/monitor-proxy/ldoh` | ANY | `monitor.go` |
| 39 | `/monitor-proxy/ldoh/*` | ANY | `monitor.go` |
| 40 | `/api/oauth/providers/{provider}/start` | POST | `oauth_routes.go` |
| 41 | `/api/oauth/sessions/{state}` | GET | `oauth_routes.go` |
| 42 | `/api/oauth/sessions/{state}/manual-callback` | POST | `oauth_routes.go` |
| 43 | `/api/oauth/connections` | GET | `oauth_routes.go` |
| 44 | `/api/oauth/connections/{accountId}/rebind` | POST | `oauth_routes.go` |
| 45 | `/api/oauth/connections/{accountId}/proxy` | PATCH | `oauth_routes.go` |
| 46 | `/api/oauth/connections/{accountId}` | DELETE | `oauth_routes.go` |
| 47 | `/api/oauth/connections/{accountId}/quota/refresh` | POST | `oauth_routes.go` |
| 48 | `/api/oauth/connections/quota/refresh-batch` | POST | `oauth_routes.go` |
| 49 | `/api/oauth/import` | POST | `oauth_routes.go` |
| 50 | `/api/oauth/route-units` | POST | `oauth_routes.go` |
| 51 | `/api/oauth/route-units/{routeUnitId}` | PATCH | `oauth_routes.go` |
| 52 | `/api/oauth/route-units/{routeUnitId}` | DELETE | `oauth_routes.go` |
| 53 | `/api/settings/auth/info` | GET | `auth_settings.go` |
| 54 | `/api/settings/auth/change` | POST | `auth_settings.go` |
| 55 | `/api/test/proxy` | POST | `test.go` |
| 56 | `/api/test/proxy/stream` | POST | `test.go` |
| 57 | `/api/test/proxy/jobs` | POST | `test.go` |
| 58 | `/api/test/proxy/jobs/{jobId}` | GET | `test.go` |
| 59 | `/api/test/proxy/jobs/{jobId}` | DELETE | `test.go` |
| 60 | `/api/test/chat` | POST | `test.go` |
| 61 | `/api/test/chat/stream` | POST | `test.go` |
| 62 | `/api/test/chat/jobs` | POST | `test.go` |
| 63 | `/api/test/chat/jobs/{jobId}` | GET | `test.go` |
| 64 | `/api/test/chat/jobs/{jobId}` | DELETE | `test.go` |

**Total missing from docs: 64 endpoints across 13 route groups.**

---

### 1.2 ENDPOINTS IN api.md BUT NOT IN CODE (Or Wrong)

These are documented in `docs/api.md` but either do not exist in the Go handler code or have wrong method/URL:

| # | Documented Endpoint | Issue | Detail |
|---|-------------------|-------|--------|
| 1 | `GET /api/sites/:id` | NOT REGISTERED | Code only has PUT and DELETE for `/api/sites/{id}`; no GET handler |
| 2 | `GET /api/accounts/:id` | NOT REGISTERED | Code only has PUT and DELETE for `/api/accounts/{id}`; no GET handler |
| 3 | `GET /api/account-tokens/:id` | NOT REGISTERED | Code only has PUT and DELETE for `/api/account-tokens/{id}`; no GET handler |
| 4 | `GET /api/site-announcements/:id` | NOT REGISTERED | Code has no GET single-announcement route at all |
| 5 | `PUT /api/site-announcements/:id` | NOT REGISTERED | Code has no PUT for single announcement |
| 6 | `GET /api/events/unread-count` | WRONG URL | Code registers `GET /api/events/count`, not `/unread-count` |
| 7 | `POST /api/events/mark-all-read` | WRONG URL | Code registers `POST /api/events/read-all`, not `/mark-all-read` |
| 8 | `PUT /api/events/:id/read` | WRONG METHOD | Code registers `POST /api/events/{id}/read`, not PUT |
| 9 | `POST /api/checkin/run` | WRONG URL | Code registers `POST /api/checkin/trigger`, not `/run` |
| 10 | `POST /api/checkin/reset-attempts` | NOT REGISTERED | No such endpoint in code |
| 11 | `GET /api/monitor/status` | WRONG URL | Code registers `GET /api/monitor/config`, not `/status` |
| 12 | `GET /api/search` | WRONG METHOD | Code registers `POST /api/search` (body-parsed), not GET |
| 13 | `POST /api/test/echo` | NOT REGISTERED | No echo endpoint in Go test handlers |
| 14 | `POST /api/oauth/authorize` | NOT REGISTERED | Code uses `POST /api/oauth/providers/{provider}/start` instead |
| 15 | `GET /api/oauth/callback` | WRONG URL | Code has `GET /api/oauth/callback/{provider}`, not bare `/callback` |
| 16 | `GET /api/auth/settings` | NOT REGISTERED | Code registers `GET /api/settings/auth/info`, different path |
| 17 | `PUT /api/auth/settings` | NOT REGISTERED | Code registers `POST /api/settings/auth/change`, different path+method |

**Total wrong/phantom endpoints in docs: 17.**

---

### 1.3 QUERY PARAMETER DRIFT

Where docs describe query params that do not match actual code behavior:

| Endpoint | Docs param | Code actual | File |
|----------|-----------|-------------|------|
| `GET /api/stats/proxy-logs` | `page`, `limit`, `status`, `model`, `client`, `from`, `to` | `view`, `limit`, `offset`, `status`, `search`, `client`, `siteId`, `from`, `to` | `stats.go` |
| `GET /api/stats/dashboard` | (none) | `refresh`, `view` | `stats.go` |
| `GET /api/stats/proxy-debug/traces` | (none) | `limit` | `stats.go` |
| `GET /api/downstream-keys/summary` | `group`, `tags`, `tagMatch` | `range`, `status`, `search`, `group`, `tags`, `tagMatch` | `downstream_keys.go` |
| `GET /api/events` | `page`, `limit`, `level` | `limit`, `offset`, `type`, `read` | `events.go` |
| `GET /api/checkin/logs` | not documented | `limit`, `offset`, `accountId` | `checkin_routes.go` |
| `POST /api/search` | `q` (query param, GET) | `query`, `limit` (JSON body, POST) | `search.go` |

---

## 2. architecture.md Audit

### 2.1 PACKAGE LAYOUT DRIFT

Architecture doc (lines 43-74) claims this package layout vs actual:

| Doc Claim | Actual | Verdict |
|-----------|--------|---------|
| `handler/admin/ (18 files, 110 endpoints)` | 29 `.go` files total (22 non-test + 4 test + 3 in `payloads/`), ~140+ endpoints | **Wrong file count** (18 vs actual 22 non-test files). Endpoint count is underspecified -- actual count is ~140+ |
| `proxycore/` with subdirs (profiles, session, retry, selector, endpoint, failure, surface, conductor) | No `proxycore/` exists. Actual: `proxy/`, `router/`, `transform/` | **Completely wrong**: directory does not exist |
| `protocol/` | No `protocol/` directory. Actual: `transform/` | **Wrong name**: should be `transform/` |
| (not mentioned) `service/` | `service/` exists as a Go package | **Missing**: important package not documented |
| (not mentioned) `data/`, `data-baseline/`, `docs/`, `e2e/`, `testdata/` | These exist | **Missing** (minor -- these are non-code or test artifacts, but `docs/` and `e2e/` are significant) |

### 2.2 ARCHITECTURE DIAGRAM ISSUES

The diagram (lines 7-38) shows `ProxyCore` as a central component with sub-stages (Profile -> Session -> Retry -> Selector -> Endpoint -> Judge), and `Protocol Transformers` beneath it. The actual codebase does NOT have a `proxycore` package. Instead:

- Proxy pipeline logic lives in `proxy/` and `router/`
- Protocol transformers live in `transform/`, not `protocol/`

This makes the architecture diagram misleading for anyone trying to navigate the codebase.

### 2.3 PROXY REQUEST FLOW

The documented flow (lines 80-96) references:
- `ProxyCore.Execute(ctx, profile, session, retry)` -- does not map to any real function name
- `ProtocolTransformer` -- wrong package name (should be `transform/`)
- `FailureJudge.Assess()` -- no such type found in handler code
- `Surface.Format()` -- no such type found in handler code

The actual proxy router (`handler/proxy/router.go`) registers handlers like `HandleChatCompletions`, `HandleClaudeMessages`, etc. which delegate to flow execution but the names do not match.

### 2.4 FILE COUNT ERROR

`handler/admin/` contains 22 non-test `.go` files (plus `payloads/` subdirectory with 3 files), not 18 as claimed:

```
accounts.go, account_tokens.go, auth_settings.go, checkin_routes.go,
doc.go, downstream_keys.go, events.go, monitor.go, oauth_routes.go,
search.go, settings.go, settings_backup.go, settings_database.go,
settings_maintenance.go, settings_notify.go, site_announcements.go,
sites.go, stats.go, tasks.go, test.go, token_routes.go, update_center.go
```

---

## 3. Summary

### api.md
- **64 endpoints** are registered in code but completely absent from docs
- **17 endpoints** are documented with wrong method, wrong URL, or do not exist in code
- **7 endpoints** have query parameter mismatches
- **Most severely affected sections**: OAuth (12 missing), Sites (7 missing), Accounts (8 missing), Test (10 missing), Account Tokens (7 missing), Update Center (4 missing)

### architecture.md
- Package tree has wrong directory names (`proxycore/` vs `proxy/`, `protocol/` vs `transform/`)
- File count is wrong (18 vs actual 22 non-test files in `handler/admin/`)
- `service/` package is not documented
- Proxy request flow uses type/function names that do not exist in the codebase
