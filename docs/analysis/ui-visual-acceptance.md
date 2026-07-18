# UI visual acceptance + UX e2e harness

**Date:** 2026-07-19  
**Backlog:** [#534](https://github.com/TokenDanceLab/metapi-go/issues/534) · [#536](https://github.com/TokenDanceLab/metapi-go/issues/536)  
**Status:** harness present; `/__design__` gallery scaffolded (#533); baselines still to commit after first green run  
**Code:** `web/playwright.config.ts` · `web/e2e/**` · `web/pages/DesignSystemGallery.tsx`

## Goal

Every design-system / shell chrome change can be Vite-accepted with Playwright baselines, and critical UX (theme bootstrap, login smoke) stays green without a live API backend.

## Commands (from `web/`)

| Script | Purpose |
|:-------|:--------|
| `npm run test:e2e` | Full Playwright suite (`e2e/**`) |
| `npm run test:visual` | Gallery visual baselines only |
| `npm run test:visual:update` | Refresh gallery snapshots after intentional UI change |

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

Tests use `baseURL=http://127.0.0.1:4173`. Locally the server is reused when already up (`reuseExistingServer`); CI always boots fresh.

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

## Updating baselines

```bash
cd web
npm run test:visual:update
```

Commit new files under `web/e2e/**/*-snapshots/` only after human visual review. Prefer Linux CI baselines; Windows font AA may drift — CI is the SSOT (`maxDiffPixelRatio: 0.02`).

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
- [x] Gallery baselines: win32 committed (`web/e2e/visual-gallery.spec.ts-snapshots/*-win32.png`); **Linux CI SSOT still residual** (`*-linux.png`)

## Non-goals

- Editing product pages / `index.html` / design-system sources from this harness lane
- Backend-authenticated flows (sites, accounts) — unit/vitest and API tests own those
- Cross-browser matrix beyond Chromium (expand later if needed)
