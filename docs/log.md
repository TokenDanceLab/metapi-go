# log.md — MetAPI Go progress log

> **进度日志**（append-only）。不是现状 SSOT。  
> 现状 → [`STATE.md`](STATE.md) · 开放项 → [`progress/MASTER.md`](progress/MASTER.md)

## [2026-07-20] Linux gallery baselines = GHA actuals (not Docker)

- ui-visual failed: console density changed full-page height; Docker jammy snapshots still drift vs GHA fonts (light 3919 vs 3953).
- SSOT: copy CI `design-gallery-*-actual.png` → `*-chromium-linux.png`; drop serial so dark actuals also upload.
- Residual after green: UI release decision; empty-DB AUTH page recapture.

## [2026-07-20] Linux gallery baselines after console density (partial)

- First attempt: Docker Playwright v1.61.1 jammy regenerate — insufficient vs GHA; superseded by GHA actuals entry above.

## [2026-07-20] Console density + hi-res type polish

- System font stack (drop Google Fonts Inter CDN); letter-spacing / line-height tokens; page-title + KPI weight 400; tabular nums.
- Pill sidebar/topbar active nav; calmer card hover (no translateY lift).
- `.main-content` max-width ladder 1680→1920→2280→2600 + centered; larger pad on 2560+.
- Docs: DESIGN.md + ui-ux-refresh abstract only (no private portal facts).
- Residual: linux baselines (fixed next entry); UI release decision; empty-DB AUTH recapture.

## [2026-07-20] Shell mock sidebar full parity (14 items)

- User saw truncated left nav → root cause was `/__design__` Shell chrome mock (3–4 items), not product `sidebarGroups`.
- `DesignSystemGallery` shell mock now lists full production labels (控制台 10 + 系统 4); topbar adds 模型操练场.
- Unit guard `designSystemGallery.shell-nav.test.ts`; shell-*.png recaptured; win32 gallery visual baselines updated; `web/dist` rebuilt for embed.
- Residual: linux gallery baselines may need CI actuals if pixel-diff; empty-DB real page shots still need AUTH token; UI release decision.

## [2026-07-20] UI 原版对照 inventory（功能未删）

- 用户反馈「丑 + 原版功能和按钮全没了」→ 对照 `TokenDance/metapi` web vs metapi-go tip。
- 结论：侧栏 18 路由齐平；Sites/Accounts/Tokens/Routes/Settings 按钮计数齐平；`/tokens` 两边均 redirect 到连接管理。
- 体感来源：空库稀疏 + ops pin 0.8.44 未含 tip first-run/glass + 主题 indigo→GCP blue + 仓库空库截图仍 pre-#553/#554。
- 文档：`docs/analysis/ui-original-parity-2026-07-20.md`；STATE/MASTER residual 指针更新。无产品代码。

## [2026-07-19] M52 Wave2 first-run closed — epic #548 done

- Merged #554 Sites banner defer (PR #555 `68ff46e`) · #553 Dashboard getting-started (PR #556 `479f52c`).
- #553 fixup: Dashboard unit tests wrap `MemoryRouter` (Link context); frontend CI green.
- Closed epic #548; Milestone 52 residual = optional shot recapture + **UI release decision**.
- Tip `479f52c`; ops pin still **v0.8.44** unreleased.
- Board empty; mode → maintenance.

## [2026-07-19] M52 Wave1 merged — screenshot residual polish

- Milestone **52 UI-POLISH** + epic #548; Project items Todo.
- Wave1 closed: #543 Traffic sparkline · #544 real page score honesty · #545 hex soft pass · #546 axe smoke (PRs #549–#552).
- Unblocked CI frontend: dual-CTA EmptyState tests (`8bd9ec1`).
- First-run product backlog: #553 Dashboard zeros · #554 Sites weight banner.
- Tip `9092a4b`+; ops pin still **v0.8.44** unreleased.

## [2026-07-19] UI polish: focus-trap + EmptyState residual + skip-link

- Shared `useFocusTrap` wired into SearchModal / CenteredModal / MobileDrawer / NotificationPanel.
- Skip link → `#main-content`; sidebar `:focus-visible`; chrome i18n for nav/skip.
- EmptyState: Accounts, Tokens panel, ModelTester conversation empty.
- typecheck + related vitest pass; web dist rebuilt. Still **unreleased** (ops pin v0.8.44).
- Residual: optional live authed shots, hex hygiene, axe CI, UI patch release decision.

## [2026-07-19] M51 UI-REFRESH epic closed (unreleased)

- Closed #532 epic + #538 (mock track). All M51 children closed.
- Tip `168e8ee`; ui-visual CI green; ops pin remains v0.8.44.
- Residual only: optional live authed shots, focus-trap/hex, Accounts/ModelTester empty, UI patch release.

