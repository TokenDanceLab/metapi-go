# log.md — MetAPI Go progress log

> **进度日志**（append-only）。不是现状 SSOT。  
> 现状 → [`STATE.md`](STATE.md) · 开放项 → [`progress/MASTER.md`](progress/MASTER.md)

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
