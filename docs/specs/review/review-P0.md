# P0 Cross-Reference Review: Go Spec vs TypeScript Source

**Reviewed**: 2026-07-04
**Spec**: `D:/Code/TokenDance/metapi-go/docs/specs/p0-skeleton.md`
**TS Sources**: `config.ts` (185 lines), `index.ts` (309 lines), `.env.example` (31 lines), `Dockerfile.slim` (35 lines)

---

## Accuracy Issues

### A1. Config surface is claimed as "完整映射" but is effectively absent

The spec states "Config 完整映射" and "所有 env var 保持与 TS 版完全一致的命名 (不添加前缀)". However, the spec does not enumerate a single config field. The actual TS `buildConfig()` returns an object with **88+ fields** spanning auth, notifications (webhook/bark/serverchan/telegram/smtp), proxy, routing weights, debugging, model probing, retention, OAuth, payload rules, and more. The spec only mentions 5 env vars by name (PORT, DB_TYPE, DB_URL, CHECKIN_INTERVAL_HOURS, TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC) and only in the Edge Cases section. Without a full mapping, the claim of "完整映射" is false.

**Severity**: BLOCKING -- the config struct cannot be implemented from this spec.

### A2. Chi middleware ordering mapped onto Fastify source without translation notes

The spec prescribes a Chi middleware chain (`Logger` / `Recoverer` / `RealIP` / `corsMiddleware` / `auth.AdminAuth` / `auth.ProxyAuth`) as though it directly ports from TS. In reality, the TS source uses Fastify with:
- `logger: true` in server options (not middleware)
- `trustProxy: true` (not `RealIP` middleware)
- `@fastify/cors` plugin (not hand-written corsMiddleware)
- `app.addHook('onRequest', ...)` for auth (not per-route-group middleware)

This is a framework-level design decision, not a bug per se, but the spec presents these Chi choices as derived from TS when they are actually new architectural decisions that diverge from the source. The spec should explicitly call out that these are **design translations**, not direct ports.

**Severity**: MEDIUM -- implementable but misleading about provenance.

### A3. "CORS allow all origins" is factually wrong about the TS source

The spec says `corsMiddleware // CORS allow all origins`. The TS source uses `app.register(cors)` from `@fastify/cors` with **no options**, which defaults to `origin: "*"` only when no `Origin` header is present (same-origin requests). For cross-origin requests with an `Origin` header, the default behavior **does not** return `Access-Control-Allow-Origin: *` without explicit configuration. The claim that TS "allows all origins" is inaccurate -- either the default is stricter than claimed, or there is an undocumented override.

**Severity**: MEDIUM -- the Go implementation may end up more permissive than the TS source.

### A4. `/health` endpoint does not exist in TS source

The spec adds `r.Get("/health", app.Health)` and Acceptance Criterion 2 expects `curl :4000/health` returning `{"status":"ok"}`. No dedicated `/health` route exists in `index.ts` or the 18+ registered route modules. The TS SPA fallback would serve `index.html` for `/health`. This is a **net-new addition** to the Go rewrite -- it may be a good idea, but the spec presents it as if it is a port when it is not.

**Severity**: LOW -- the addition is fine, but the provenance claim is wrong.

### A5. `/v1` route auth pattern does not match TS

The spec shows `/v1` routes wrapped in `r.Use(auth.ProxyAuth)` for "managed key / global proxy token". In TS, proxy routes are registered via `app.register(proxyRoutes)` and handle their own auth internally. The TS `/v1` routes are not explicitly namespaced with a dedicated auth middleware in `index.ts` -- auth is self-contained within the proxy router. The spec's pattern may still work but is a design choice, not a port.

**Severity**: LOW -- architecturally valid, just not mirroring TS structure.

---

## Missing Details

### M1. Config struct field enumeration (88+ fields missing)

No config field is listed in the spec. At minimum, the following groups must be added:

