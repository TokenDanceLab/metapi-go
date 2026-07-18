# MetAPI Design System

**Product**: TokenDance / MetAPI admin  
**Scope**: Enterprise ops control plane (sites, accounts, tokens, routes, monitors, logs)  
**Issue**: #12 (U0) — foundation only; component rewrites land in U1–U3  
**Source of truth**: this document + `web/styles/tokens.css`  
**Last updated**: 2026-07-16  
**Next visual language**: [`../analysis/ui-ux-refresh.md`](../analysis/ui-ux-refresh.md) (design-only; FOUC + GCP/glass/Apple) — this file remains token SSOT until Phase 1 lands

---

## 1. Brand

| Attribute | Decision |
|-----------|----------|
| Product voice | Professional, dense, high-signal ops UI |
| Audience | Operators managing multi-site API gateways, keys, and routing |
| Personality | Calm control room — not consumer SaaS marketing |
| Density default | Comfortable-dense (admin tables + KPI cards coexist) |
| Brand color | Indigo primary `#4f46e5` with cyan accent `#06b6d4` |
| Logo mark | Indigo → cyan 135° gradient chip |

**Principles**

1. **Signal over decoration** — every color/weight change means status, severity, or hierarchy.
2. **Token-first** — no new hard-coded hex in pages; use CSS custom properties.
3. **Dual theme parity** — light and dark must share the same semantic token names.
4. **Dense but readable** — prefer 12–14px body, tight gaps, clear table rhythm.
5. **Progressive adoption** — U0 freezes tokens; U1 migrates shared components; U2 pages; U3 a11y/responsive polish.

---

## 2. Color tokens

All values live in `web/styles/tokens.css` under `:root` (light) and `[data-theme="dark"]` (dark).

### 2.1 Surfaces

| Token | Light | Dark | Usage |
|-------|-------|------|-------|
| `--color-bg` | `#f5f5f5` | `#0f0f0f` | App canvas / page background |
| `--color-bg-secondary` | `#eeeeee` | `#161616` | Nested wells, inset panels |
| `--color-bg-card` | `#ffffff` | `#1a1a1a` | Cards, tables, drawers |
| `--color-bg-sidebar` | `#ffffff` | `#141414` | Sidebar surface |
| `--color-bg-topbar` | `#ffffff` | `#141414` | Topbar base (glass may overlay) |
| `--color-bg-elevated` | `#ffffff` | `#222222` | Popovers, dropdowns, floating menus |
| `--color-bg-hover` | `rgba(0,0,0,0.04)` | `rgba(255,255,255,0.05)` | Row/button hover wash |
| `--color-bg-active` | `rgba(0,0,0,0.06)` | `rgba(255,255,255,0.08)` | Pressed / selected wash |

### 2.2 Borders

| Token | Light | Dark | Usage |
|-------|-------|------|-------|
| `--color-border` | `#e8e8e8` | `#2a2a2a` | Default dividers, inputs, cards |
| `--color-border-light` | `#f0f0f0` | `#222222` | Subtle internal rules |
| `--color-border-strong` | `#d4d4d4` | `#3a3a3a` | Emphasis frames, focus rings base |

### 2.3 Text

| Token | Light | Dark | Usage |
|-------|-------|------|-------|
| `--color-text-primary` | `#1a1a1a` | `#f0f0f0` | Titles, primary values |
| `--color-text-secondary` | `#666666` | `#a0a0a0` | Labels, nav, meta |
| `--color-text-tertiary` | `#8a8a8a` | `#7a7a7a` | Helper copy between secondary and muted |
| `--color-text-muted` | `#999999` | `#666666` | Placeholders, disabled-ish hints |
| `--color-text-inverse` | `#ffffff` | `#0f0f0f` | Text on solid brand/danger fills |
| `--color-text-link` | `var(--color-primary)` | `var(--color-primary)` | Inline links |

### 2.4 Brand / accent

