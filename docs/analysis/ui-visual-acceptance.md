# UI visual acceptance + UX e2e harness

**Date:** 2026-07-19  
**Backlog:** [#534](https://github.com/TokenDanceLab/metapi-go/issues/534) · [#536](https://github.com/TokenDanceLab/metapi-go/issues/536) · [#538](https://github.com/TokenDanceLab/metapi-go/issues/538) · [#544](https://github.com/TokenDanceLab/metapi-go/issues/544)  
**Status:** harness green; shell mock + **real authed page sample present** (`page-*-win32.png`, empty-DB honesty); win32+linux gallery baselines committed  
**Code:** `web/playwright.config.ts` · `web/e2e/**` · `web/pages/DesignSystemGallery.tsx` · `web/scripts/capture-ui-shots.mjs`

## Goal

Every design-system / shell chrome change can be Vite-accepted with Playwright baselines, and critical UX (theme bootstrap, login smoke) stays green without a live API backend. Post-login shell composition (Dashboard / Sites / Settings) is scored via a **gallery shell mock** when credentials are unavailable, and via **real page shots** when `METAPI_UI_AUTH_TOKEN` is set.

## Commands (from `web/`)

| Script | Purpose |
|:-------|:--------|
| `npm run test:e2e` | Full Playwright suite (`e2e/**`) |
| `npm run test:visual` | Gallery visual baselines only |
| `npm run test:visual:update` | Refresh gallery snapshots after intentional UI change |
| `node scripts/capture-ui-shots.mjs` | Human-score PNGs under `docs/analysis/ui-shots/` |

Root Makefile:

```bash
make ui-e2e      # → cd web && npm run test:e2e
make ui-visual   # → cd web && npm run test:visual
```

First-time browser install:

```bash
cd web
npm install
npx playwright install chromium
```

## Runtime model

`playwright.config.ts` starts:

1. `npm run build:web`
2. `vite preview --host 127.0.0.1 --port 4173 --strictPort`

Tests use `baseURL=http://127.0.0.1:4173`.

### Port 4173 / reuseExistingServer pitfall

Locally Playwright **reuses** an existing server on `:4173` (`reuseExistingServer: !CI`). If a **foreign** process already listens there (another project, stale preview, wrong build), gallery specs soft-skip or snapshot against the wrong app — looks like “visual tests passed/skipped” with no useful artifact.

**Mitigations:**

1. Free the port before `test:visual` / `test:e2e` (Windows: `Get-NetTCPConnection -LocalPort 4173`).
2. Force a clean preview for this repo:

```bash
# from web/
$env:METAPI_PW_FORCE_SERVER = '1'   # PowerShell
# or: METAPI_PW_FORCE_SERVER=1
npm run test:visual
```

`METAPI_PW_FORCE_SERVER=1` sets `reuseExistingServer: false` so Playwright always builds + boots this tree’s preview (fails if port busy — kill the occupant first).

CI always boots fresh (`reuseExistingServer: false` when `CI` is set).

## Specs

| File | Asserts |
|:-----|:--------|
| `e2e/visual-gallery.spec.ts` | Light + dark full-page screenshots of `/__design__`. Sets `metapi_design_gallery=1`; **skips** when marker missing. |
| `e2e/ux-theme.spec.ts` | `addInitScript` sets `theme_mode=dark\|light` → `html[data-theme]` matches; login surface has no hard console errors. |
| `e2e/fouc-bootstrap.spec.ts` | `theme_mode=dark` alone → head bootstrap sets `data-theme=dark` by `load` (FOUC-1). |

### Theme keys

| Key | Role |
|:----|:-----|
| `theme_mode` | Canonical App preference (`system` \| `light` \| `dark`) — `THEME_MODE_STORAGE_KEY` |
| `theme` | Legacy key kept in sync by App when mode is light/dark |
| `metapi_design_gallery` | `'1'` enables `/__design__` outside DEV builds |

FOUC Phase 1 (#535): inline head script is **theme_mode-first** (see `web/themeBootstrap.ts` + `web/index.html`).

## Shell page screenshots (#538)

### Approach

| Track | What | Auth | CI-friendly |
|:------|:-----|:-----|:------------|
| **B – gallery shell mock** | `/__design__` section `Shell chrome mock` reuses production `topbar` / `sidebar` / `page-header` / `data-table` classes with static Dashboard · Sites · Settings content | none | yes |
| **A – real pages** | `capture-ui-shots.mjs` with `METAPI_UI_AUTH_TOKEN` hits `/`, `/sites`, `/settings` against a live API-backed UI | required | no |

Default CI-friendly acceptance for material / brand_calm / spacing / elevation / dark_parity uses **shell mock** artifacts:

- `docs/analysis/ui-shots/shell-dashboard-{light\|dark}-win32.png`
- `docs/analysis/ui-shots/shell-sites-{light\|dark}-win32.png`
- `docs/analysis/ui-shots/shell-settings-{light\|dark}-win32.png`

**Real track sample present** (#544) — empty-state first-run honesty on local sqlite is OK:

- `docs/analysis/ui-shots/page-{dashboard\|sites\|settings}-{light\|dark}-win32.png`
- Score note: `docs/analysis/ui-score-pages-2026-07-19.md`

**Shell mock nav (2026-07-20):** mock sidebar lists the same **14** product labels as `App.sidebarGroups` (only Dashboard/Sites/Settings switch mock content). Truncated mock nav is **not** product IA — see [`ui-original-parity-2026-07-20.md`](./ui-original-parity-2026-07-20.md).

### Capture SOP (human score)

From a **built** `web/` tree (or let the script start `vite preview` on `:4181`):

```bash
cd web
npm run build:web

# Mock + login + gallery (no backend)
node scripts/capture-ui-shots.mjs

# Optional: real shell pages (admin bearer token; API must accept it)
# Auth keys: localStorage auth_token + auth_token_expires_at (see web/authSession.ts)
# Do NOT echo / print the token. Prefer env already set in the shell session.
# $env:METAPI_UI_AUTH_TOKEN = '<set-once-do-not-log>'
# Optional cookie header fragments:
# $env:METAPI_UI_AUTH_COOKIE = 'meta_monitor_auth=...'
# Optional: point at an already-running UI instead of local preview:
# $env:METAPI_UI_SHOT_BASE = 'http://127.0.0.1:3000'
node scripts/capture-ui-shots.mjs
```

### Re-run real pages one-liner (no secret printing)

Point at a live API-backed UI (or leave `METAPI_UI_SHOT_BASE` unset to use local vite preview on `:4181`). Token must already be in the environment — never `echo` / `Write-Host` it.

```powershell
# PowerShell — token already exported in this session
cd web
$env:METAPI_UI_SHOT_BASE = 'http://127.0.0.1:3000'   # optional live UI
# $env:METAPI_UI_AUTH_TOKEN = '<set-once-do-not-log>'
node scripts/capture-ui-shots.mjs
```

```bash
# bash — same idea
cd web
export METAPI_UI_SHOT_BASE="${METAPI_UI_SHOT_BASE:-}"   # optional
# export METAPI_UI_AUTH_TOKEN='…'   # set once; do not print
node scripts/capture-ui-shots.mjs
```

Empty DB is acceptable for honesty scores (first-run EmptyStates). Populated fixtures are optional residual.

Env vars:

| Variable | Purpose |
|:---------|:--------|
| `METAPI_UI_AUTH_TOKEN` | Bearer admin token → `localStorage.auth_token` |
| `METAPI_AUTH_TOKEN` | Alias for the above |
| `METAPI_UI_AUTH_COOKIE` | Optional `Cookie` header string (`a=b; c=d`) |
| `METAPI_UI_SHOT_BASE` | Skip local preview; use this origin |
| `METAPI_UI_SHOT_PORT` | Local preview port (default `4181`) |

Gallery mock marker: `[data-testid="shell-chrome-mock"]` with `data-shell-page=dashboard|sites|settings`.

Score axes (target ≥4/5): material, brand_calm, spacing, elevation, dark_parity. Record in `docs/analysis/ui-score-*.md` or issue comment on [#532](https://github.com/TokenDanceLab/metapi-go/issues/532).

## Updating baselines

```bash
cd web
# Prefer free :4173 or force clean server
$env:METAPI_PW_FORCE_SERVER = '1'
npm run test:visual:update
```

Commit new files under `web/e2e/**/*-snapshots/` only after human visual review. Linux SSOT files: `design-gallery-{light,dark}-chromium-linux.png` (from CI actuals / Linux runner). Windows font AA may drift — CI is the SSOT (`maxDiffPixelRatio: 0.02`).

**When to refresh gallery baselines:** any intentional change to `/__design__` layout height/composition (including the #538 shell mock section). Skip update for pure docs/script-only changes.

## CI

Workflow: [`.github/workflows/ui-visual.yml`](../../.github/workflows/ui-visual.yml)

- Triggers on `web/**`, the workflow file, and this runbook
- Node 25, `npm ci`, Playwright Chromium, `npm run test:e2e`
- On failure uploads `web/test-results` + `web/playwright-report`

## Local artifacts (do not commit)

- `web/test-results/`
- `web/playwright-report/`
- `web/blob-report/`

## Acceptance checklist

- [x] `test:e2e` / `test:visual` / `test:visual:update` scripts
- [x] `playwright.config.ts` preview on `:4173` after build
- [x] Gallery light+dark screenshots (skip-safe without route)
- [x] UX theme + FOUC bootstrap specs with `theme_mode=dark`
- [x] Makefile `ui-visual` / `ui-e2e`
- [x] GH workflow path-filtered on `web/**`
- [x] Gallery baselines: win32 + Linux CI SSOT committed (`web/e2e/visual-gallery.spec.ts-snapshots/*-{win32,linux}.png`, #539)
- [x] Shell chrome mock on `/__design__` for Dashboard/Sites/Settings (#538)
- [x] `capture-ui-shots.mjs` auth env + shell mock shots SOP
- [x] Document `:4173` reuse pitfall + `METAPI_PW_FORCE_SERVER=1`
- [x] Real authed page sample `page-*-win32.png` + score honesty (#544)

## Non-goals

- Editing product pages / `index.html` / design-system sources from the harness lane alone (shell mock reuses existing classes only)
- Backend-authenticated e2e in CI (sites/accounts still unit/vitest + API tests)
- Cross-browser matrix beyond Chromium (expand later if needed)