| Group | Fields (TS env var names) |
|-------|--------------------------|
| Auth | `AUTH_TOKEN`, `PROXY_TOKEN`, `DEPLOY_HELPER_TOKEN` (and legacy `UPDATE_CENTER_HELPER_TOKEN`), `ACCOUNT_CREDENTIAL_SECRET` |
| OAuth clients | `CODEX_CLIENT_ID`, `CLAUDE_CLIENT_ID`, `CLAUDE_CLIENT_SECRET`, `GEMINI_CLI_CLIENT_ID`, `GEMINI_CLI_CLIENT_SECRET` |
| Server | `PORT`, `HOST`, `DATA_DIR`, `DB_TYPE`, `DB_URL`, `DB_SSL`, `TZ` |
| Cron | `CHECKIN_CRON`, `CHECKIN_SCHEDULE_MODE`, `CHECKIN_INTERVAL_HOURS`, `BALANCE_REFRESH_CRON`, `LOG_CLEANUP_CRON` |
| Log cleanup | `LOG_CLEANUP_USAGE_LOGS_ENABLED`, `LOG_CLEANUP_PROGRAM_LOGS_ENABLED`, `LOG_CLEANUP_RETENTION_DAYS`, `logCleanupConfigured` (derived) |
| Notify: Webhook | `WEBHOOK_URL`, `WEBHOOK_ENABLED` |
| Notify: Bark | `BARK_URL`, `BARK_ENABLED` |
| Notify: ServerChan | `SERVERCHAN_KEY`, `SERVERCHAN_ENABLED` |
| Notify: Telegram | `TELEGRAM_ENABLED`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `TELEGRAM_USE_SYSTEM_PROXY`, `TELEGRAM_MESSAGE_THREAD_ID` |
| Notify: SMTP | `SMTP_ENABLED`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_SECURE`, `SMTP_USER`, `SMTP_PASS`, `SMTP_FROM`, `SMTP_TO` |
| Notify: General | `NOTIFY_COOLDOWN_SEC`, `SYSTEM_PROXY_URL` |
| Admin | `ADMIN_IP_ALLOWLIST` |
| Proxy: Core | `REQUEST_BODY_LIMIT` (hardcoded 20MB, no env var), `ROUTING_FALLBACK_UNIT_COST`, `PROXY_FIRST_BYTE_TIMEOUT_SEC` |
| Proxy: Token Router | `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC`, `TOKEN_ROUTER_CACHE_TTL_MS` |
| Proxy: Channel | `PROXY_MAX_CHANNEL_ATTEMPTS`, `PROXY_STICKY_SESSION_ENABLED`, `PROXY_STICKY_SESSION_TTL_MS` |
| Proxy: Session | `PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT`, `PROXY_SESSION_CHANNEL_QUEUE_WAIT_MS`, `PROXY_SESSION_CHANNEL_LEASE_TTL_MS`, `PROXY_SESSION_CHANNEL_LEASE_KEEPALIVE_MS` |
| Proxy: Debug | `PROXY_DEBUG_TRACE_ENABLED`, `PROXY_DEBUG_CAPTURE_HEADERS`, `PROXY_DEBUG_CAPTURE_BODIES`, `PROXY_DEBUG_CAPTURE_STREAM_CHUNKS`, `PROXY_DEBUG_TARGET_SESSION_ID`, `PROXY_DEBUG_TARGET_CLIENT_KIND`, `PROXY_DEBUG_TARGET_MODEL`, `PROXY_DEBUG_RETENTION_HOURS`, `PROXY_DEBUG_MAX_BODY_BYTES` |
| Proxy: Misc | `CODEX_UPSTREAM_WEBSOCKET_ENABLED`, `RESPONSES_COMPACT_FALLBACK_TO_RESPONSES_ENABLED`, `DISABLE_CROSS_PROTOCOL_FALLBACK`, `PROXY_EMPTY_CONTENT_FAIL`, `PROXY_ERROR_KEYWORDS` |
| Proxy: Codex | `CODEX_RESPONSES_WEBSOCKET_BETA`, `CODEX_HEADER_DEFAULTS_USER_AGENT`, `CODEX_HEADER_DEFAULTS_BETA_FEATURES` |
| Model Probe | `MODEL_AVAILABILITY_PROBE_ENABLED`, `MODEL_AVAILABILITY_PROBE_INTERVAL_MS`, `MODEL_AVAILABILITY_PROBE_TIMEOUT_MS`, `MODEL_AVAILABILITY_PROBE_CONCURRENCY` |
| Retention | `PROXY_LOG_RETENTION_DAYS`, `PROXY_LOG_RETENTION_PRUNE_INTERVAL_MINUTES`, `PROXY_FILE_RETENTION_DAYS`, `PROXY_FILE_RETENTION_PRUNE_INTERVAL_MINUTES` |
| Routing Weights | `BASE_WEIGHT_FACTOR`, `VALUE_SCORE_FACTOR`, `COST_WEIGHT`, `BALANCE_WEIGHT`, `USAGE_WEIGHT` |
| Payload | `PAYLOAD_RULES` / `PAYLOAD_RULES_JSON` |
| Service Tier | `OPENAI_SERVICE_TIER_RULES` / `OPENAI_SERVICE_TIER_RULES_JSON` |
| Internal | `globalBlockedBrands`, `globalAllowedModels` (both hardcoded empty arrays, no env var) |

### M2. Parse functions not specified

The spec references parse functions only obliquely in Edge Cases ("bool '1'/'true'/'yes'/'on', int clamp, csv split, JSON parse") but never defines them. The TS has 8 parse functions with distinct behaviors:

| Function | Input | Output | Key behavior |
|----------|-------|--------|-------------|
| `parseBoolean` | `string or undefined` | `boolean` | `"1"/"true"/"yes"/"on"` maps to true; case-insensitive trimmed |
| `parseNumber` | `string or undefined` | `number` | `Number.isFinite` guard; returns fallback on NaN/Infinity |
| `parseCsvList` | `string or undefined` | `string[]` | Split by comma, trim, filter empty |
| `parseOptionalSecret` | `string or undefined` | `string` | Trim only; empty string fallback |
| `parseJsonValue` | `string or undefined` | `unknown` | `JSON.parse`; returns `undefined` on parse error |
| `parseDbType` | `string or undefined` | `sqlite or mysql or postgres` | Normalizes `postgresql` to `postgres`; default `sqlite` |
| `normalizeTokenRouterFailureCooldownMaxSec` | `unknown` | `number or null` | Clamp [1, 30 days]; `Math.trunc`; null on invalid/<=0 |
| `parseListenHost` | `ProcessEnv` | `string` | Reads `HOST`; fallback `0.0.0.0` |

### M3. Default value constants not listed

The spec says `defaults.go` contains "所有默认值常量" but lists none. TS defines these key defaults that must be ported:

- `DEFAULT_REQUEST_BODY_LIMIT = 20 * 1024 * 1024` (20 MB)
- `DEFAULT_CODEX_CLIENT_ID = 'CODEX_CLIENT_ID_PLACEHOLDER'`
- `DEFAULT_CLAUDE_CLIENT_ID = 'CLAUDE_CLIENT_ID_PLACEHOLDER'`
- `DEFAULT_GEMINI_CLI_CLIENT_ID = 'GEMINI_CLI_CLIENT_ID_PLACEHOLDER'`
- `DEFAULT_GEMINI_CLI_CLIENT_SECRET = 'GEMINI_CLI_CLIENT_SECRET_PLACEHOLDER'`
- `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC_CEILING = 30 * 24 * 60 * 60` (30 days)

### M4. Startup flow is radically simplified

The spec says: `config.Load -> store.Open -> migrate -> router.New -> app.Run`

The actual TS startup flow (from `index.ts`) is:

1. `ensureRuntimeDatabaseReady` -- bootstrap the runtime DB
2. Load settings from `settings` table
3. `extractSavedRuntimeDatabaseConfig` -- check if DB config was overridden via settings
4. `switchRuntimeDatabase` -- apply saved DB config if different
5. `ensureSiteCompatibilityColumns` (and 5+ other schema migration functions)
6. `applyRuntimeSettings` -- hydrate config from DB settings
7. `ensureOauthProviderSitesExist`
8. Create Fastify app
9. Register CORS
10. Register auth hook (with `isPublicApiRoute` bypass)
11. Register 18+ route modules
12. Register proxy routes
13. Register static file serving (`@fastify/static`) with cache headers
14. Set SPA fallback (`setNotFoundHandler`)
15. Start 8+ background services (scheduler, polling, probes, aggregation, OAuth callbacks)
16. Register `onClose` hook for all background services
17. `app.listen()`

**Every step between `store.Open` and `app.Run` is missing from the spec.**

### M5. Graceful shutdown missing background service lifecycle

The spec shows shutdown as `srv.Shutdown(ctx)` followed by `store.Close()`. The TS shutdown sequence (via `onClose` hook) stops 10 services in order:

1. `stopSiteAnnouncementPolling`
2. `stopUpdateCenterPolling`
3. `stopProxyFileRetentionService`
4. `stopProxyLogRetentionService`
5. `stopModelAvailabilityProbeScheduler`
6. `stopChannelRecoveryProbeScheduler`
7. `stopUsageAggregationProjectorScheduler` (async)
8. `stopAdminSnapshotWarmScheduler` (async)
9. `stopSub2ApiManagedRefreshScheduler` (async)
10. `stopOAuthLoopbackCallbackServers` (async)

The spec's `store.Close()` alone is insufficient.

### M6. SPA static file serving details missing

The spec has `r.NotFound(serveSPA(webFS))` without details. TS implements:

- Checks `existsSync(webDir)` before registering static handler
- Sets `Cache-Control: public, max-age=31536000, immutable` for `/assets/*`
- Sets `Cache-Control: no-cache` for `index.html`
- SPA fallback: returns `index.html` for non-`/api/` and non-`/v1/` routes; returns 404 JSON for API routes

### M7. Runtime settings hydration missing

TS loads config overrides from the `settings` database table at startup via `applyRuntimeSettings()`. This is a critical architectural concern: the config is not purely env-var-driven. The spec makes no mention of this two-phase config loading (env vars, then DB override).

### M8. Database compatibility migration steps missing

TS runs 6+ schema migration functions at startup:
- `ensureSiteCompatibilityColumns`
- `ensureRouteGroupingCompatibilityColumns`
- `ensureProxyFileCompatibilityColumns`
- `ensureProxyLogStreamTimingColumns`
- `ensureProxyLogClientColumns`
- `ensureProxyLogDownstreamApiKeyIdColumn`
- `ensureProxyLogBillingDetailsColumn`
- `repairStoredCreatedAtValues`
- `migrateSiteApiKeysToAccounts`
- `ensureDefaultSitesSeeded`
- `ensureOauthIdentityBackfill`

The spec only says `migrate` with no detail.

### M9. Public API route bypass missing

The spec shows `/api` routes all behind `auth.AdminAuth`. TS has `isPublicApiRoute(request.url)` which allows certain `/api/` endpoints to bypass auth. The set of public routes is not documented in the spec.

### M10. Dockerfile: migration directory and step not mentioned

TS `Dockerfile.slim` copies `drizzle/` and runs `node dist/server/db/migrate.js` before starting. The Go spec mentions `migrate` in the startup flow but the Dockerfile has no equivalent migration step or migration file copy.

---

## Edge Cases Not Covered

### E1. `.env` with malformed JSON values
TS `parseJsonValue` returns `undefined` on `JSON.parse` failure. Spec does not address what happens when `PAYLOAD_RULES_JSON` or `OPENAI_SERVICE_TIER_RULES_JSON` contain invalid JSON.

### E2. Unknown `DB_TYPE` value
TS `parseDbType` defaults to `sqlite` for any unrecognized value. Spec only covers `DB_TYPE=postgres` with empty `DB_URL`. What about `DB_TYPE=mongodb` or garbage input?

### E3. Port already in use
Neither TS nor spec addresses `EADDRINUSE`. Go's `ListenAndServe` would return an error that the spec's `go srv.ListenAndServe()` goroutine silently drops.

### E4. `DATA_DIR` with trailing slash or relative path
TS uses `DATA_DIR` as-is. Spec does not mention path normalization or creation of data directory.

### E5. Settings table missing or corrupted
TS wraps the entire settings-load block in try/catch. Spec has no error handling for DB read failures during startup.

### E6. Runtime DB switch failure rollback
TS has explicit rollback logic: if `switchRuntimeDatabase` throws, it checks whether the switch partially succeeded and reverts. Spec does not mention this.

### E7. Double SIGTERM during graceful shutdown
Spec shows a single `sigCh` receive. If a second signal arrives during the 5-second shutdown window, the default Go behavior is immediate exit. The spec should address whether a second signal should force-exit.

### E8. Background service start failures
TS wraps `startOAuthLoopbackCallbackServers` in try/catch and logs a warning. The spec has no error handling for any service startup failure -- should a failed background service prevent startup or just log?

### E9. `.env` file not found
Spec mentions this in Edge Cases but does not specify: should it be a silent skip (TS behavior via `dotenv/config`) or a logged warning?

### E10. Empty string vs undefined for optional secrets
TS `parseOptionalSecret` returns empty string for undefined input, but some config fields have hardcoded defaults (e.g., `codexClientId` falls through to `DEFAULT_CODEX_CLIENT_ID` when parse returns empty). The spec does not distinguish between "env var not set" and "env var set to empty string."

### E11. `PORT=0` behavior
TS `parseNumber(env.PORT, 4000)` with `Math.trunc` would yield `0` if `PORT=0`. The Edge Cases section says "PORT 为负数/非数字 -> 使用默认值 4000" but `0` is neither negative nor non-numeric. Go would bind to a random port on `:0`, which is a significant behavioral difference.

### E12. `CHECKIN_SCHEDULE_MODE` not mentioned
TS supports `CHECKIN_SCHEDULE_MODE=cron|interval`. Only `CHECKIN_INTERVAL_HOURS` is mentioned in the spec. If mode is `interval`, the interval value matters; if `cron`, it is ignored.

---

## Incorrect Details

### I1. Acceptance Criterion: "缺失必填项时报清晰错误 (不是 panic trace)"
The spec does not define which config fields are **required** vs **optional**. TS has no truly required env vars -- everything has a default or falls back gracefully. The acceptance criterion implies required fields exist but none are listed.

### I2. Acceptance Criterion: "Docker 镜像 <20MB"
TS `Dockerfile.slim` uses `node:25-alpine` (~130MB base) plus `node_modules`. The Go spec's Dockerfile (from scratch after alpine:3.21) should easily hit <20MB for the binary, but this is a **new target**, not a port of the TS value. The spec implies this is a constraint derived from TS but the TS image is much larger.

### I3. Dockerfile: Missing native build dependencies for web frontend
TS `Dockerfile.slim` installs `python3 make g++` before `npm ci` because `better-sqlite3`, `sharp`, and `esbuild` require native compilation. The Go spec's Dockerfile frontend stage omits these dependencies. If `npm ci` for the web frontend requires native modules, the Go Dockerfile will fail at build time.

### I4. Dockerfile: Missing `DATA_DIR` env var
TS `Dockerfile.slim` sets `ENV DATA_DIR=/app/data`. The Go spec's Dockerfile does not set this. While the Go config defaults to `./data`, in the container the working directory is undefined (no `WORKDIR` in stage 3). The binary may attempt to write to `/data` (relative to root) or fail.

### I5. Makefile targets claim: `make build test lint docker-build run`
No `Makefile` content is provided in the spec. The acceptance criterion lists these targets but the spec does not define what `build`, `test`, `lint`, or `run` should do. Cross-referencing: TS has no Makefile; uses npm scripts exclusively. This is a net-new build system design with zero specification.

### I6. slog log format in Acceptance Criteria
Spec shows `{"time":"...","level":"INFO","msg":"listening","port":4000}`. TS uses Fastify's built-in logger (pino) with a different format. The spec's format is a reasonable Go choice but should not be presented as "matching TS" -- it does not.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 5 |
| Missing Details | 10 |
| Edge Cases Not Covered | 12 |
| Incorrect Details | 6 |
| **Total Findings** | **33** |

### Verdict: **NEEDS_REVISION**

The spec provides a reasonable architectural skeleton (Chi router, graceful shutdown pattern, multi-stage Dockerfile) but fails at its core claim: cross-referencing the TypeScript source for completeness. The config surface -- the single largest piece of this P0 -- is effectively undocumented. The startup flow is missing 90% of the actual TS startup sequence including runtime DB bootstrap, settings hydration from the database, schema compatibility migrations, and background service lifecycle. The shutdown logic omits all 10 background service stop hooks.

**Minimum required before re-review:**
1. Add full config field mapping table (all 88+ fields with TS env var names, Go types, defaults, and parse functions)
2. Add parse function specifications (8 functions with exact behavior)
3. Add complete startup flow (all 17 steps from TS, even if some are deferred to later phases with stub markers)
4. Add graceful shutdown service lifecycle (all 10 background service stops)
5. Add missing edge cases E1-E12
6. Remove or caveat the "allow all origins" CORS claim
7. Note that `/health` is a net-new addition, not a port
