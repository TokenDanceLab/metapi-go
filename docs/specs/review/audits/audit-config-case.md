# Config Case Sensitivity Audit: metapi-go/config

**Audit date**: 2026-07-05
**Scope**: `D:/Code/TokenDance/metapi-go/config/config.go` (Load function), `cmd/server/main.go` (environMap + godotenv), `D:/Code/TokenDance/metapi-go/docs/specs/p0-skeleton.md` (spec)

---

## Summary

| Dimension | Verdict | Severity |
|-----------|---------|----------|
| Env var lookups are case-sensitive | **CONFIRMED** -- Go map access, no normalization | HIGH |
| Lowercase / mixed-case env vars silently ignored | **CONFIRMED** -- fall through to defaults with zero warning | HIGH |
| Documented anywhere | **NOWHERE** -- not in spec, code comments, or README | MEDIUM |

---

## 1. Root Cause: Go Map Lookup with Exact UPPER_SNAKE_CASE Keys

`config.Load(env map[string]string)` (config.go:295) defines a `get` closure:

```go
get := func(key string) string {
    return env[key]
}
```

Every env var lookup uses exact UPPER_SNAKE_CASE string literals:

```go
cfg.CheckinCron = firstNonEmpty(get("CHECKIN_CRON"), DefaultCheckinCron)   // line 364
cfg.Port        = int(math.Trunc(parseNumber(get("PORT"), DefaultPort)))    // line 355
cfg.AuthToken   = firstNonEmpty(get("AUTH_TOKEN"), DefaultAuthToken)        // line 339
```

Go `map[string]string` access is **strictly case-sensitive**. There is no `strings.ToUpper()` normalization on either the key side or the map-construction side. This means `CHECKIN_CRON`, `checkin_cron`, `Checkin_Cron`, and `Checkin_cron` are four **different** keys -- only the first one is recognized.

This applies to all 94 config entries enumerated in `docs/specs/p0-skeleton.md` section 3. None of them is case-insensitive.

---

## 2. How the Env Map Is Built (No Normalization)

### 2.1 `environMap()` -- preserves OS case (cmd/server/main.go:117-128)

```go
func environMap() map[string]string {
    env := make(map[string]string)
    for _, kv := range os.Environ() {
        for i := 0; i < len(kv); i++ {
            if kv[i] == '=' {
                env[kv[:i]] = kv[i+1:]   // <-- raw split, no ToUpper
                break
            }
        }
    }
    return env
}
```

`os.Environ()` returns `KEY=VALUE` strings in whatever case the OS provides them. No normalization is performed.

### 2.2 `godotenv.Load()` -- preserves .env file case (cmd/server/main.go:20)

```go
_ = godotenv.Load()
```

By default, `github.com/joho/godotenv` preserves the exact case written in the `.env` file. If the file contains `checkin_cron=*/30 * * * *`, the resulting environment variable key is `checkin_cron` (lowercase), not `CHECKIN_CRON`.

### 2.3 Combined effect

| Source | Input | Key in `env` map | Matched by `get("CHECKIN_CRON")`? |
|--------|-------|-------------------|------------------------------------|
| `.env` file | `CHECKIN_CRON=...` | `CHECKIN_CRON` | YES |
| `.env` file | `checkin_cron=...` | `checkin_cron` | NO |
| `.env` file | `Checkin_Cron=...` | `Checkin_Cron` | NO |
| `os.Environ()` (Linux `export`) | `CHECKIN_CRON=...` | `CHECKIN_CRON` | YES |
| `os.Environ()` (Linux `export`) | `checkin_cron=...` | `checkin_cron` | NO |
| `os.Environ()` (Windows `set`) | `CHECKIN_CRON=...` | `CHECKIN_CRON` | YES |
| `os.Environ()` (Windows `set`) | `checkin_cron=...` | `checkin_cron` | NO |
| Docker `--env` / `environment:` | `CHECKIN_CRON=...` | `CHECKIN_CRON` | YES |
| Docker `--env` / `environment:` | `checkin_cron=...` | `checkin_cron` | NO |

---

## 3. Failure Mode: Silent Fallback to Default