## [2026-07-19] M51 closeout: foundation issues + Linux CI green + more EmptyState

- Pushed linux gallery baselines; `ui-visual.yml` **success**.
- Closed #533–#536 · #539 (with #537/#540/#541 already closed).
- EmptyState: Sites / ProxyLogs / OAuth / TokenRoutes; residual Accounts/ModelTester/Tokens panel.
- Epic #532 open for #538 real authed shots + optional UI release decision.

## [2026-07-19] #539 Linux gallery baselines + more EmptyState pages

- Committed `design-gallery-*-chromium-linux.png` from CI failure actuals (ubuntu Playwright).
- EmptyState adoption: ProgramLogs + SiteAnnouncements.
- Residual: #538 real authed page shots; Accounts/OAuth/ProxyLogs empty migration; focus-trap/hex.

## [2026-07-19] #541 EmptyState page adoption (DownstreamKeys/CheckinLog/Models)

- Migrated empty surfaces to design-system `EmptyState` + primary action.
- Residual: remaining pages (Accounts/Sites/Logs/OAuth/…); Tokens panel is redirect-only.

## [2026-07-19] UI-REFRESH Phase 4/5/shell **source** merge + EmptyState

- Fixed incomplete prior tip: worktree source for #537/#540/#538 actually merged to product tree (forms/a11y/shell mock).
- #541: `EmptyState` ds primitive + gallery samples; legacy `.empty-state` retokenized.
- Playwright e2e 7/7; gallery score axes 5/5; win32 baselines refreshed.
- Residual: #539 Linux CI baselines; real authed Dashboard/Sites/Settings; focus-trap/hex.

## [2026-07-19] UI-REFRESH Phase 4/5 + shell mock integrated

