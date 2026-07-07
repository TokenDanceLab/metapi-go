# Code Smell Audit: metapi-go

**Date**: 2026-07-05
**Scope**: `<repo>` (300 `.go` files)
**Categories**: `panic()` abuse, `init()` side effects, global mutable state, magic numbers, TODO/FIXME, commented-out code, long functions, deep nesting

---

## 1. `panic()` Calls (8 non-test, HIGH)

`panic()` kills the entire server process. It should be reserved for truly unrecoverable startup conditions, not used in request-handling or cryptographic utility paths.

| File | Line | Panic Message | Severity | Mitigation |
|---|---|---|---|---|
| `config/config.go` | 30 | `config.Get() called before config.Set()` | MEDIUM | Acceptable as a startup invariant guard; could be `log.Fatal` |
| `scheduler/usage_aggregation.go` | 141 | re-panics after `recover()` | HIGH | Re-panicking defeats the purpose of recovery. Should log the error and abort the aggregation pass gracefully |
| `service/account_credential.go` | 40 | `crypto/rand failed` | HIGH | This is a library utility. Must return `(string, error)` instead of panicking |
| `service/account_credential.go` | 45 | `aes.NewCipher failed` | HIGH | Same as above -- return error |
| `service/account_credential.go` | 50 | `cipher.NewGCM failed` | HIGH | Same as above -- return error |
| `service/oauth/account.go` | 225, 298 | `oauth provider is required` | MEDIUM | Should return error; caller context is a service function that can propagate errors |
| `service/oauth/claude.go` | 61 | `CLAUDE_CLIENT_ID is not configured` | MEDIUM | Config validation should happen at startup, not at request time |
| `service/oauth/codex.go` | 61 | `CODEX_CLIENT_ID is not configured` | MEDIUM | Same as above |
| `service/oauth/gemini_cli.go` | 77, 81 | `GEMINI_CLI_CLIENT_ID/SECRET is not configured` | MEDIUM | Same as above |
| `service/oauth/session.go` | 179 | `crypto/rand.Read failed` | HIGH | Same category as `account_credential.go` -- library code should return errors |

**Summary**: 3 of 8 panic sites are in library-style functions (`EncryptAccountPassword`, session state generation) that should return errors. 4 are config-missing guards that should be caught at startup validation. 1 is a re-panic inside a defer-recover that defeats the purpose of crash resilience.

---

## 2. `init()` Functions (21 total, LOW-MEDIUM)

All 21 `init()` functions follow one of three patterns:

### Pattern A: Platform/OAuth adapter registration (18 files) -- LOW

```go
func init() {
    Register(&SomeAdapter{...})
}
```

These register adapters into a global registry before `main()` runs. This is idiomatic Go for plugin-like registration. **Acceptable**.

| File | Registers |
|---|---|
| `platform/antigravity.go` | AntigravityAdapter |
| `platform/anyrouter.go` | AnyRouterAdapter |
| `platform/claude.go` | ClaudeAdapter |
| `platform/cliproxyapi.go` | CliProxyApiAdapter |
| `platform/codex.go` | CodexAdapter |
| `platform/donehub.go` | DoneHubAdapter |
| `platform/gemini.go` | GeminiAdapter |
| `platform/gemini_cli.go` | GeminiCliAdapter |
| `platform/newapi.go` | NewApiAdapter |
| `platform/oneapi.go` | OneApiAdapter |
| `platform/onehub.go` | OneHubAdapter |
| `platform/openai.go` | OpenAiAdapter |
| `platform/sub2api.go` | Sub2ApiAdapter |
| `platform/veloera.go` | VeloeraAdapter |
| `proxy/detect.go` | Detection profiles + fallback |
| `service/oauth/antigravity.go` | OAuthProviderDefinition |
| `service/oauth/claude.go` | OAuthProviderDefinition |
| `service/oauth/codex.go` | OAuthProviderDefinition |
| `service/oauth/gemini_cli.go` | OAuthProviderDefinition |

### Pattern B: Order-bookkeeping (1 file) -- LOW

`handler/admin/settings_backup.go:86` -- reverses `allTables` for backup ordering. Harmless.

### Pattern C: Import side-effect (1 file) -- MEDIUM

`service/oauth/flow.go:760`:
```go
func init() {
    _ = sqlx.BindType("")
}
```