| Token | Light | Dark | Usage |
|-------|-------|------|-------|
| `--color-primary` | `#4f46e5` | `#6366f1` | Primary actions, active nav |
| `--color-primary-hover` | `#4338ca` | `#818cf8` | Hover on primary |
| `--color-primary-light` | `#eef2ff` | `#1e1b4b` | Soft primary fill / active chip |
| `--color-accent` | `#06b6d4` | `#22d3ee` | Secondary brand accent (logo, charts) |
| `--color-accent-soft` | `#ecfeff` | `#083344` | Soft cyan wash |

### 2.5 Semantic status

| Role | Solid | Soft fill | Typical use |
|------|-------|-----------|-------------|
| Success | `--color-success` `#16a34a` / dark `#4ade80` | `--color-success-soft` | Healthy, completed, positive delta |
| Warning | `--color-warning` `#d97706` / dark `#fbbf24` | `--color-warning-soft` | Degraded, pending attention |
| Danger | `--color-danger` `#dc2626` / dark `#f87171` | `--color-danger-soft` | Errors, destructive, offline |
| Info | `--color-info` `#2563eb` / dark `#60a5fa` | `--color-info-soft` | Neutral informational callouts |

Badge/alert text should use solid tokens (or darkened variants) on soft fills — never invent one-off greens/reds.

### 2.6 Chart series

Stable 8-stop categorical palette (Tailwind-like spectrum already used by charts):

| Token | Hex |
|-------|-----|
| `--color-chart-1` | `#4f46e5` |
| `--color-chart-2` | `#06b6d4` |
| `--color-chart-3` | `#10b981` |
| `--color-chart-4` | `#f59e0b` |
| `--color-chart-5` | `#ef4444` |
| `--color-chart-6` | `#8b5cf6` |
| `--color-chart-7` | `#ec4899` |
| `--color-chart-8` | `#14b8a6` |

Series colors stay fixed across themes for operator muscle memory; axis/label colors read from text tokens via `useThemeLabelColor`.

### 2.7 Stat icon chips (accent family)

Soft pastel chips for dashboard KPI icons — tokenized so dark mode can remap:

| Token pair | Light fill / ink |
|------------|------------------|
| blue | `#dbeafe` / `#2563eb` |
| green | `#dcfce7` / `#16a34a` |
| yellow | `#fef3c7` / `#d97706` |
| pink | `#fce7f3` / `#db2777` |
| red | `#fee2e2` / `#dc2626` |
| orange | `#ffedd5` / `#ea580c` |
| purple | `#ede9fe` / `#7c3aed` |
| cyan | `#cffafe` / `#0891b2` |

Exposed as `--color-stat-{name}` / `--color-stat-{name}-ink`.

### 2.8 Focus & overlays

| Token | Light | Dark |
|-------|-------|------|
| `--color-focus-ring` | `rgba(79,70,229,0.45)` | `rgba(99,102,241,0.55)` |
| `--color-overlay` | `rgba(15,15,15,0.45)` | `rgba(0,0,0,0.65)` |
| `--color-scrollbar` | `#cfcfcf` | `#3a3a3a` |

---

## 3. Typography

### 3.1 Families

| Token | Stack |
|-------|-------|
| `--font-sans` | `'Inter', -apple-system, BlinkMacSystemFont, 'PingFang SC', 'Noto Sans SC', 'Microsoft YaHei', 'Segoe UI', Roboto, sans-serif` |
| `--font-mono` | `'SF Mono', 'Cascadia Code', 'Fira Code', Consolas, 'Liberation Mono', Menlo, monospace` |

CJK fallbacks are mandatory for operator-facing Chinese copy.

### 3.2 Scale

