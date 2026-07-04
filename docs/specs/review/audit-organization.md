# Package Organization Audit: metapi-go

**Date**: 2026-07-04
**Scope**: Full repository, excluding `.git/`, `testdata/`, `e2e/`, and `web/` (frontend assets).
**Method**: Static analysis of import graph (DFS cycle detection), package-name collision scan, file/line counts, and architectural review.

---

## Executive Summary

The repository is well-structured overall with no circular dependencies. The import graph forms a clean DAG. The primary concerns are: (1) a naming collision between two `proxy` packages, (2) the `handler/admin/` package is excessively large and monolithic, and (3) the root `service/` package mixes concerns that could be better scoped into sub-packages. Minor issues include confusingly similar package names (`router/` vs `routing/`) and inconsistent file-naming conventions within `handler/admin/`.

---

## 1. Circular Import Analysis

**Result: PASS -- No circular dependencies detected.**

Full DFS traversal of the import graph across 37 distinct package directories confirms the graph is acyclic. The dependency direction is clean:

```
cmd/server -> app, router -> handler/{admin,proxy} -> {service, proxy, routing} -> store -> config
```

`config` sits at the bottom with zero internal dependencies. `platform` and `transform` are lateral trees that do not import from the main application packages, which is architecturally sound.

---

## 2. Package Naming Collisions

| Package Name | Directories | Severity |
|---|---|---|
| `proxy` | `proxy/` (15 files, 2999 lines), `handler/proxy/` (19 files, 1969 lines) | **HIGH** |
| `main` | `cmd/migrate/`, `cmd/server/` | None (standard Go convention) |

### 2.1 `proxy` Name Collision (HIGH)

Two distinct packages both declare `package proxy`:

- **`proxy/`** (top-level): Core proxy engine -- `Conductor`, `ProxyChannelCoordinator`, `EndpointFlow`, `FailureJudge`, `RetryPolicy`, session management, CLI profile detection. Internal sub-packages: `proxy/profiles/` and `proxy/types/`.

- **`handler/proxy/`**: Thin HTTP handlers for `/v1/*` proxy routes -- `chat.go`, `completions.go`, `embeddings.go`, `messages.go`, `responses.go`, `gemini.go`, etc. Delegates to the top-level `proxy` package.

**Current workaround**: The `router/` package uses an import alias:
```go
proxyhandler "github.com/tokendancelab/metapi-go/handler/proxy"
```

This is a code smell. Any developer working in a file that imports both packages must carefully manage aliases, and the collision is easy to miss during refactoring.

**Recommendations** (pick one):
- **A (preferred)**: Rename `handler/proxy/` to `handler/proxyapi/` (package `proxyapi`). This is a transparent rename that clarifies the package handles the proxy HTTP API surface.
- **B**: Rename `handler/proxy/` package declaration to `proxyhandler` (directory name stays, Go package name changes). Simpler but inconsistent with Go convention that package name matches directory name.
- **C**: Rename top-level `proxy/` to `proxyengine/` (package `proxyengine`). Higher effort since more files import it.

**Impact of A**: 3 files import `handler/proxy` (`router/router.go`, `cmd/server/main.go`, and `handler/proxy/*` internal files). All can be updated mechanically.

---

## 3. `handler/admin/` -- Monolithic Package (HIGH)

**24 files, 7,551 lines, single `package admin`.**

### 3.1 Current Responsibilities

| File(s) | Concern | Lines |
|---|---|---|
| `accounts.go` + `account_tokens.go` | Account + token CRUD | ~1,571 |
| `sites.go` | Site CRUD, detection, probing | ~815 |
| `downstream_keys.go` | Downstream API key management | ~1,233 |
| `token_routes.go` | Token routing configuration | ~707 |
| `stats.go` | Dashboard, proxy logs, traces, marketplace | ~401 |
| `settings.go` | Runtime settings (brand, proxy, cache) | ~640 |
| `settings_backup.go` | Backup export/import + WebDAV | ~337 |
| `settings_database.go` | Database config + migration | ~175 |
| `settings_maintenance.go` | Cache clear, factory reset | ~80 |
| `settings_notify.go` | Notification test endpoint | ~25 |
| `oauth_routes.go` | OAuth provider flows | ~170 |
| `checkin_routes.go` | Checkin triggers + logs | ~120 |
| `events.go` | Event log CRUD | ~110 |
| `monitor.go` | Monitor config + LDOH proxy | ~100 |
| `search.go` | Global search | ~200 |
| `tasks.go` | Task listing | ~40 |
| `auth_settings.go` | Auth token management | ~105 |
| `site_announcements.go` | Site announcement sync | ~200 |
| `update_center.go` | Update check + deploy | ~150 |
| `test.go` | Proxy/chat test endpoints | ~75 |
| `payloads/` | Request/response DTOs | 3 files |