When a lowercase env var is used, the behavior is **silent fallback to the default value** with no warning, error, or log message.

### Concrete test case

```go
// User intends to set a custom checkin cron
cfg := config.Load(map[string]string{
    "checkin_cron": "*/30 * * * *",   // lowercase -- user's intent
})

// Result:
cfg.CheckinCron         // "0 8 * * *"        <-- DEFAULT, not "*/30 * * * *"
cfg.CheckinScheduleMode // "cron"              <-- DEFAULT
cfg.CheckinIntervalHours // 6                   <-- DEFAULT
```

The same applies to every field. For boolean fields the effect is especially dangerous:

```go
cfg := config.Load(map[string]string{
    "webhook_enabled": "false",    // user thinks they disabled it
})
// cfg.WebhookEnabled == true      // DEFAULT (true) -- webhook is ON
```

### For fields with empty-string defaults, the failure is invisible

```go
cfg := config.Load(map[string]string{
    "db_url": "postgres://...",   // user's actual DB
})
// cfg.DbUrl == ""                // EMPTY -- will fail at DB connect time
```

### Affected fields by category

| Category | Count | Example env var | Default if case-mismatched |
|----------|-------|-----------------|---------------------------|
| Auth | 5 | `AUTH_TOKEN` | `"change-me-admin-token"` (insecure) |
| OAuth | 4 | `CLAUDE_CLIENT_ID` | Hardcoded real-looking UUID |
| Server | 7 | `PORT` | 4000 |
| Cron | 5 | `CHECKIN_CRON` | `"0 8 * * *"` |
| Log Cleanup | 4 | `LOG_CLEANUP_RETENTION_DAYS` | 30 |
| Notify: Webhook | 2 | `WEBHOOK_URL` | `""` (empty, enabled by default) |
| Notify: Bark | 2 | `BARK_URL` | `""` (empty, enabled by default) |
| Notify: ServerChan | 2 | `SERVERCHAN_KEY` | `""` (empty, enabled by default) |
| Notify: Telegram | 6 | `TELEGRAM_BOT_TOKEN` | `""` (empty) |
| Notify: SMTP | 8 | `SMTP_HOST` | `""` (empty) |
| Notify: General | 2 | `NOTIFY_COOLDOWN_SEC` | 300 |
| Admin | 1 | `ADMIN_IP_ALLOWLIST` | `[]` (empty) |
| Proxy: Core | 2 | `ROUTING_FALLBACK_UNIT_COST` | 1.0 |
| Proxy: Token Router | 2 | `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC` | 30-day ceiling |
| Proxy: Channel | 3 | `PROXY_MAX_CHANNEL_ATTEMPTS` | 3 |
| Proxy: Session | 4 | `PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT` | 2 |
| Proxy: Misc | 6 | `PROXY_ERROR_KEYWORDS` | `[]` (empty) |
| Proxy: Debug | 9 | `PROXY_DEBUG_TRACE_ENABLED` | false |
| Codex-specific | 3 | `CODEX_RESPONSES_WEBSOCKET_BETA` | `"responses_websockets=2026-02-06"` |
| Model Probe | 4 | `MODEL_AVAILABILITY_PROBE_ENABLED` | false |
| Retention | 4 | `PROXY_LOG_RETENTION_DAYS` | 30 |
| Routing Weights | 5 | `BASE_WEIGHT_FACTOR` | 0.5 |
| Payload Rules | 2 | `PAYLOAD_RULES` | nil |

---

## 4. Cross-Platform Risk Amplification

### 4.1 Windows

Windows environment variables are case-insensitive at the OS level (`set CHECKIN_CRON` and `set checkin_cron` target the same variable), but `os.Environ()` returns the case that was used to **set** the variable. A variable set as `set checkin_cron=...` will appear in `os.Environ()` as `checkin_cron=...` and will NOT match `CHECKIN_CRON`.

### 4.2 Docker / docker-compose

Docker environment variables preserve case as written in the compose file. Both `CHECKIN_CRON` and `checkin_cron` are valid compose keys and will be passed through to the container as-is.

### 4.3 Kubernetes ConfigMap / Secret