| Token | Size | Line-height | Weight default | Role |
|-------|------|-------------|----------------|------|
| `--text-xs` | 11px | 1.35 | 500 | Badges, tiny meta |
| `--text-sm` | 12px | 1.4 | 400/500 | Table secondary, chips |
| `--text-md` | 13px | 1.45 | 400 | Dense body / form labels |
| `--text-base` | 14px | 1.5 | 400 | Default body, nav |
| `--text-lg` | 15px | 1.45 | 600 | Logo text, section labels |
| `--text-xl` | 18px | 1.35 | 600 | Page subtitle emphasis |
| `--text-2xl` | 22px | 1.3 | 700 | Page titles |
| `--text-3xl` | 28px | 1.25 | 700 | KPI hero numbers (optional) |

### 3.3 Weights

| Token | Value |
|-------|-------|
| `--font-weight-regular` | 400 |
| `--font-weight-medium` | 500 |
| `--font-weight-semibold` | 600 |
| `--font-weight-bold` | 700 |

### 3.4 Rules

- Prefer `tabular-nums` for balances, latencies, counters.
- Mono only for keys, IDs, request IDs, raw payloads.
- Do not drop below 11px for interactive labels.

---

## 4. Spacing

4px base unit. Prefer tokens over ad-hoc `px` in new work.

| Token | Value |
|-------|-------|
| `--space-0` | 0 |
| `--space-1` | 4px |
| `--space-2` | 8px |
| `--space-3` | 12px |
| `--space-4` | 16px |
| `--space-5` | 20px |
| `--space-6` | 24px |
| `--space-8` | 32px |
| `--space-10` | 40px |
| `--space-12` | 48px |

**Common layout constants** (also tokens):

| Token | Value | Role |
|-------|-------|------|
| `--space-main` | 24px | `.main-content` padding |
| `--topbar-height` | 56px | Sticky topbar |
| `--sidebar-width` | 220px | Expanded sidebar |
| `--sidebar-collapsed-width` | 64px | Icon rail |
| `--gap-tight` | 4px | Chip groups |
| `--gap-default` | 8px | Toolbar clusters |
| `--gap-section` | 16px | Card stacks |

Historical paddings in CSS (6/10/14px) remain valid transitional values; new components should snap to the 4px ladder.

---

## 5. Radius

| Token | Value | Use |
|-------|-------|-----|
| `--radius-xs` | 4px | Tiny chips, status dots wrappers |
| `--radius-sm` | 8px | Buttons, inputs, logo chip |
| `--radius-md` | 12px | Cards, search trigger, selects |
| `--radius-lg` | 16px | Modals, large panels |
| `--radius-xl` | 20px | Marketing-like login surfaces |
| `--radius-full` | 9999px | Pills, avatars |

---

## 6. Elevation (shadows)

| Token | Light | Dark | Use |
|-------|-------|------|-----|
| `--shadow-sm` | soft 1–3px gray | deeper black | Subtle lift |
| `--shadow-md` | 4–12px | stronger | Dropdowns, floating bars |
| `--shadow-lg` | 10–30px | strong | Modals |
| `--shadow-card` | 1px card edge | soft | Default card resting state |

Avoid stacking multiple large shadows; one elevation level per surface.

---

## 7. Motion

| Token | Value | Use |
|-------|-------|-----|
| `--duration-instant` | 100ms | Micro feedback |
| `--duration-fast` | 150ms | Hover, focus |
| `--duration-normal` | 200ms | Buttons, nav |
| `--duration-moderate` | 300ms | Page enter, fade |
| `--duration-slow` | 450ms | Complex panel transitions |
| `--ease-standard` | `cubic-bezier(0.4, 0, 0.2, 1)` | Default |
| `--ease-emphasized` | `cubic-bezier(0.22, 1, 0.36, 1)` | Slide-up / enter |
| `--ease-linear` | `linear` | Spinners |

Named keyframes already in `web/index.css` (`fade-in`, `slide-up`, `scale-in`, `toast-in`, `skeleton-wave`, …) remain the animation vocabulary. Prefer `prefers-reduced-motion: reduce` to collapse non-essential motion (U3).

---

