# MetAPI Design System

**Product**: TokenDance / MetAPI admin  
**Scope**: Enterprise ops control plane (sites, accounts, tokens, routes, monitors, logs)  
**Visual language**: GCP cloud console IA + frosted glass + Apple detail (UI-REFRESH M51)  
**Source of truth**: this document + `web/styles/tokens.css` + `web/design-system/**`  
**Last updated**: 2026-07-19  
**Related**: [`components.md`](./components.md) · [`../analysis/ui-ux-refresh.md`](../analysis/ui-ux-refresh.md) · gallery `/__design__`

---

## 1. Brand

| Attribute | Decision |
|-----------|----------|
| Product voice | Professional, dense, high-signal ops UI |
| Audience | Operators managing multi-site API gateways, keys, and routing |
| Personality | Calm GCP control room with Apple-grade materials — not consumer marketing |
| Density default | Comfortable-dense (admin tables + KPI cards coexist) |
| Brand color | **GCP Blue** `#1a73e8` (dark `#8ab4f8`) with cool gray accent |
| Logo mark | Soft blue gradient chip (no neon indigo/cyan marketing pair) |

**Principles**

1. **Signal over decoration** — every color/weight change means status, severity, or hierarchy.
2. **Token-first** — no new hard-coded hex in pages; use CSS custom properties.
3. **Dual theme parity** — light and dark share the same semantic token names.
4. **Dense but breathing** — 12–14px body, clear table rhythm, Apple spacing ladder.
5. **One glass system** — shell/modal/dropdown only; never blur table rows.
6. **Progressive adoption** — primitives in `web/design-system`; migrate pages gradually.

---

## 2. Color tokens

All values live in `web/styles/tokens.css` under `:root` (light) and `[data-theme="dark"]` (dark).

### 2.1 Surfaces

| Token | Light | Dark | Usage |
|-------|-------|------|-------|
| `--color-bg` | `#eef2f6` | `#0b0f14` | App canvas (matches FOUC bootstrap) |
| `--color-bg-secondary` | `#e4eaf0` | `#12181f` | Nested wells, inset panels |
| `--color-bg-card` | `#ffffff` | `#161b22` | Cards, tables, drawers |
| `--color-bg-sidebar` | translucent white | translucent deep slate | Sidebar (prefer glass tokens) |
| `--color-bg-topbar` | translucent white | translucent deep slate | Topbar base |
| `--color-bg-elevated` | `#ffffff` | `#1c2330` | Popovers, solid fallbacks |
| `--color-bg-hover` | black 4% | white 5% | Row/button hover wash |
| `--color-bg-active` | black 6% | white 8% | Pressed / selected wash |

### 2.2 Brand / accent

| Token | Light | Dark | Usage |
|-------|-------|------|-------|
| `--color-primary` | `#1a73e8` | `#8ab4f8` | Primary actions, active nav |
| `--color-primary-hover` | `#1557b0` | `#aecbfa` | Hover on primary |
| `--color-primary-light` | `#e8f0fe` | `#1a2b45` | Soft primary fill / active chip |
| `--color-accent` | `#5f6368` | `#9aa0a6` | Cool gray secondary (not cyan neon) |
| `--color-brand-gradient` | blue ramp | elevated blue ramp | Logo chip only |

### 2.3 Glass material

| Token | Purpose |
|-------|---------|
| `--glass-bg` / `--glass-bg-strong` | Translucent panel fills |
| `--glass-border` | Hairline glass edge |
| `--glass-blur` / `--glass-saturate` | Backdrop filter recipe |
| `--shadow-glass` | Soft elevation + inset highlight |

Fallbacks: `@supports not (backdrop-filter)` and `prefers-reduced-transparency: reduce` → solid elevated surface.

### 2.4 Semantic status

Unchanged roles: success / warning / danger / info with solid + soft pairs. Badge text uses solid on soft fill.

---

## 3. Spacing, radius, elevation, motion

| Family | Tokens | Notes |
|--------|--------|-------|
| Spacing | `--space-0…12`, `--gap-tight/default/section`, `--space-main` | 4px base; section gap 20px |
| Radius | `--radius-control` 10 · `--radius-card` 14 · `--radius-shell` 16 | Buttons/inputs = control; cards = card |
| Shadow | `--shadow-sm/md/lg/card/elevated/glass` | Dual ambient+key; dark uses emissive border |
| Motion | `--motion-swift`, `--motion-soft`, `--ease-spring` | Calm; honor `prefers-reduced-motion` |

---

## 4. Components

Primitive inventory and API: [`components.md`](./components.md).

| Layer | Prefix / classes | Where |
|-------|------------------|-------|
| Design system | `ds-*` | `web/design-system/**` |
| Legacy shell | `.topbar` `.sidebar` `.card` `.stat-card` | `web/index.css` |
| Gallery | `/__design__` | DEV or `localStorage.metapi_design_gallery=1` |

New UI must start from design-system primitives when possible. Legacy classes are being retokenized in place.

---

## 5. Visual acceptance

1. `cd web && npm run test:visual` — gallery light/dark baselines  
2. `npm run test:e2e` — FOUC + theme UX  
3. Manual score rubric (target ≥ 4/5 each):  
   - Material (glass/solid hierarchy)  
   - Brand calm (GCP blue, no neon)  
   - Spacing rhythm  
   - Card elevation / radius  
   - Motion restraint  
   - Dark parity  

Runbook: [`../analysis/ui-visual-acceptance.md`](../analysis/ui-visual-acceptance.md).

---

## 6. a11y non-negotiables

- Focus-visible rings via `--color-focus-ring-strong`
- Contrast on soft badges in both themes
- `prefers-reduced-transparency` / `prefers-reduced-motion`
- FOUC: `theme_mode`-first bootstrap; no white flash in dark

Checklist: [`a11y-checklist.md`](./a11y-checklist.md).

---

## 7. Change log (visual)

| Date | Change |
|------|--------|
| 2026-07-16 | U0 token freeze (indigo era) |
| 2026-07-19 | UI-REFRESH: FOUC canvas, glass family, GCP primary, card density, shell glass |
| 2026-07-19 | Phase 3: dual-theme semantic ink, purple badge, table/filter/pagination/toast retokenize |