This calls `sqlx.BindType("")` solely to ensure the `sqlx` import is not flagged as unused. A cleaner approach: use `var _ = sqlx.BindType` in the import block instead, or use a blank import `_ "github.com/jmoiron/sqlx"` if that is the actual intent.

---

## 3. Global Mutable State (7 instances, MEDIUM)

| Variable | File | Mutated By | Risk |
|---|---|---|---|
| `globalCfg` + `cfgMu` | `config/config.go:13-15` | `config.Set()`, `config.Get()` (read) | LOW -- singleton pattern with RWMutex, set-once in practice |
| `globalAccountsCache` | `handler/admin/accounts.go:75` | Inline mutation via `accountsSnapshotCache` methods | LOW -- TTL-cached snapshot, read-heavy |
| `globalSessionStore` | `service/oauth/session.go:134` | `SetSessionStore()` (test injection) | MEDIUM -- mutable global for DI; could use context-based injection |
| `orderedProfiles` + `profileMap` | `proxy/profile.go:28-30` | `RegisterDetectionProfile()`, `RegisterFallbackProfile()` | LOW -- populated via `init()` only, effectively write-once |
| `workflowHooks` | `service/oauth/hooks.go:22` | Setter function | MEDIUM -- global hook registration; typical plugin pattern but untestable in isolation |
| `videoTaskStoreMu` | `handler/proxy/videos.go:25` | Concurrent read/write | LOW -- protected by RWMutex |
| `registry` (platform) | `platform/registry.go:46-51` | `Register()` in `init()` | LOW -- write-once via init |

**Verdict**: Mostly low-risk. The real concern is `globalSessionStore` and `workflowHooks` which make unit testing harder and couple modules through global state. Consider dependency injection.

---

## 4. Magic Numbers (LOW-MEDIUM)

The project does a reasonable job of extracting constants into `config/defaults.go`. However, minimum-value guards in `config/config.go` use raw integers as lower bounds inside `maxInt()` calls:

| File:Line | Raw Number | Context |
|---|---|---|
| `config/config.go:433` | `100` | `maxInt(100, ...)` for `TokenRouterCacheTtlMs` |
| `config/config.go:438` | `30000` | `maxInt(30000, ...)` for `ProxyStickySessionTtlMs` |
| `config/config.go:443` | `5000` | `maxInt(5000, ...)` for `ProxySessionChannelLeaseTtlMs` |
| `config/config.go:444` | `1000` | `maxInt(1000, ...)` for `ProxySessionChannelLeaseKeepaliveMs` |
| `config/config.go:464` | `1024` | `maxInt(1024, ...)` for `ProxyDebugMaxBodyBytes` |
| `config/config.go:478` | `60000` | `maxInt(60000, ...)` for `ModelAvailabilityProbeIntervalMs` |
| `config/config.go:479` | `3000` | `maxInt(3000, ...)` for `ModelAvailabilityProbeTimeoutMs` |
| `router/router.go:42` | `100, 200` | Rate limit: 100 req/s, burst 200 |
| `router/middleware.go:20` | `300` | CORS MaxAge |
| `auth/admin.go:85` | `500` | Status code in error response |
| `auth/downstream.go:85` | `500` | Status code in error response |
| `cmd/migrate/main.go:327` | `100` | Progress report interval |

**Recommendation**: Move lower-bound constants into `config/defaults.go` alongside the existing defaults (e.g., `DefaultTokenRouterCacheTtlMinMs = 100`). The `100, 200` rate-limit values in `router.go` should be named constants.

---

## 5. TODO / FIXME Comments (17 TODO, 0 FIXME -- LOW)

All 17 TODO comments are in non-test code. They are reasonably well-prioritized with P4/P7/P8 labels. No FIXME markers found.

| File | Count | Notes |
|---|---|---|
| `service/site_service.go` | 3 | P4/P7/P8 -- integration with proxy cache, token router cache, route-building pipeline |
| `handler/admin/accounts.go` | 3 | P4 -- sub2api managed auth, expired API key recovery |
| `scheduler/sub2api_refresh.go` | 2 | Parse extraConfig, wire Sub2API refresh |
| `scheduler/channel_recovery.go` | 2 | Wire active candidate loading, actual probe call |
| `scheduler/update_center.go` | 1 | Wire update center polling |
| `scheduler/site_announcement.go` | 1 | Wire platform-specific announcement sync |
| `scheduler/model_probe.go` | 1 | Wire probe execution |
| `handler/proxy/images.go` | 1 | Full multipart upstream forwarding |
| `handler/admin/sites.go` | 1 | Cross-reference with preset registry |
| `transform/shared/chatFormatsCore.go` | 1 | `collectIndexedToolCalls` |
| `service/account_token_service.go` | 1 | `adapter.getUserGroups()` instead of local DB query |

