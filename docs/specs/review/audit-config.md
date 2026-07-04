# Config Hardening Audit: metapi-go/config

**Audit date**: 2026-07-04  
**Scope**: `D:/Code/TokenDance/metapi-go/config/config.go`, `defaults.go`; related files `store/settings.go`, `handler/admin/settings.go`, `cmd/server/main.go`

---

## Summary

| Dimension | Verdict | Severity |
|-----------|---------|----------|
| Env var validation (port, URL, cron) | **MISSING** at load time | HIGH |
| Startup validation step | **NONE** | HIGH |
| Conflicting / insecure defaults | **PRESENT** | HIGH |
| Settings DB override correctness | **PARTIAL** (incomplete coverage, asymmetry) | MEDIUM |

---

## 1. No Startup Config Validation

`config.Load(env)` (config.go:295) builds a `*Config` and returns it. There is **no `Validate()` method** on `Config`, and `cmd/server/main.go:26-27` calls `Load` then `Set` without any post-load validation.

```go
// main.go:26-27 — no validation step
cfg := config.Load(env)
config.Set(cfg)
```

Consequences:
- Invalid port numbers, empty cron expressions, empty URLs for enabled notifiers, and unreasonably large timeout values all pass through silently.
- The server will start with broken configuration and fail at runtime (possibly after serving partial traffic) rather than failing fast at startup.

**Recommendation**: Add a `func (c *Config) Validate() []error` method and call it in `main.go` immediately after `config.Load`. On validation errors, log and exit before binding the port.

---

## 2. Missing / Insufficient Env Var Validation

### 2.1 Port (config.go:355) — NO range check

```go
cfg.Port = int(math.Trunc(parseNumber(get("PORT"), DefaultPort)))
```

`parseNumber` rejects NaN/Inf but passes **any finite float**. Negative ports, port 0, port 999999 are all accepted. No check for [1, 65535].

**Severity**: HIGH. A negative port or port 0 would cause `net.Listen` to fail at a confusing point in startup. Port >65535 silently wraps.

### 2.2 Cron expressions (config.go:364-376) — NOT validated at load time

```go
cfg.CheckinCron        = firstNonEmpty(get("CHECKIN_CRON"), DefaultCheckinCron)
cfg.BalanceRefreshCron = firstNonEmpty(get("BALANCE_REFRESH_CRON"), DefaultBalanceRefreshCron)
cfg.LogCleanupCron     = firstNonEmpty(get("LOG_CLEANUP_CRON"), DefaultLogCleanupCron)
```

Cron validation (`scheduler.ValidateCronExpr`) only happens **lazily** when schedulers start (checkin.go:132, balance.go:63, log_cleanup.go:68). Invalid env var cron expressions are stored in the config and only detected later, with the scheduler silently falling back to the default. This means the user's intent is silently ignored.

**Severity**: MEDIUM. Not a crash, but silent fallback violates principle of least surprise.

### 2.3 URL fields — NO format validation

The following fields accept any string, including empty strings, without format validation:

| Field | Env var | Default-enabled? |
|-------|---------|------------------|
| `WebhookUrl` | `WEBHOOK_URL` | Yes (`WebhookEnabled` defaults to `true`) |
| `BarkUrl` | `BARK_URL` | Yes (`BarkEnabled` defaults to `true`) |
| `ServerChanKey` | `SERVERCHAN_KEY` | Yes (`ServerChanEnabled` defaults to `true`) |
| `SystemProxyUrl` | `SYSTEM_PROXY_URL` | No |
| `DbUrl` | `DB_URL` | N/A |
| `TelegramApiBaseUrl` | (hardcoded to `https://api.telegram.org`) | N/A |

**Severity**: HIGH for webhook/bark/serverchan. The combination of "enabled by default" + "empty URL" means notification attempts will fail at runtime with confusing errors (connection refused, malformed URL, etc.).

### 2.4 SMTP port (config.go:407) — NO range check

```go
cfg.SmtpPort = atoiOr(get("SMTP_PORT"), DefaultSmtpPort)
```