## 8. Density & layout chrome

| Mode | Intent | Guidance |
|------|--------|----------|
| Default (ops) | Tables + filters + KPI | 13–14px type, 8–12px gaps |
| Compact (future) | Power-user tables | Reduce row padding ~20%; keep hit targets ≥32px |
| Comfortable | Settings forms | 16–24px section gaps |

Layout attributes (already used by app shell):

- `data-layout="mobile|desktop"` on root
- `data-theme="light|dark"` on `document.documentElement`

Shell metrics:

- Topbar 56px sticky glass
- Sidebar 220 / 64
- Main content padding 24px
- Z-index ladder: topbar 100 → mobile batch 210 → overlay 320 → drawer 340  
  Tokens: `--z-topbar`, `--z-mobile-batch-bar`, `--z-overlay`, `--z-drawer`, plus `--z-toast` (360), `--z-tooltip` (380)

---

## 9. Light / dark strategy

### 9.1 Current system (keep)

- Explicit dual theme via `document.documentElement` `data-theme="light|dark"` (not Tailwind `dark:`).
- FOUC bootstrap in `web/index.html`: legacy `localStorage.theme`, else `prefers-color-scheme`.
- App theme mode: `system | light | dark` under `theme_mode`; fixed modes still write legacy `theme`.
- System mode tracks `matchMedia('(prefers-color-scheme: dark)')`.
- Charts/hooks observe `data-theme` (MutationObserver) and read CSS variables.

### 9.2 Token rules

1. Components consume **semantic** tokens only (`--color-bg-card`, `--color-danger`, …).
2. Dark theme remaps the same names — never branch on theme in component hex maps for core UI.
3. Exceptions allowed temporarily:
   - Vendor brand chips (`BrandIcon` one-off maps)
   - Chart categorical series (shared across themes by design)
4. Gaps to close in U1–U2:
   - Badges still using fixed light-only hex → soft semantic tokens
   - Undefined `--color-text-tertiary` / `--color-bg-secondary` references → **defined in U0**
   - Skeleton hard-coded grays → surface tokens
   - Topbar glass rgba → optional elevated tokens later

### 9.3 Target end-state

- 100% of admin chrome on tokens
- Badge / alert / toast / stat-icon families fully semantic
- Chart series read `--color-chart-*`
- Reduced-motion respected
- Contrast: body text ≥ 4.5:1 against surfaces in both themes (verify in visual checklist)

---

## 10. Component inventory & token adoption

Status legend:

| Status | Meaning |
|--------|---------|
| **planned** | Documented; migration not yet executed (U0) |
| **partial** | Uses some tokens + residual hard-coded hex |
| **adopted** | Fully on design tokens |

| Component / area | Path / class family | Status | Notes |
|------------------|---------------------|--------|-------|
| Design tokens | `web/styles/tokens.css` | **adopted** | U0 SSOT |
| Global import | `web/index.css` | **partial** | Imports tokens; component CSS still mixed |
| Layout shell | `.topbar`, `.sidebar`, `.app-layout`, `.main-content`, page-header | **partial** | Core vars used; glass/hover rgba remain |
| Auth / login | `.login-shell`, `.login-surface`, brand/auth panels | **partial** | Surfaces on tokens; decorative gradients TBD |
| Buttons | `.btn`, `.btn-primary`, soft/danger/success/ghost/link | **partial** | Primary on tokens; variants to unify |
| Cards / stats | `.card`, `.stat-card`, `.mobile-card*`, KPI chips | **partial** | Card bg/border tokenized |
| Tables | `.data-table`, domain tables, row states | **partial** | Borders/hover mixed |
| Forms / selects | inputs, `ModernSelect`, filter panels, toolbar-search | **partial** | Need focus-ring token |
| Feedback | alert, toast, spinner, skeleton, badge, empty-state | **partial** | Badge soft colors often hard-coded |
| Overlays / menus | modal, drawer, filter sheet, batch bar, dropdowns | **partial** | z-index tokens exist |
| Brand / model badges | `BrandIcon`, `BrandGlyph`, `ModelBadge`, `SiteBadgeLink` | **planned** | Vendor maps stay exception |
| Charts | SiteDistribution / SiteTrend / DownstreamKeyTrend | **planned** | Still local `COLOR_PALETTE` arrays |
| Domain panels | `ModelAnalysisPanel`, filter/mobile helpers | **partial** | |
| Hooks | `useIsMobile`, `useAnimatedVisibility`, `useThemeLabelColor` | **partial** | Theme observer OK |