**Verdict**: All TODOs are feature-gap markers, not technical debt flags. No action needed beyond normal backlog tracking.

---

## 6. Commented-Out Code (1 instance -- LOW)

Only one clear case of commented-out production code was found:

`handler/proxy/upstream.go:259`:
```go
//	cloneAndSetModel(ctx.Body) -> json.Marshal
```

This appears to be a documentation note rather than dead code. No other significant commented-out code blocks were detected in non-test files.

---

## 7. Excessively Long Functions (28 non-test functions >100 lines, MEDIUM)

Functions exceeding 100 lines in non-test code:

| Lines | File:Line | Function |
|---|---|---|
| 403 | `handler/admin/settings.go:121` | `updateRuntime` |
| 273 | `transform/shared/chatFormatsCore.go:901` | `NormalizeUpstreamStreamEvent` |
| 250 | `handler/admin/downstream_keys.go:228` | `updateKey` |
| 240 | `service/checkin/checkin.go:165` | `CheckinAccount` |
| 218 | `transform/shared/chatFormatsCore.go:112` | `convertClaudeRequestToOpenAiBody` |
| 211 | `config/config.go:295` | `Load` |
| 207 | `platform/newapi.go:1400` | `parseChallengeXorSeed` |
| 203 | `service/balance/balance.go:332` | `RefreshBalance` |
| 199 | `proxy/endpoint_flow.go:125` | `ExecuteEndpointFlow` |
| 189 | `service/oauth/flow.go:350` | `activatePersistedOAuthAccount` |
| 177 | `handler/admin/sites.go:223` | `updateSite` |
| 163 | `handler/admin/sites.go:57` | `createSite` |
| 162 | `transform/canonical/openai_bridge.go:630` | `CanonicalRequestToOpenAiChatBody` |
| 155 | `service/notify/notify.go:66` | `SendNotification` |
| 154 | `transform/anthropic/messages/conversion.go:106` | `ConvertOpenAiBodyToAnthropicMessagesBody` |
| 154 | `transform/anthropic/messages/conversion.go:296` | `sanitizeAnthropicContentBlock` |
| 153 | `transform/gemini/generate_content/compatibility.go:205` | `BuildGeminiGenerateContentRequestFromOpenAi` |
| 152 | `transform/shared/chatFormatsCore.go:517` | `NormalizeUpstreamFinalResponse` |
| 146 | `config/validate.go:13` | `Validate` |
| 145 | `service/daily/daily_summary.go:38` | `CollectDailySummaryMetrics` |
| 143 | `service/oauth/route_unit.go:153` | `CreateOauthRouteUnit` |
| 139 | `routing/runtime_health.go:1290` | `unmarshalPayload` |
| 137 | `routing/matcher.go:205` | `ParseModelMappingRecord` |
| 135 | `handler/admin/accounts.go:119` | `createAccount` |
| 134 | `routing/router.go:608` | `RecordFailure` |
| 132 | `cmd/migrate/main.go:235` | `runMigration` |
| 128 | `routing/weights.go:592` | `BuildStableFirstPoolPlan` |
| 123 | `transform/gemini/generate_content/compatibility.go:78` | `BuildOpenAiBodyFromGeminiRequest` |

**Top 3 priorities for refactoring**:
1. `handler/admin/settings.go:121` `updateRuntime` (403 lines) -- admin handler with extensive field-by-field patching; extract field-set helpers
2. `transform/shared/chatFormatsCore.go` -- both `NormalizeUpstreamStreamEvent` (273) and `convertClaudeRequestToOpenAiBody` (218) are transformer functions that could be split into format-specific sub-functions
3. `platform/newapi.go` `parseChallengeXorSeed` (207) -- XOR seed extraction from HTML; extract helper functions for each parsing stage

---

## 8. Deep Nesting (>4 levels, MEDIUM)

Files with nesting depth >= 5 (excluding test files):