`atoiOr` only checks parseability of the string, not whether the value is in [1, 65535]. Same issue as Port.

### 2.5 AuthToken — INSECURE default with no enforcement

```go
cfg.AuthToken = firstNonEmpty(get("AUTH_TOKEN"), DefaultAuthToken)
// DefaultAuthToken = "change-me-admin-token"
```

The system warns when `AccountCredentialSecret` uses the default (config.go:343-345), but **no warning is emitted when `AuthToken` itself uses the default**. An unconfigured deployment ships with `AuthToken = "change-me-admin-token"`, granting full admin access.

**Severity**: HIGH. Trivially guessable admin token in production.

### 2.6 RequestBodyLimit — env var IGNORED

```go
cfg.RequestBodyLimit = DefaultRequestBodyLimit  // hardcoded, line 422
```

No env var is read for `RequestBodyLimit`. The value is unconditionally 20 MB. This field is also absent from the `updateRuntime` API and the `ApplyRuntimeSettings` DB override path.

**Severity**: LOW. More of a feature gap than a hardening issue.

### 2.7 GlobalBlockedBrands / GlobalAllowedModels — hardcoded empty

```go
cfg.GlobalBlockedBrands = []string{}   // line 452
cfg.GlobalAllowedModels = []string{}   // line 453
```

No env var path exists. These can only be set via the runtime settings API (which persists to DB and later reloads via `ApplyRuntimeSettings`). If the DB is unavailable or settings haven't been loaded yet, these are empty.

**Severity**: LOW. Design choice, but a `GLOBAL_BLOCKED_BRANDS` / `GLOBAL_ALLOWED_MODELS` env var would be useful for immutable bootstrap.

### 2.8 TZ — accepted without validation

```go
cfg.Tz = get("TZ")  // line 361
```

Empty string (or garbage like `"Mars/Olympus"`) is accepted. An invalid timezone causes `time.LoadLocation` failures at point of use.

**Severity**: LOW. Fails at point of use with a clear error.

---

## 3. Conflicting / Insecure Defaults

### 3.1 Notifiers enabled with empty targets

| Field | Default enabled | Default target |
|-------|----------------|----------------|
| `WebhookEnabled` | `true` | `""` (empty) |
| `BarkEnabled` | `true` | `""` (empty) |
| `ServerChanEnabled` | `true` | `""` (empty) |

A fresh deployment with no env vars will attempt to fire webhook/bark/serverchan notifications with empty URLs/keys. Each attempt will fail, potentially generating cascading error logs.

**Recommendation**: Default notifier `Enabled` fields to `false`, OR validate at startup that enabled notifiers have non-empty targets.

### 3.2 Hardcoded OAuth client credentials

```go
DefaultCodexClientId   = "CODEX_CLIENT_ID_PLACEHOLDER"    // real-looking client ID
DefaultClaudeClientId  = "CLAUDE_CLIENT_ID_PLACEHOLDER"  // real-looking UUID
DefaultGeminiCliClientId     = "GEMINI_CLI_CLIENT_ID_PLACEHOLDER"
DefaultGeminiCliClientSecret = "GEMINI_CLI_CLIENT_SECRET_PLACEHOLDER"
```

The Codex and Claude client IDs appear to be real OAuth client IDs checked into source code. If these are production credentials, they are **leaked**. If they are test/demo credentials, they should be clearly labeled as such (the Gemini ones use `_PLACEHOLDER` but the others do not).

**Severity**: HIGH. Potential credential leak.

### 3.3 AccountCredentialSecret fallback chain

```go
resolveAccountCredentialSecret := func() string {
    if v := get("ACCOUNT_CREDENTIAL_SECRET"); v != "" { return v }
    if v := get("AUTH_TOKEN"); v != "" { return v }
    return DefaultAuthToken  // "change-me-admin-token"
}
```