### 3.2 Assessment

This package has grown into a "catch-all" for every admin API concern. While each file is nominally focused on one domain, they all share a single package namespace, meaning:

- Any type, constant, or helper function is visible to all other files (no encapsulation).
- Helper functions like `coalesceInt`, `normalizeString`, `toFloat64`, `toBool` are duplicated or share global scope.
- The `settings_*.go` files share a `settingsHandler` struct despite spanning 5 files -- the cohesion is implicit, not enforced.
- Test coverage exists only for `accounts`, `account_tokens`, and `sites` -- 3 of 20+ route groups.

### 3.3 Recommendation: Split into Sub-Packages

Proposed structure within `handler/admin/`:

```
handler/admin/
  accounts.go / accounts_test.go        --> handler/admin/accounts/
  account_tokens.go / *_test.go         --> handler/admin/accounts/   (merged)
  auth_settings.go                      --> handler/admin/auth/
  checkin_routes.go                     --> handler/admin/checkin/
  downstream_keys.go                    --> handler/admin/keys/
  events.go                             --> handler/admin/events/
  monitor.go                            --> handler/admin/monitor/
  oauth_routes.go                       --> handler/admin/oauth/
  search.go                             --> handler/admin/search/
  settings.go                           --> handler/admin/settings/
  settings_backup.go                    --> handler/admin/settings/
  settings_database.go                  --> handler/admin/settings/
  settings_maintenance.go               --> handler/admin/settings/
  settings_notify.go                    --> handler/admin/settings/
  site_announcements.go                 --> handler/admin/sites/
  sites.go + sites_test.go              --> handler/admin/sites/
  stats.go                              --> handler/admin/stats/
  tasks.go                              --> handler/admin/tasks/
  test.go                               --> handler/admin/testing/
  token_routes.go                       --> handler/admin/tokenroutes/
  update_center.go                      --> handler/admin/update/
  payloads/                             --> keep as shared DTOs, or move into respective sub-packages
```

This reduces the largest sub-package (settings) to ~1,257 lines across 5 files, and the largest sub-package overall (accounts) to ~1,571 lines -- both well within reason.

---

## 4. `proxy/` Package Assessment

**15 files, 2,999 lines (excluding sub-packages).**

The top-level `proxy/` package is actually well-scoped. It implements the core proxy execution pipeline:

| File | Responsibility |
|---|---|
| `conductor.go` | Orchestrates attempt/retry/failover loop |
| `channel_selection.go` | Channel selection with forced-channel and loopback detection |
| `endpoint_flow.go` | Full upstream request lifecycle (auth, headers, dispatch) |
| `executor.go` | Low-level HTTP dispatch with first-byte timeout |
| `failure_judge.go` | Detects proxy failures from raw response text |
| `retry_policy.go` | Pattern-based retry/abort decisions |
| `session.go` | Channel lease coordination + sticky sessions |
| `surface.go` | Public API surface (channel binding, failure toolkit) |
| `detect.go` | Client context detection (SDK type identification) |
| `profile.go` | CLI profile detection and registration |
| `profiles/` | Per-CLI profile definitions (claude_code, codex, gemini_cli) |
| `types/` | Shared type definitions |

**Verdict**: This package is well-factored and does not need splitting. The primary issue is the name collision with `handler/proxy/` (see Section 2).

---

## 5. `service/` Package Assessment

**43 files total: 11 root + 32 across 7 sub-packages.**

### 5.1 Root-Level Files

The root `service/` package contains:

