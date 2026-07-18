# Changelog

All notable changes to MetAPI-Go will be documented in this file.

格式基于 [Keep a Changelog](https://keepachangelog.com/en/1.0.0/)，
版本号遵循 [Semantic Versioning](https://semver.org/spec/v2.0.0.html)。

## [v0.8.37] — 2026-07-18

### Docs
- Align README/README_EN stack badges to Go 1.26.5 + React 19 + Vite 8 (#494 / #498)

### Reliability
- Best-effort TPM admission estimate when maxTPM is set (no invent; empty body skips) (#495 / #500)

### Tests / Honesty
- P0-585 credential usage-limit multi-channel cool honesty tests (intentional shared-key scope; cascade still partial) (#496 / #499)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 47 post-product landings; DOCS-STACK-TRUTH + REL-TPM-ESTIMATE + REL-CRED-USAGE-HONESTY present · board #494–#497 closed (#497 / #501)

## [v0.8.36] — 2026-07-18

### Security
- Clear meta_monitor_auth cookie on successful admin AuthToken change (defense-in-depth; HMAC already invalidates) (#484 / #489)

### UI
- Tokenize residual monitor-hint / route-enable-disabled / stat-summary / topbar brand hex to design tokens (#485 / #490)

### Tests / Observability
- P0-555 Claude/Anthropic stream message_delta usage merge honesty tests (never invents tokens) (#486 / #488)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 46 post-product landings; SEC-MONITOR-TOKEN-CLEAR + UI-CSS-RESIDUAL + REL-P0555-STREAM-TESTS present · board #484–#487 closed (#487 / #491)

## [v0.8.35] — 2026-07-18

### UI
- Wire DownstreamKeys maxRpm/maxTpm create/edit/list (backend #116 admission already present) (#475 / #481)
- Tokenize residual login-shell hard-coded hex to design tokens where clean (#477 / #480)

### Tests / Reliability
- P0-585 empty-filter global full-set fallback honesty regression tests (does not flip cascade to present) (#476 / #479)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 45 post-product landings; UI-KEY-RPM-TPM + REL-EMPTY-FILTER-TESTS + UI-LOGIN-TOKENS present · board #475–#478 closed (#478 / #482)

## [v0.8.34] — 2026-07-18

### UI
- Wire DownstreamKeys proxyUrl create/edit/list (backend KEY-578 already present) (#466 / #471)
- Wire TokenRoutes contextLength create/edit/list badge (backend CTX-520 admin already present) (#467 / #472)
- Migrate high-value hard-coded CSS hex clusters (checkin-toggle, route-enable, info-tip, model-tag-*, status-dot-*) to design tokens (#468 / #470)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 44 post-product landings; UI-KEY-PROXY + UI-ROUTE-CTX + UI-TOKEN-DEBT present · board #466–#469 closed (#469 / #473)

## [v0.8.33] — 2026-07-18

### UI
- Migrate hard-coded .stat-icon-* colors to design tokens (var(--color-stat-*)) for light/dark SSOT (#456 / #460)
- Wire Sites maxConcurrency in admin create/edit/list (backend limiter already present) (#457 / #461)

### Fixed
- Gemini generateContent/streamGenerateContent: reject generationConfig.maxOutputTokens above positive route context_length with honest 400 (extends CTX-520) (#458 / #462)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 43 post v0.8.32; UI-STAT-TOKENS + UI-SITE-CONC + CTX-520-GEMINI present · board #456–#459 closed (#459 / #464)

## [v0.8.32] — 2026-07-18

### Security
- system-proxy/test rejects non-empty targetUrl that fails IsValidHTTPURL / IsForbiddenSiteTargetURL (metadata/link-local) before probe (#449 / #452)

### Fixed
- OpenAI /v1/responses (+ /compact): reject max_output_tokens or max_tokens above positive route context_length with honest 400 (no silent clamp; extends CTX-520) (#450 / #454)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 42 post v0.8.31; SEC-PROXY-TEST-TARGET + CTX-520 Responses path present · board #449–#451 closed (#451 / #453)

## [v0.8.31] — 2026-07-18

### Security
- ProxyAwareHTTPClient shares RejectCrossOriginRedirect (HTTPGet/Post helpers inherit; Telegram patch idempotent) (#441 / #446)
- SiteProxy buildClients + doWithExplicitProxy share RejectCrossOriginRedirect (parity with DoWithProxy hot path) (#442 / #444)
- Downstream-keys update + reset-usage redact plaintext key (keyMasked only; create/export once-echo unchanged) (#440 / #445)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 41 post v0.8.30; SEC-PROXY-UTIL-REDIR / SEC-SITEPROXY-REDIR / SEC-KEY-MUTATE present · board #440–#443 closed (#443 / #447)

## [v0.8.30] — 2026-07-18

### Security
- Share RejectCrossOriginRedirect on residual OAuth Codex HTTP client + Telegram notify clients; public-origin 302 to different host rejected (#433 / #436)

### Fixed
- loadRouteMatch applies source route model_pattern as SourceModel fallback when channel SourceModel blank/nil (group/source eligibility + resolveModel) (#434 / #438)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 40 post v0.8.29; SEC-OAUTH-NOTIFY-REDIR + REL-SOURCE-MODEL present · board #433–#435 closed (#435 / #437)

## [v0.8.29] — 2026-07-18

### Fixed
- Preferred/sticky channel selection respects open site/model breaker and falls through when healthy siblings exist (`SelectPreferredChannel`) (#423 / #430)
- CooldownUntil eligibility parses timestamps (no millis-ISO vs RFC3339 lex compare) via `IsCooldownActive` (#424 / #427)
- Proxy conductor hard max attempt budget across same-channel + refresh + failover; cap RefreshAuth successes; nil/error RefreshAuth → sibling failover with channel-scoped exclude (#425 / #431)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 39 post v0.8.28; REL-PREFERRED-BREAKER / REL-COOLDOWN-TS / REL-CONDUCTOR-BUDGET present · board #423–#426 closed (#426 / #428)

## [v0.8.28] — 2026-07-18

### Security
- Share `RejectCrossOriginRedirect` on residual bare clients: channel health probe, channel test harness, and `defaultUpstreamClient` (no longer follow public-origin 302 → private/metadata) (#416 / #420)
- Admin logout / session clear sets `Max-Age=0` for `meta_monitor_auth` with matching `Path=/monitor-proxy/` (#417 / #421)

### Fixed
- Residual bare HTTP clients no longer inherit Go default redirect policy when site proxy is absent (#416 / #420)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 38 post v0.8.27; SEC-REDIR bare clients + SEC-MONITOR logout residual shipped · board #416–#418 closed (#418 / #419)

## [v0.8.27] — 2026-07-18

### Security
- Monitor session cookie is opaque HMAC (never embeds live `AUTH_TOKEN`); constant-time compare; cookie scoped to `Path=/monitor-proxy/` so theft cannot become full admin bearer (#407 / #414)
- Admin auth token change: constant-time `OldToken` compare (parity with AdminAuth middleware; reject mismatched lengths without leaking timing) (#408 / #411)

### Fixed
- Claude `/v1/messages`: reject `max_tokens` above positive selected-route `context_length` with honest 400 `invalid_request_error` (no silent clamp; extends OpenAI #399) (#409 / #412)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 37 post v0.8.26; SEC-MONITOR + SEC-AUTH-TIMING present · CTX-520 Claude path shipped (further dialect residual) · board #407–#410 closed (#410 / #413)

## [v0.8.26] — 2026-07-18

### Security
- `IsValidAPIEndpointURL` rejects cloud metadata / link-local targets (aligned with `IsForbiddenSiteTargetURL` / `IsValidHTTPURL`); any caller is safe by default (#398 / #403)

### Fixed
- OpenAI chat/completions (and legacy completions): reject `max_tokens` above positive selected-route `context_length` with honest 400 `invalid_request_error` (no silent clamp; Claude out of scope) (#399 / #404)
- OpenAI chat/completions stream: `slog.Warn` once when stream ends without usable usage after `stream_options.include_usage` (injected or client-provided); never invent token counts (#400 / #401)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 36 post M35 / v0.8.25; SEC-ENDPOINT fully present · CTX-520 max_tokens reject shipped · P0-555 missing-usage warn residual note (#397 / #402)

## [v0.8.25] — 2026-07-17

### Security
- `IsValidHTTPURL` rejects cloud metadata / link-local targets (aligned with `IsForbiddenSiteTargetURL`); site externalCheckin URL uses the hardened check (#382 / #385)

### Fixed
- Admin `GET /api/routes` batch-loads route channels in one query and groups in memory (kills per-route N+1; response shape + #375 redact unchanged) (#383 / #386)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 34 post v0.8.24; SEC-HTTPURL + PERF-ROUTES → present (#384 / #387)

## [v0.8.24] — 2026-07-17

### Security
- Admin routes channel list/get + `POST /api/search` redact plaintext `accessToken`/`apiToken`/`token` (masked only) (#375 / #378)
- Site create/update + API endpoint upsert reject cloud metadata / link-local URLs (`169.254.0.0/16`, IPv6 link-local, `metadata.google.internal`); RFC1918 + localhost still allowed (#376 / #379)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 33 post v0.8.23; SEC-ROUTE + SEC-SITEURL → present (#377 / #380)

## [v0.8.23] — 2026-07-17

### Security
- Admin account list/overview redacts `accessToken`/`apiToken` (masked only) and strips `passwordCipher` from list `extraConfig`; account-token list drops join credential fields (#367 / #372)
- Credential export remains intentional product path (create/update may still echo once outside list enrichment)

### Fixed
- Round-robin / stable_first / least_* soft-filter priority demotion: soft-empty higher priority tries next layer (parity with weighted #358) (#368 / #370)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 32 post v0.8.22; SEC-ADMIN + REL-SOFT-RR → present (#366 / #369)

## [v0.8.22] — 2026-07-17

### Security
- Redact plaintext `key` from admin downstream-keys list/summary/overview (`keyMasked` only) (#355 / #361)
- Deny-list sensitive site `custom_headers` (Authorization/Host/Cookie/hop-by-hop/Proxy-*/Content-Type); shared `platform.ApplyCustomHeaders`; Bearer set after custom so identity cannot be overridden (#356 / #364)
- RuntimeExecutor `CheckRedirect` rejects cross-origin and private/metadata redirect targets (#357 / #360)

### Fixed
- Weighted routing: when soft-filter empties a priority layer, try the next priority instead of reselecting the unfiltered broken layer (#358 / #362)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 31 post v0.8.21; SEC-KEY/HDR/REDIR + REL-SOFT → present (#359 / #363)

## [v0.8.21] — 2026-07-17

### Fixed
- OpenAI legacy `/v1/completions` stream: same `stream_options.include_usage=true` inject as chat (path helper; still skips codex/sub2api and non-OpenAI stream_options paths) (#350 / #352)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 30 post v0.8.20 (#351 / #353)
- P0-555: chat + legacy completions stream policy wired; residual provider-ignore / media zeros / multi-instance lag / orphan site join

## [v0.8.20] — 2026-07-17

### Fixed
- OpenAI-compatible chat/completions stream: inject/merge `stream_options.include_usage=true` on upstream body for final SSE usage chunks; skip codex/sub2api and non-chat paths (P0-555 residual) (#345 / #347)

### Docs / Honesty
- Residual inventory + MASTER for Milestone 29 post v0.8.19 (#346 / #348)
- P0-555 evidence: stream_options policy addressed for chat stream; residual provider-ignore / media zeros / multi-instance lag / orphan site join

## [v0.8.19] — 2026-07-17

### Fixed
- Race-harden `scheduleSiteRuntimeHealthPersistence` / `persistSiteRuntimeHealthState`: timer + in-flight flags under `healthStateMu`; concurrent success/failure regression (#335 / #339)

### Docs / Honesty
- Residual inventory + MASTER flip for Milestone 28 post v0.8.18 (#334 / #337)
- P0-585 cascade residual honesty: shipped channel isolation vs site/model breaker + empty-filter fallback + no production multi-channel load proof (`failover-isolation.md`) (#336 / #343)

## [v0.8.18] — 2026-07-17

### Added
- OpenAI `/v1/models` prefers positive `token_routes.context_length` (max per exposed model id) over `knownModelContextLength` heuristics; production load path SELECTs the column (#327 / #332)

### Fixed
- Admin test isolation: stop reassigning `globalAccountsCache` pointer; drain background health-refresh runners before registry reset (DATA RACE under full `-race` suite) (#328 / #331)
- Race-safe `healthPersistTimer` clear under `healthStateMu` (ConfigureProxyUpstream / site runtime health debounce) (#327 gate)

### Docs / Honesty
- Residual inventory + MASTER pointers for Milestone 27 (#329 / #330)
- CTX-520 residual: models wire present; still no proxy max-token enforcement

## [v0.8.17] — 2026-07-17

### Added
- Admin `token_routes.contextLength` create/update + list/summary/lite surfaces (metadata-only; no proxy max-token enforcement) (#320 / #323)

### Fixed / Verified
- Usage aggregation projects `proxy_logs.status=failed` tokens into `failed_calls` + `total_tokens` (regression + audit note; aggregation logic already status-agnostic) (#319 / #324)

### Docs / Honesty
- Residual inventory + MASTER pointers post v0.8.16 (#318 / #322)
- P0-555 → present-with-residual after #311/#319; residual policy/media/lag only

## [v0.8.16] — 2026-07-17

### Fixed
- Wire Gemini official tool-history `thought_signature` inject/preserve on generateContent / gemini-cli paths (#309 / #314)
- Harden multi-turn Responses reasoning content sanitize (pretty-printed type keys + input gate) (#310 / #313)
- Persist failed upstream attempts to proxy_logs with best-effort usage from error bodies (#311 / #315)

### Docs / Honesty
- Gap matrix #580/#581/#538 → present (with residual notes)
- usage-token-extraction-audit follow-up (#311)
- Hot-fix conflict markers in upstream_test after squash (#316)

## [v0.8.15] — 2026-07-17

### Fixed
- Gate `ReportTokenExpired` / checkin-balance mark paths with `ShouldMarkAccountExpired` (no bare/generic 401 over-expiry) (#298 / #301)
- Channel-scoped cascade isolation: 429 fails over, same-channel timeout budget, multi-channel same-site isolation tests (#299 / #302)
- Preserve stream/partial usage on client disconnect when usage was already extracted (#300 / #303)

### Docs / Honesty
- Failover isolation residual notes for #585 (#299)
- Gap matrix rows for #568 present + #585/#555 partial evidence refresh (via #301/#302/#303)

## [v0.8.14] — 2026-07-17

### Docs / Honesty
- Residual next candidates inventory post v0.8.13 (#290 / #293)
- Redis sticky Option B design spike (no product code) (#292 / #294)
- Admin /api/test stream and job residual honesty (#291 / #295)

## [v0.8.13] — 2026-07-17

### Added
- token_routes.sort_order + PUT /api/routes/reorder bulk drag reorder (#284 / #288)

### Docs / Honesty
- original-gap-matrix refresh for shipped surfaces (rerank/site concurrency/key proxy/rebuild/cache_ratio) (#281 / #285)
- sticky multi-instance affinity product-path evaluation (#282 / #286)
- update-center residual honesty hardening (no remote registry) (#283 / #287)

## [v0.8.12] — 2026-07-17

### Fixed
- Admin BackgroundTask snapshot under mutex (DATA RACE on get/list vs runner Result write) (#271 / #275)

### Added
- Site-announcement scheduler wires to real `SyncSiteAnnouncements` via SyncFunc (#272 / #278)
- Channel recovery active candidates via optional `ProxyChannelCoordinator` provider hook (#273 / #276)

### Docs / Honesty
- Responses WebSocket residual product path evaluation (stay 426/501 for v0.8.x) (#274 / #277)

## [v0.8.11] — 2026-07-17

### Added
- DB-backed durable admin BackgroundTask store (cross-instance list/get) (#265 / #267)

### Fixed
- Frontend CI EnvironmentTeardownError flake hardening (#266 / #268)

## [v0.8.10] — 2026-07-17

### Added
- Sub2API refresh scheduler wires to RefreshBalance (#261 / #263)
- Proxy video task age-based retention scheduler (config-gated, default off) (#262 / #263)

## [v0.8.9] — 2026-07-17

### Added
- Videos GET/DELETE sticky pin via ForcedChannelID from mapping ChannelID (#253 / #256)

### Docs / Honesty
- proxy_video_tasks retention residual (no TTL/GC) (#254 / #259)

## [v0.8.8] — 2026-07-17

### Added
- Durable `proxy_video_tasks` dual-write for video publicId mapping (multi-instance / restart) (#244 / #251)
- TPM multi-instance Redis sharing via sharedcount (fail-open, mirrors RPM) (#245 / #249)

### Docs / Honesty
- Scheduler silent TODO residual inventory (sub2api / channel-recovery / announcement / update-center) (#246 / #250)

## [v0.8.7] — 2026-07-17

### Added
- Videos create: process-local publicId mapping + response `id` rewrite on successful POST /v1/videos (#235 / #241)

### Fixed / Honesty
- ResolveInputFile returns explicit residual error (no silent vault) (#238 / #239)
- Sticky session multi-instance residual analysis + code comment (#237 / #240)
- Admin StartBackgroundTask / /api/tasks process-local multi-instance residual honesty (#236 / #242)

## [v0.8.6] — 2026-07-17

### Fixed
- Videos GET/DELETE honest upstream passthrough without empty local-store 404 theater (#225 / #231)

### Added / Tests
- Downstream key maxCost/maxRequests clear-to-NULL API tests (#226 / #233)
- Claude cache_ratio 0.1 / cache_creation_ratio 1.25 assertions on proxy billing details (#227 / #230)
- ParseInputFiles extracts OpenAI input_file/file body refs (#228 / #232)

## [v0.8.5] — 2026-07-17

### Added
- Site initialization preset registry + create/detect validation (#214 / #222)
- Gemini `/v1beta/models` from owned model catalog (#215 / #221)
- Site proxy cache invalidation hooks (routing + admin accounts snapshot) (#216 / #219)
- Responses WebSocket honest residual + boot wire (#217 / #220)

### Fixed
- Shared PG CI: prefer `SiteSelectColumns` over `SELECT * FROM sites` (probe-column drift)

## [v0.8.4] — 2026-07-17

### Fixed
- PostgreSQL CreateSite: RETURNING id + explicit sites column select (shared CI probe-column drift) (#204 / #208)
- Multipart `/v1/images/edits` forwards via dispatchUpstream (no example.com stub) (#207 / #210)

### Added
- Expired API-key account recovery on credential update (allowInactive model refresh + reactivate) (#205 / #212)
- Account token groups via platform.GetUserGroups with local fallback (#206 / #211)

## [v0.8.3] — 2026-07-17

### Added
- Admin residual stubs wave (milestone 12):
  - sub2api managed auth merge on account update/rebind (#194 / #202)
  - Real account health-refresh via balance probe (#195 / #199)
  - OAuth start/rebind CSRF state tokens (server-stored, TTL) (#196 / #200)
  - Honest update-center deploy/rollback residuals + real clear-cache invalidation (#197 / #201)

## [v0.8.2] — 2026-07-17

### Added
- P4 adapter wiring (milestone 11):
  - Account token create/delete/sync via platform adapters + SyncTokensFromUpstream (#182 / #190)
  - Account create fail-closed VerifyToken / GetModels with skipModelFetch residual (#183 / #189)
  - Real system-proxy probe + brand list from platform registry (#184 / #186)
  - `/api/test/proxy` + `/api/test/chat` wired to forced-channel harness; stream/jobs honest 501 (#185 / #187)

### Notes
- Residual TODOs: sub2api managed auth on update, expired API-key recovery model refresh, async health-refresh job, OAuth state stubs.

## [v0.8.1] — 2026-07-17

### Fixed
- Go 1.26.5 toolchain; vulncheck green (GO-2026-5856) (#168)

### Added
- Live /v1/models listing via TokenRouter.GetAvailableModels (#169)
- Boot-wired ModelProbeScheduler probe executor + health recorder (#170)
- Route decision admin APIs wired to ExplainSelection (#171)

## [v0.8.0] — 2026-07-17

### Added
- Competitive learn program (M-COMPETE) fully implemented for [learn] issues #110–#121:
  - Request trace IDs across retries/failovers (#110)
  - Per-request cost attribution + cache token types (#111)
  - TTFT/first-byte signals in routing health (#113)
  - Cross-site model price comparison APIs (#112)
  - Background channel health probing (#114)
  - Pluggable routing strategies: least_busy / lowest_latency / lowest_cost (#115)
  - Downstream-key RPM/TPM soft admission + Retry-After (#116)
  - Richer Prometheus histograms/labels + MetricsObserver export hook (#117)
  - Optional Redis-backed shared RPM admission (fail-open, zero third-party dep) (#118)
  - Admin forced-channel test harness (#119)
  - Client credential export adapters (openai/cherry/generic) (#120)
  - Usage heatmaps + slow-request ranking stats (#121)
- Enterprise ops residual milestone opened for remaining admin/proxy stubs (#154–#158).

### Changed
- MASTER progress: M-COMPETE learn #110–#121 marked complete; stack remains TS 7.0.2 + React 19.2.7 + Vite 8.1.5 + Go 1.26.4.

### Notes
- `vulncheck` may still fail on Go 1.26.4 stdlib advisory GO-2026-5856; CI continues with continue-on-error until a Go patch is available.
- Residual operator stubs (site probe-now stream, /v1/files 501, marketplace/token-candidates, notify/LDOH/tasks) tracked under milestone Enterprise ops residual + v0.8.0.

## [v0.7.0] — 2026-07-17

### Added
- Enterprise modernization program (stack TS7/React19/Vite8, UI tokens/a11y, backend boundaries, schema additive migrations, reliability SSOT).
- Feature completeness from original metapi gap matrix: site max concurrency, per-key proxy, group route rebuild, `/v1/rerank`, usage/token accounting, failover/first-byte, protocol pack (Gemini thought_signature, Minimax thinking, models shape, previous_response_id, skill-call, responses multi-turn reasoning, responses-only sites), Codex OAuth gpt-5.5 + discovery soft-retry.
- Competitive learning milestone (M-COMPETE) vs all-api-hub / axonhub / new-api / litellm with matrix + `[learn]` backlog.

### Fixed
- Admin correctness: key refresh name/enable preserve, quota clear, model whitelist non-destructive parse, in-route model config preserve, expired account health.
- Frontend CI flake: dashboard site-observability EnvironmentTeardownError hardening.

### Notes
- `vulncheck` may still fail on Go 1.26.4 stdlib advisories (GO-2026-5856 class); CI keeps continue-on-error until Go patch available.
- Competitive `[learn]` items remain backlog-only until scheduled for implementation.

## [v0.6.5] — 2026-07-10

### Fixed
- 修复 Content-Security-Policy 缺少 `frame-src` 导致 `check.linux.do` iframe 被拦截。

## [v0.6.4] — 2026-07-10

### Fixed
- 修复 Content-Security-Policy 过紧导致 dicebear 头像图片和 Cloudflare Insights 脚本被浏览器拦截。
- 新增 `img-src 'self' https://api.dicebear.com`、`connect-src 'self'` 和 `script-src https://static.cloudflareinsights.com` 指令。

## [v0.6.3] — 2026-07-07

### Fixed
- 修复后台 Admin API 被重复挂载成 `/api/api/*` 的生产路由问题，恢复 `/api/settings/auth/info`、站点、账号、签到等管理接口的正常访问。
- 登录页增加登录前明暗/跟随系统主题切换，并修复深色模式下品牌面板、链接和图标对比度。

## [v0.6.2] — 2026-07-07

### Fixed
- 修复根路径 WebUI 被非 `/v1` 代理别名鉴权拦截的问题，确保嵌入式 SPA fallback 正常返回前端页面。
- 修复嵌入式前端文件系统路径兼容性，支持 `web/dist` 作为 embed 根。
- 稳定 routing golden 与加权随机测试，避免 Windows CRLF checkout 和单次随机抽样导致 CI 偶发失败。

## [v0.6.1] — 2026-07-07

### Fixed
- CI/CD secret scan 改用开源 gitleaks CLI，避免 organization 仓库被 `gitleaks/gitleaks-action@v2` license gate 阻断发布。

## [v0.6.0] — 2026-07-07

### Security
- CI/CD 发布门禁加入 gitleaks、Go module 校验、race 测试、PostgreSQL integration 测试、前端 typecheck/test/build 和生产依赖 audit。
- CD 镜像发布前执行 Docker smoke test；发布镜像启用 provenance 和 SBOM。
- 测试和文档中的 PostgreSQL DSN 改为运行时拼接，减少 secret scanner 噪声。
- 站点自定义 headers 过滤 `Authorization`、`Cookie`、`New-API-User`、`Content-Type`、`Content-Length`、`Host` 等保留头，避免覆盖运行时认证语义。

### Fixed
- `/v1/*` 数据面接入数据库路由和真实上游选择，不再停留在未配置 stub 行为。
- 上游代理支持站点/账号代理、自定义 headers、失败记录和非流式可重试 failover。
- AnyRouter 禁用 NewAPI 风格 token 管理端点，避免错误调用 `/api/token`。
- API-key/proxy-only 账号不再执行签到或余额上游调用，禁用状态判断改为大小写不敏感。
- 账号 session rebind、manual models、account token 默认值维护补齐事务和错误处理，失败路径回滚。

### Added
- 覆盖 SQLite 和 PostgreSQL 的账号、签到、余额、AnyRouter、代理上游和路由选择回归测试。
- 运行时说明明确当前支持 SQLite 单节点和 PostgreSQL 部署；Redis 尚未集成。

## [v0.5.0] — 2026-07-05

### Security
- Admin/proxy token 比较改用 `crypto/subtle.ConstantTimeCompare`（防时序攻击）
- CI 启用 `errcheck`、`staticcheck`、`ineffassign` linter
- CI 测试启用 `-race`（data race 检测）
- `/debug/vars` 移至 admin auth 保护后（此前无认证暴露）
- 安全响应头中间件：`X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `CSP`
- AES 密钥派生不再 fallback 到 `AUTH_TOKEN`（独立默认值）

### Fixed
- 代理出口 `http.DefaultClient`（零超时）→ 接入 `RuntimeExecutor`（90s 超时 fallback）
- 6 处 OAuth `panic()` → `return error`
- SSE 流式响应 `WriteTimeout: 60s` 截断问题 → `SetWriteDeadline` 禁用
- 13 处 `log.Printf` → `slog.Warn/Error`
- DB 连接池补充 `ConnMaxLifetime`(5min) + `ConnMaxIdleTime`(2min)
- `usage_aggregation` goroutine re-panic 修复（不再能崩进程）
- `CheckinScheduler.Stop()` data race 修复
- CI：`golangci-lint-action@v6` Go 1.25 不兼容 → `go install` 最新版
- `golangci-lint` 全项目 zero warning

### Added
- `/metrics` Prometheus 端点（零依赖 text format）
- `RequestID` 中间件（`X-Request-Id` header + 日志关联）
- `handler/shared/errors.go`：`APIError` 结构化错误类型
- git pre-push hook（`.githooks/pre-push`）：自动跑 `vet + test -race`
- Claude Code push guard（`~/.claude/hooks/metapi-go-push-guard.sh`）
- `AGENTS.md` CI Discipline 规范

### Tests
- 8 个零覆盖包全部补齐测试（最低 50%，最高 100%）
- 新增 3 个 e2e 场景：并发代理、auth 时序安全、rate limit 拒绝
- `e2e` 测试包总数：4 → 5 文件

## [v0.4.0] — 2026-07-05

### Fixed
- 6 轮 audit 全部修复
- PG 兼容：`INSERT OR IGNORE` → `ON CONFLICT DO NOTHING`
- Cron 5 字段 → 6 字段自动转换
- `sqlx.BindDriver` 时序修复（`?` → `$N` 占位符重绑定）

## [v0.3.0] — 2026-07-04

### Changed
- goroutine 泄漏修复
- JSON 性能优化
- 包命名规范化
- `config.Validate()` 10 项启动校验

## [v0.2.0] — 2026-07-04

### Added
- 限流中间件（admin 100rps, OAuth 10rps）
- RWMutex 假桩替换为真实 `sync.RWMutex`
- DB 事务包裹 usage aggregation batch
- `store.Close()` 优雅关机

## [v0.1.0] — 2026-07-03

### Added
- MetAPI TypeScript → Go 完整重写初始发布
- 27 表双数据库（SQLite + PostgreSQL）
- 14 平台适配器
- 4 协议流式转换
- 15 后台调度任务
- 单二进制 + Docker 部署

[v0.6.3]: https://github.com/TokenDanceLab/metapi-go/compare/v0.6.2...v0.6.3
[v0.6.2]: https://github.com/TokenDanceLab/metapi-go/compare/v0.6.1...v0.6.2
[v0.6.1]: https://github.com/TokenDanceLab/metapi-go/compare/v0.6.0...v0.6.1
[v0.6.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.5.0...v0.6.0
[v0.5.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.4.0...v0.5.0
[v0.4.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.3.0...v0.4.0
[v0.3.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.2.0...v0.3.0
[v0.2.0]: https://github.com/TokenDanceLab/metapi-go/compare/v0.1.0...v0.2.0
[v0.1.0]: https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.1.0