- Phase 4 (#537): form/drawer/modal Apple-detail density (36px controls, glass chrome, gallery samples).
- Phase 5 (#540): prefers-reduced-motion hard-cut + reduced-transparency solid glass fallbacks.
- #538: auth-free shell chrome mock (Dashboard/Sites/Settings) + capture SOP + METAPI_PW_FORCE_SERVER.
- Security: TokenRoutes escapeHtml also escapes apostrophe for dialog HTML.
- e2e 7/7 green; gallery score axes 5/5 after shell mock height growth.

## [2026-07-19] docs: M51 Phase 4–5 board + worktree lanes

- Open issues #537–#541 on Milestone 51 (forms · shell shots · linux baselines · a11y · empty/error).
- Worktree lanes: `ui/phase4-forms` · `ui/shell-page-shots` · `ui/phase5-a11y` under `.worktrees/*`.
- MASTER/STATE board lists #532–#541; Phase 1–3 remains on master (`af3a4d2`); Phase 4–5 not yet merged.

## [2026-07-19] UI-REFRESH Phase 3 data surfaces + scored gallery

- Token-only polish: dual-theme semantic `*-ink`, purple badge family; tables/filters/pagination/toasts/badges retokenized in `index.css`.
- Gallery sample: filter chips + pill tabs + data-table + pagination; win32 visual baselines refreshed; `npm run test:e2e` 7/7 green.
- Capture script `web/scripts/capture-ui-shots.mjs` + `docs/analysis/ui-shots/*` (login/gallery light+dark).
- Residual: Linux baselines, Dashboard/Sites/Settings shell screenshots, Phase 4 forms/drawers.

## [2026-07-19] UI-REFRESH GCP/Apple token + card density

- Primary remapped to GCP blue (`#1a73e8` / dark `#8ab4f8`); cool gray accent; FOUC canvas retained.
- New semantic radius (`control`/`card`/`shell`), dual soft shadows, `motion-swift`/`motion-soft`.
- Cards/stat-cards/page-header + design-system primitives consume new tokens; DESIGN.md rewritten.

## [2026-07-19] UI-REFRESH Phase 2 shell glass

- CSS-only glass chrome in `web/index.css`: topbar, sidebar, user-dropdown, mobile drawer, login surfaces.
- Token-only (`--glass-*`); `@supports` + `prefers-reduced-transparency` solid fallbacks.
- Login/sidebar unit tests green; typecheck green.

## [2026-07-19] UI-REFRESH Phase 1 foundation in tree

- FOUC #535: `themeBootstrap` + head script theme_mode-first; canvas #0b0f14/#f4f6f8; unit + e2e contracts.
- Design system #533: `web/design-system/*` primitives (ds-*) + `/__design__` gallery (auth-free when DEV or `metapi_design_gallery=1`); glass tokens.
- Visual/e2e #534/#536: Playwright harness (`web/e2e/*`, Makefile `ui-e2e`/`ui-visual`, CI `ui-visual.yml`); vitest excludes e2e.
- Residual: gallery snapshot baselines, DESIGN.md full rewrite, shell glass Phase 2.

## [2026-07-19] UI-REFRESH M51 opened + multi-lane kickoff

- Milestone 51 UI-REFRESH; issues #532 epic, #535 FOUC, #533 design-system, #534 visual, #536 e2e.
- Session loop every 10m; lanes: FOUC / design-system / visual+e2e harness.

## [2026-07-19] design: formal readiness + UI-REFRESH

- Added `docs/analysis/formal-readiness.md` — Track A 对内正式可用（已达标）vs Track B 对外完备（未达标）；T0–T4 运行档位；Redis 可选。
- Added `docs/analysis/ui-ux-refresh.md` — GCP IA + 白磨砂玻璃 + 苹果细节；FOUC/夜间闪光弹 P0；分 Phase 落地，未改 web 实现。

## [2026-07-19] ops: hk3 deploy v0.8.44 shared-tiny

- Pin + up `td-metapi` 0.8.44; compose force `DB_PROFILE=shared-tiny` + MaxOpen/Idle 1/1 + `application_name=metapi-hk3`; restart=no.
- Verified healthy/ready; metrics open=1 errors=0; Azure backends=1; no 53300; NewAPI ok.

## [2026-07-19] #531 PostgreSQL pool budget profiles + lease pressure

- Product: `DB_PROFILE` shared-tiny/normal/dedicated; default normal 10/3 (dedicated still 20/5 for large DBs).
- Inject `application_name=metapi-<host>`; startup banner logs profile + pool.
- Scheduler lease: MaxOpen≤2 → local; 53300 backoff + log denoise + force-local.
- Metrics: db_connections_in_use + db_conn_errors_total.
- Docs: `docs/analysis/db-pool-budget.md`; CHANGELOG v0.8.44 pending tag/deploy.

## [2026-07-19] M50 v0.8.43 residual honesty + us1 pin

- GitHub Milestone 50 + Project items #527–#530.
- P0-585 unit load-proof tests (5xx storm + 429 same-channel policy); P0-585 stays partial.
- P0-555 Gemini SSE usageMetadata honesty tests; stays present-with-residual.
- us1 `/opt/tokendance-us1` compose pin 0.8.42 + pull; cold no auto-start.
- Docs: residual / high-value-next / MASTER / STATE / CHANGELOG.

## [2026-07-18] docs: high-value-next shortlist (ours vs original)

- Add `docs/analysis/high-value-next.md` separating metapi-go residual from cita-777/metapi parity leftovers.
- Banner matrix/sources as historical; residual header → post v0.8.42; wire README/STATE/MASTER entry points.
- No product board opened; maintenance default remains.

## [2026-07-18] v0.8.42 cron validation + prod roll-forward

- Fix: config `validateCronExpr` accepts default 5-field crons (parity with scheduler normalize).
- Ship/tag v0.8.42; deploy hk3 pin 0.8.42; generate `ACCOUNT_CREDENTIAL_SECRET` when missing (no OAuth client invent).
- Residual: OAuth client placeholders remain intentional until real client IDs are configured.

## [2026-07-18] deploy v0.8.41 to hk3 (0.6.5 → 0.8.41)

- Tags: v0.8.40 (PG pool + docs) · **v0.8.41** (request_id index upgrade fix for old DBs).
- Prod: Azure PG `tokendance-pg` / role `metapi`; container `td-metapi` healthy; migrations sc2_001–006 applied.
- Ops fix: role CONNECTION LIMIT 2→15; app pool max_open=5 idle=2.
- Evidence: `/health` `/ready database=ok`; admin auth OK; 103 sites; public 302 to ID.

## [2026-07-18] neat-freak: STATE/MASTER/LOG roles + branch hygiene

- Closed M49 / shipped **v0.8.39**; board empty.
- Post-tag **#526** landed on master: explicit PostgreSQL pool budget (config + store + docs).
- Progress docs split: **STATE** = 现状, **MASTER** = 开放门禁, **LOG** = 本文件; no HANDOFF SSOT.
- Pruned ~255 agent worktrees → main only; deleted merged-PR remote heads (~200+) and abandoned leftovers; local non-master cleaned.
- Memory pointer updated for metapi-go docs map.

## [2026-07-18] v0.8.39 / M49 adversarial bugfix residual

- Product: RR fail-count, used_requests 429 order, Redis admit rollback, max_cost wire, Gemini path/stream, retention RFC3339 (#511–#516).
- Docs honesty #517; release docs #525; tag + GitHub Release published; Milestone 49 closed.

## Earlier residual train

- v0.8.18–v0.8.38 narrative: root `CHANGELOG.md` + GitHub Releases (do not duplicate here).