| File | Concern | Suggested Home |
|---|---|---|
| `account_service.go` | Account CRUD, credential mode, capabilities | `service/account/` |
| `account_token_service.go` | Token CRUD, masking, default token logic | `service/account/` |
| `account_credential.go` | Password encrypt/decrypt | `service/account/` |
| `account_extra_config.go` | Extra config parsing (auto-relogin, sub2api) | `service/account/` |
| `account_health.go` | Runtime health state management | `service/account/` |
| `site_service.go` | Site CRUD, cache invalidation, route rebuild | `service/site/` |
| `site_detect.go` | Site platform detection | `service/site/` |
| `site_endpoint_service.go` | Endpoint URL normalization/validation | `service/site/` |
| `proxy_util.go` | Proxy-aware HTTP client helpers | `service/httputil/` or leave in root |
| `localtime.go` | Time formatting utilities | `service/` (generic utility) |
| `today_reward.go` | Today's income snapshot tracking | `service/account/` or `service/reward/` |

### 5.2 Sub-Packages (Well-Scoped)

| Sub-Package | Files | Status |
|---|---|---|
| `service/adapter/` | 1 | OK -- interface definition |
| `service/alert/` | 2 | OK -- alert rules + dispatch |
| `service/balance/` | 2 | OK -- balance checking |
| `service/checkin/` | 4 | OK -- checkin logic + reward parsing |
| `service/daily/` | 2 | OK -- daily summary |
| `service/notify/` | 8 | OK -- multi-channel notifications |
| `service/oauth/` | 17 | OK -- large but cohesive OAuth flow |

### 5.3 Recommendation

Group the 5 account-related root files into `service/account/` and the 3 site-related root files into `service/site/`. `localtime.go` and `proxy_util.go` can remain in the root `service/` package as cross-cutting utilities. This would reduce the root package from 11 files to 2, making the `service/` namespace much clearer.

---

## 6. `router/` vs `routing/` Naming Confusion

| Package | Purpose | Files | Lines |
|---|---|---|---|
| `router/` | HTTP chi router setup, middleware stack, route registration, SPA fallback | 2 | 185 |
| `routing/` | Channel/route selection algorithm, weights, cooldown, snapshot, pricing, matcher, round-robin | 15 | 5,641 |

The names are confusingly similar. A new contributor seeing `import "...routing"` and `import "...router"` in the same file (which happens in `cmd/server/main.go` via transitive imports) would need to mentally disambiguate every time.