| Depth | File:Line |
|---|---|
| **8** | `platform/newapi.go:547` |
| **8** | `routing/runtime_health.go:1062` |
| **8** | `transform/gemini/generate_content/compatibility.go:267` |
| **7** | `proxy/endpoint_flow.go:238` |
| **7** | `service/oauth/codex.go:301` |
| **6** | `handler/admin/settings.go:437` |
| **6** | `handler/proxy/multipart.go:76` |
| **6** | `platform/gemini.go:47` |
| **6** | `proxy/failure_judge.go:177` |
| **6** | `scheduler/scheduler.go:41` |
| **6** | `service/oauth/gemini_cli.go:562` |
| **6** | `transform/anthropic/messages/conversion.go:131` |
| **6** | `transform/canonical/openai_bridge.go:642` |
| **6** | `transform/shared/chatFormatsCore.go:216` |
| 5 | 16 additional files |

**Root cause**: The deep nesting is predominantly in API response parsing (JSON `map[string]any` traversal) and multi-condition validation chains. The typical pattern is:

```go
if resp, err := fetch(...); err == nil {
    if success, _ := getBool(resp, "success"); success {
        if data, ok := getMap(resp, "data"); ok {
            if nested, ok := data["field"].(map[string]any); ok {
                // depth 5+
            }
        }
    }
}
```

**Recommendation**: Apply early-return inversion:
```go
resp, err := fetch(...)
if err != nil {
    return nil, err
}
success, _ := getBool(resp, "success")
if !success {
    return nil, nil
}
data, ok := getMap(resp, "data")
if !ok {
    return nil, nil
}
```

This flattens the pyramid and reduces cognitive load. The `platform/newapi.go:540-560` block is a textbook case -- 5 levels of nesting that can be collapsed to 2 with early returns.

---

## 9. Additional Patterns Found

### 9.1 Ignored Error Returns (LOW)

Multiple instances of `_ = expr` and `_, _ = expr` (non-test). Most are in routing decision logging (acceptable) but a few merit attention:

- `auth/admin.go:283` -- `_ = json.NewEncoder(w).Encode(body)` -- writing the response body and ignoring the error means a partial write could go undetected
- `cmd/server/main.go:20` -- `_ = godotenv.Load()` -- dotenv failure is silently ignored; should at least log a warning
- `proxy/channel_selection.go:210` -- `_, _ = refreshForFirstAttempt()` -- dual ignore, unclear if both return values are truly discardable

### 9.2 Hardcoded Credential Secret (MEDIUM)

`service/account_credential.go:27`:
```go
secret = "change-me-admin-token" // fallback matches TS config defaults
```

This fallback hardcodes the default admin token. If a deployment forgets to set `AccountCredentialSecret` and `AuthToken`, the encryption key becomes the SHA-256 of a well-known string. Should log a warning when this fallback is activated.

### 9.3 Duplicate `clampInt`/`maxInt` Definitions (LOW)

`clampInt` and `maxInt` are defined in two packages:
- `config/config.go:520-540`
- `handler/admin/search.go:187-197`

These should be consolidated into a shared `internal/mathutil` or similar package.

---

## Summary

| Category | Count | Severity | Action Needed |
|---|---|---|---|
| panic() calls | 8 (non-test) | HIGH | Refactor 3 library panics to error returns; 4 config-missing panics to startup validation |
| init() functions | 21 | LOW | All follow idiomatic registration patterns; 1 `sqlx.BindType` hack worth cleaning |
| Global mutable state | 7 | MEDIUM | `globalSessionStore` and `workflowHooks` are untestable; consider DI |
| Magic numbers | ~12 | LOW-MEDIUM | Move lower-bound constants to `defaults.go` |
| TODO comments | 17 | LOW | All are feature-gap markers, not tech debt |
| FIXME comments | 0 | -- | None found |
| Commented-out code | 1 | LOW | Appears to be a documentation note |
| Long functions (>100 lines) | 28 | MEDIUM | Top 3 candidates for refactoring: `updateRuntime`, `NormalizeUpstreamStreamEvent`, `parseChallengeXorSeed` |
| Deep nesting (>4 levels) | 32 files | MEDIUM | Apply early-return inversion to API response parsers |
| Ignored errors | ~28 | LOW | Review `json.Encode` and `godotenv.Load` cases |
| Hardcoded secret | 1 | MEDIUM | Add warning log when fallback is used |
| Duplicate utilities | 2 functions | LOW | Consolidate `clampInt`/`maxInt` |