If neither `ACCOUNT_CREDENTIAL_SECRET` nor `AUTH_TOKEN` is set, the account credential secret becomes `"change-me-admin-token"`. This means the encryption key for stored account credentials is trivially guessable.

**Severity**: HIGH. Should require explicit configuration or generate a random secret at startup.

### 3.4 Routing weights accept any float without normalization

```go
BaseWeightFactor:  parseNumber(get("BASE_WEIGHT_FACTOR"), 0.5),
ValueScoreFactor:  parseNumber(get("VALUE_SCORE_FACTOR"), 0.5),
CostWeight:        parseNumber(get("COST_WEIGHT"), 0.4),
BalanceWeight:     parseNumber(get("BALANCE_WEIGHT"), 0.3),
UsageWeight:       parseNumber(get("USAGE_WEIGHT"), 0.3),
```

Negative weights are accepted. No normalization (e.g., making them sum to 1). The `updateRuntime` API also accepts any float with no range check.

**Severity**: MEDIUM. Negative weights could cause undefined routing behavior.

---

## 4. Settings DB Override Mechanism: Partial and Asymmetric

### 4.1 Incomplete field coverage

`ApplyRuntimeSettings` (store/settings.go:82) handles only a subset of config fields. The following fields are settable in `config.Load` but **NOT** overridable from DB settings:

| Field | Load-time env var | DB-overridable? |
|-------|-------------------|-----------------|
| `RequestBodyLimit` | (none, hardcoded) | No |
| `RoutingFallbackUnitCost` | `ROUTING_FALLBACK_UNIT_COST` | No |
| `ProxyFirstByteTimeoutSec` | `PROXY_FIRST_BYTE_TIMEOUT_SEC` | No |
| `TokenRouterFailureCooldownMaxSec` | `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC` | No |
| `TokenRouterCacheTtlMs` | `TOKEN_ROUTER_CACHE_TTL_MS` | No |
| `ProxyStickySessionEnabled` | `PROXY_STICKY_SESSION_ENABLED` | No |
| `ProxyStickySessionTtlMs` | `PROXY_STICKY_SESSION_TTL_MS` | No |
| `ProxySessionChannelLeaseTtlMs` | `PROXY_SESSION_CHANNEL_LEASE_TTL_MS` | No |
| `ProxySessionChannelLeaseKeepaliveMs` | `PROXY_SESSION_CHANNEL_LEASE_KEEPALIVE_MS` | No |
| `NotifyCooldownSec` | `NOTIFY_COOLDOWN_SEC` | No |
| `SystemProxyUrl` | `SYSTEM_PROXY_URL` | No |
| `RoutingWeights` (all 5) | various | No |
| `DbSsl` | `DB_SSL` | No |
| `Tz` | `TZ` | No |
| `ListenHost` | `HOST` | No |
| `DataDir` | `DATA_DIR` | No |
| `DbType` | `DB_TYPE` | No |
| `DbUrl` | `DB_URL` | No |

### 4.2 Validation asymmetry: Load vs updateRuntime vs ApplyRuntimeSettings

The same field can be set through three paths, each with different validation:

| Path | Validation |
|------|-----------|
| `config.Load` (env vars) | Minimal: NaN/Inf rejection, some clamps |
| `handler/admin/settings.go updateRuntime` (API) | Range checks, "sk-" prefix, cron validation |
| `store/settings.go ApplyRuntimeSettings` (DB reload) | **No validation at all** — `parseInt` has no range check |

**Critical example**: `ProxyToken` in `updateRuntime` requires "sk-" prefix; `ApplyRuntimeSettings` applies it with no check. A malformed token written directly to the DB would pass through silently on next reload.

### 4.3 DB reload path does not call ValidateCronExpr

`scheduler/settings.go:resolveCronSetting` validates cron expressions when individual schedulers read their settings, but `ApplyRuntimeSettings` does not. The DB can contain invalid cron expressions that persist until a scheduler restart.

### 4.4 `parseInt` in store/settings.go allows negative values