**Recommendation**: Rename `router/` to `httpserver/` or `server/` (it's the HTTP server wiring layer). Alternatively, rename `routing/` to `channelrouter/` or `routeselect/` to clarify it deals with channel-level routing decisions rather than HTTP routing.

---

## 7. `platform/` Package Assessment

**20 files, 5,399 lines, zero internal dependencies.**

This package is a clean, self-contained adapter layer for 14 upstream API platforms. It uses Go struct embedding for inheritance:

```
BaseAdapter
  StandardAdapter
    OpenAiAdapter, ClaudeAdapter, GeminiAdapter, GeminiCliAdapter, CliProxyApiAdapter
  CodexAdapter, AntigravityAdapter, OneApiAdapter, OneHubAdapter, DoneHubAdapter,
  VeloeraAdapter, Sub2ApiAdapter, NewApiAdapter, AnyRouterAdapter
```

Each adapter lives in its own file (e.g., `openai.go`, `claude.go`) with a corresponding `*_test.go`. The package also includes detection logic (`detect.go`), a platform alias registry (`registry.go`), and the `SiteProxyAdapter` (`site_proxy.go`).

**Verdict**: Well-structured. No splitting needed. The flat file-per-adapter convention is idiomatic Go for this pattern. The only minor note: `newapi.go` at 1,606 lines is the largest single file in the entire repository and could be split by concern (login, checkin, balance).

---

## 8. `transform/` Package Assessment

**13 files, 6,128 lines across 8 sub-packages.**

```
transform/
  shared/          -- Chat format core types + utilities
  canonical/       -- Canonical envelope + OpenAI bridge
  anthropic/messages/
  gemini/generate_content/
  openai/chat/
  openai/completions/
  openai/embeddings/
  openai/images/
  openai/responses/
```

Clean tree structure organized by provider and format. Each leaf package handles format-specific request/response transformation. No issues.

---

## 9. File Naming Inconsistencies in `handler/admin/`

The `handler/admin/` package uses inconsistent file-naming conventions:

**Uses underscores**:
- `auth_settings.go`
- `checkin_routes.go`
- `downstream_keys.go`
- `oauth_routes.go`
- `token_routes.go`
- `site_announcements.go`
- `update_center.go`

**No underscores**:
- `accounts.go`
- `account_tokens.go`
- `events.go`
- `monitor.go`
- `search.go`
- `stats.go`
- `tasks.go`
- `test.go`
- `sites.go`
- `settings.go`

Additionally, `settings_*.go` uses underscore prefixes for variants while most other files do not follow any variant-splitting pattern consistently.

**Recommendation**: Pick one convention. Go community convention favors no underscores in file names unless the underscore separates a logical prefix (like `settings_backup.go`). The files `checkin_routes.go` and `token_routes.go` could become `checkin.go` and `tokenroutes.go` respectively.

---

## 10. Test Coverage Distribution

| Package | Production Files | Test Files | Coverage |
|---|---|---|---|
| `handler/admin/` | 24 | 3 (`accounts_test.go`, `account_tokens_test.go`, `sites_test.go`) | 12.5% of files |
| `handler/proxy/` | 19 | 11 | 58% |
| `proxy/` | 15 | 7 | 47% |
| `platform/` | 20 | 12 | 60% |
| `routing/` | 15 | 9 | 60% |
| `service/` | 11 | 2 | 18% |
| `service/oauth/` | 17 | 8 | 47% |
| `store/` | 7 | 5 | 71% |

The largest gap is `handler/admin/` where 21 of 24 files have no dedicated tests. This is the highest-risk area given its size and the fact that it directly handles HTTP request/response cycles.

---

## 11. Summary of Recommendations

| Priority | Issue | Recommendation | Effort |
|---|---|---|---|
| **P0** | `proxy` package name collision | Rename `handler/proxy/` to `handler/proxyapi/` | Low -- 3 import sites |
| **P0** | `handler/admin/` monolithic | Split into ~10 sub-packages by domain | High -- ~20 files moved, imports updated |
| **P1** | `service/` root mixing concerns | Move account+site files to `service/account/` and `service/site/` | Medium -- ~8 files moved |
| **P1** | `router/` vs `routing/` confusion | Rename `router/` to `httpserver/` | Low -- ~3 import sites |
| **P2** | `handler/admin/` file naming | Standardize naming (drop unnecessary `_routes` suffix) | Low -- mechanical rename |
| **P2** | `platform/newapi.go` size | Split 1,606-line file by concern | Medium |
| **P3** | `handler/admin/` test gaps | Add tests for settings, stats, keys, token_routes | Ongoing |
| **P3** | `handler/admin/settings_*.go` shared struct | If splitting into sub-packages, make the struct explicit or split into independent handlers | Medium |

---

## Appendix A: Full Dependency Graph

```
app                        -> config, scheduler, store
auth                       -> config, store
cmd/migrate                (leaf)
cmd/server                 -> app, config, router, store, web
config                     (leaf -- zero internal deps)
handler/admin              -> config, handler/admin/payloads, service, store
handler/admin/payloads     (leaf)
handler/proxy              -> auth, config, proxy, routing
platform                   (leaf -- zero internal deps)
proxy                      -> config, proxy/profiles, proxy/types, routing, service
proxy/profiles             -> proxy/types
proxy/types                (leaf)
router                     -> auth, config, handler/admin, handler/proxy, store
routing                    -> config, store
scheduler                  -> config, service/{balance,checkin,daily,notify}, store
service                    -> config, store
service/{adapter,oauth}    (leaf)
service/alert              -> config, service, service/notify
service/balance            -> config, service, service/adapter, service/alert, store
service/checkin            -> config, service, service/{adapter,alert,balance,notify}, store
service/daily              -> config, service, service/checkin, service/notify, store
service/notify             -> config, service
store                      -> config
transform/anthropic/messages -> transform/shared
transform/canonical         (leaf)
transform/gemini/generate_content -> transform/shared
transform/openai/chat       -> transform/canonical, transform/shared
transform/openai/{completions,embeddings,images,responses} (leaf)
transform/shared            (leaf)
web                         (leaf)
```