U1 should prioritize: Button, Badge, Alert/Toast, Card, Table row states, Form controls, Modal shell.  
U2 migrates page-local hex. U3 covers a11y contrast, focus rings, reduced motion, responsive density.

---

## 11. CSS architecture

```
web/index.css
  ├── @import "tailwindcss";          /* utility import only; almost no utilities used */
  ├── @import "./styles/tokens.css";  /* design tokens SSOT */
  └── component / layout rules        /* semantic classes */
```

Rules:

1. **Do not** expand Tailwind utility usage for product chrome; keep semantic CSS classes + `style={{ color: 'var(--color-*)' }}`.
2. New tokens only in `tokens.css` (and this doc).
3. No product API or backend coupling.
4. Prefer additive token aliases over renames to avoid churn.

---

## 12. Visual review checklist

Use before merging UI-facing PRs (U0 foundation + later waves).

### Brand & tokens

- [ ] No new raw hex/rgb in page/component styles except documented exceptions (vendor brands, chart series until migrated)
- [ ] New colors map to an existing semantic token or add a named token in `tokens.css` + this doc
- [ ] Primary actions use indigo; destructive uses danger tokens

### Light / dark

- [ ] Toggle `light` / `dark` / `system` without FOUC regressions
- [ ] Surfaces, borders, text readable in both themes
- [ ] Soft status fills remain distinct on dark cards
- [ ] Charts axes/labels follow theme text color

### Typography & density

- [ ] Page title / subtitle / body hierarchy matches scale
- [ ] Tables remain scannable; no clipped labels at default density
- [ ] Mono used only for technical identifiers

### Spacing / radius / elevation

- [ ] Card and modal radii match ladder (sm/md/lg)
- [ ] Consistent 8/12/16 gaps in toolbars and filter rows
- [ ] Shadows do not double-stack awkwardly in dark mode

### Components

- [ ] Buttons: primary / soft / danger / ghost states + disabled
- [ ] Forms: focus ring visible (keyboard)
- [ ] Badges/alerts: success/warning/danger/info variants
- [ ] Tables: hover + selected states
- [ ] Empty / loading / error states present for data views
- [ ] Modals: header/body/footer spacing, overlay contrast

### Motion & a11y (baseline)

- [ ] Essential transitions ≤ 300ms for routine chrome
- [ ] Interactive targets ≥ ~32px where feasible
- [ ] Status not conveyed by color alone when critical (icon/text)

### Regression smoke

- [ ] Login surface
- [ ] Dashboard KPI + one chart
- [ ] Sites / Accounts table
- [ ] Token routes dense view
- [ ] Settings form
- [ ] Mobile drawer / filter sheet (narrow viewport)

### Build

- [ ] Frontend build succeeds after token CSS changes (`npm run build` in `web/` when node_modules present)

---

## 13. Out of scope (U0)

- Rewriting page business logic or React components
- Migrating chart `COLOR_PALETTE` arrays (document only)
- Backend / API / package.json stack bumps (owned by S1 / other issues)
- Full a11y audit (U3)

---

## 14. References

- Implementation tokens: `web/styles/tokens.css`
- Consumer stylesheet: `web/index.css`
- Milestone: M-UI (issues #12–#15)
- Inventory snapshot (U0 research): primary tokens previously inline in `web/index.css` `:root`