```go
func parseInt(value string, fallback int) int {
    v, err := strconv.Atoi(strings.TrimSpace(value))
    if err != nil { return fallback }
    return v  // no range check
}
```

This is used for `port`, `smtp_port`, `proxy_max_channel_attempts`, and others. A negative port stored in the DB becomes the live port number.

**Severity**: MEDIUM. Requires DB write access, but the blast radius is large.

---

## 5. Other Observations

### 5.1 `parseNumber` silently truncates floats to ints

Every numeric config field goes through `parseNumber` (returns float64) then `int(math.Trunc(...))`. If an env var contains `"3.7"`, it becomes `3`, not `4`. No warning is emitted for the truncation.

### 5.2 `ListenHost` reads env map directly, not via `get` helper

```go
cfg.ListenHost = parseListenHost(env)  // line 356
```

`parseListenHost` accesses `env["HOST"]` directly, bypassing the `get` closure used everywhere else. This is inconsistent but functionally equivalent since `get` is just a map access.

### 5.3 `TelegramApiBaseUrl` is NOT overridable from env vars

```go
cfg.TelegramApiBaseUrl = TelegramApiBaseUrl  // hardcoded to "https://api.telegram.org"
```

No `get("TELEGRAM_API_BASE_URL")` call. The `updateRuntime` API CAN change it, and `ApplyRuntimeSettings` does NOT map any key to it. So the only way to set a custom Telegram API URL is via the admin API at runtime. This means custom Telegram endpoints (e.g., self-hosted bot API) require manual API calls after each restart.

### 5.4 `LogCleanupConfigured` is always reset to `false` at Load

```go
cfg.LogCleanupConfigured = false  // line 382, comment: "set later during runtime settings hydration"
```

This is intentional (it gets set by `LoadRuntimeSettings`), but means any code that checks this field between `config.Load` and `store.LoadRuntimeSettings` will see `false`. Currently this window is small (both happen in `main.go` before the router is created), but it is fragile.

### 5.5 Config singleton panics on early access

```go
func Get() *Config {
    if globalCfg == nil {
        panic("config.Get() called before config.Set() — load config first")
    }
    return globalCfg
}
```

Any code path that calls `config.Get()` before `config.Set()` panics. This is documented but harsh. The existing `main.go` does `Load` then `Set` before anything else, so the current code is safe.

---

## 6. Findings Summary (ranked by severity)

### HIGH

1. **No startup validation step** — server starts with invalid config; fails at runtime instead of fast-failing.
2. **Port not range-checked** (config.go:355) — negative/zero/overflow port silently accepted.
3. **AuthToken defaults to trivially guessable value** with no warning (only `AccountCredentialSecret` gets a warning).
4. **AccountCredentialSecret falls back to DefaultAuthToken** ("change-me-admin-token") making stored credentials decryptable.
5. **Hardcoded real-looking OAuth client credentials** in defaults.go (Codex, Claude).
6. **Notifiers enabled by default with empty targets** — webhook, bark, serverchan.
7. **DB reload path (`ApplyRuntimeSettings`) has zero validation** — bypasses all range/format checks that the API path enforces.

### MEDIUM

8. **Cron expressions not validated at load time** — invalid env var values silently fall back to defaults.
9. **URL fields not format-validated** (WebhookUrl, BarkUrl, DbUrl, SystemProxyUrl).
10. **Routing weights accept negative values** with no normalization.
11. **`parseInt` in store/settings.go has no range check** — negative ports/limits possible via DB.
12. **Validation asymmetry** across env-load, API-update, and DB-reload paths.
13. **`RequestBodyLimit` hardcoded** — env var ignored, not in DB override map, not in API.

### LOW

14. **`parseNumber` silently truncates floats** for integer fields.
15. **`TelegramApiBaseUrl` not overridable from env vars.**
16. **`GlobalBlockedBrands` / `GlobalAllowedModels` hardcoded to empty** — no env var path.
17. **`Tz` accepted without validation** — invalid timezone fails at point of use.
18. **`LogCleanupConfigured` race window** — fragile ordering dependency.