Kubernetes ConfigMap keys preserve case. It is common to see lowercase or camelCase keys in ConfigMaps, especially when generated by Helm or Kustomize.

### 4.4 systemd EnvironmentFile

systemd `EnvironmentFile=` preserves the case from the file. Lowercase keys in systemd env files are not uncommon in some conventions.

---

## 5. Documentation Gap

The env var case requirement is **not documented anywhere**:

| Source | Mentions case requirement? |
|--------|---------------------------|
| `config/config.go` comments | No -- says "maps 1:1 to TS config field" but does not state UPPER_SNAKE_CASE requirement |
| `cmd/server/main.go` | No |
| `docs/specs/p0-skeleton.md` | Uses UPPER_SNAKE_CASE in all 94 field entries, but never states it is required |
| `AGENTS.md` | No |
| Existing `audit-config.md` (2026-07-04) | Does not mention case sensitivity |
| README or other docs | Not checked (no README found in metapi-go root) |

The spec silently assumes UPPER_SNAKE_CASE without warning that deviation causes silent failures.

---

## 6. Recommendations (ranked by impact-to-effort ratio)

### 6.1 IMMEDIATE: Normalize keys in `environMap()` (cmd/server/main.go:117)

Add `strings.ToUpper()` to every key when building the env map. This is a one-line change with zero risk:

```go
func environMap() map[string]string {
    env := make(map[string]string)
    for _, kv := range os.Environ() {
        for i := 0; i < len(kv); i++ {
            if kv[i] == '=' {
                env[strings.ToUpper(kv[:i])] = kv[i+1:]  // <-- normalize
                break
            }
        }
    }
    return env
}
```

**Caveat**: On Windows where env vars are case-insensitive at the OS level, this could create collisions if both `CHECKIN_CRON` and `checkin_cron` exist (they are the same OS variable, but `os.Environ()` sometimes lists them twice). Add a warning log for duplicate keys after uppercasing.

### 6.2 IMMEDIATE: Normalize godotenv keys

Use `godotenv.Load()` with explicit filename(s) and then normalize the loaded map. Or use the lower-level `godotenv.Read()` and normalize keys before merging into the env map.

### 6.3 HIGH: Normalize in `config.Load()` itself

Add normalization inside `Load()` as a defense-in-depth measure, so even if the caller passes a non-normalized map, it still works:

```go
func Load(env map[string]string) *Config {
    normalized := make(map[string]string, len(env))
    for k, v := range env {
        normalized[strings.ToUpper(k)] = v
    }
    env = normalized

    get := func(key string) string {
        return env[key]
    }
    // ... rest unchanged
}
```

### 6.4 MEDIUM: Log unrecognized env vars

After loading, scan the env map for keys that are NOT in the known set of 94+ config keys. This would catch typos and case errors. List of known keys is already fully enumerated in `docs/specs/p0-skeleton.md`.

### 6.5 MEDIUM: Document the requirement

Add to `config/config.go` package doc, the AGENTS.md, and the spec:

> All environment variable keys MUST be UPPER_SNAKE_CASE. Lowercase or mixed-case variants are not recognized and will silently fall back to default values.

---

## 7. Test Plan

A test to verify case-insensitive loading should be added to `config/config_test.go` (which does not currently exist -- the spec mentions it as planned in p0-skeleton.md line 636):

```go
func TestLoadCaseInsensitive(t *testing.T) {
    // Lowercase keys should work identically to uppercase
    cfgUpper := Load(map[string]string{
        "CHECKIN_CRON": "*/15 * * * *",
        "PORT":         "8080",
    })
    cfgLower := Load(map[string]string{
        "checkin_cron": "*/15 * * * *",
        "port":         "8080",
    })
    // Without normalization, these will DIFFER (lowercase falls to defaults)
    // With normalization, they should be identical
}
```

---

## 8. Relationship to TS Original

The TypeScript `metapi` original (at `D:\Code\TokenDance\metapi`) uses `process.env.CHECKIN_CRON` which is case-sensitive on Linux but case-insensitive on Windows. The Go port inherits the same variable names but adds the extra layer of `map[string]string` lookup which is always case-sensitive regardless of platform, making this a **regression** compared to the TS version on Windows.
