# Design system components

> Last updated: 2026-07-19
> Issue: [#533](https://github.com/TokenDanceLab/metapi-go/issues/533)
> Source of truth for primitives: `web/design-system/**`
> Tokens: `web/styles/tokens.css` + `docs/design/DESIGN.md`

## Purpose

Unified React primitives for the UI-REFRESH wave. Prefer these over ad-hoc Tailwind / inline styles for new surfaces. Class prefix is always `ds-`; colors and elevation come from CSS variables (`var(--color-*)`, glass tokens).

## Entry points

| Path | Role |
|------|------|
| `web/design-system/index.ts` | Public exports |
| `web/design-system/styles.css` | Primitive styles (imported by index) |
| `web/pages/DesignSystemGallery.tsx` | Visual acceptance page |
| `/__design__` | Dev gallery route (see Access) |

## Access (gallery)

Route: `/__design__`

Guard (in `web/App.tsx`):

- `import.meta.env.DEV`, **or**
- `localStorage.metapi_design_gallery === '1'`

When enabled, the gallery renders **outside the auth shell** so Vite preview / Playwright can screenshot without a backend token. When the guard fails, the path is not registered and falls through to the normal app catch-all.

Marker: `data-testid="design-system-gallery"`.

Theme toggles on the gallery set `document.documentElement[data-theme]` to `light` or `dark`.

## Tokens used

Primitives consume existing semantic tokens. Glass material tokens added for #533:

| Token | Purpose |
|-------|---------|
| `--glass-bg` | Translucent panel fill |
| `--glass-bg-strong` | Stronger glass fill |
| `--glass-border` | Glass edge |
| `--glass-blur` | Backdrop blur radius |
| `--glass-saturate` | Backdrop saturation |
| `--shadow-glass` | Soft glass elevation |

Also: `--color-primary*`, `--color-bg*`, `--color-text*`, `--color-border*`, status soft fills (`--color-*-soft`), spacing (`--space-*`), radius, motion.

## Inventory

### Button

```tsx
import { Button } from '../design-system/index.js';

<Button variant="primary" size="md">Save</Button>
```

| Prop | Values | Default |
|------|--------|---------|
| `variant` | `primary` \| `secondary` \| `ghost` \| `danger` | `primary` |
| `size` | `sm` \| `md` | `md` |

Classes: `ds-btn`, `ds-btn--{variant}`, `ds-btn--{size}`. Native button attrs forwarded (`type` defaults to `button`).

### Surface

```tsx
<Surface variant="glass" padding="md">…</Surface>
```

| Prop | Values | Default |
|------|--------|---------|
| `variant` | `solid` \| `glass` \| `sunken` | `solid` |
| `padding` | `none` \| `sm` \| `md` \| `lg` | `md` |
| `as` | semantic HTML tag | `div` |

Glass respects `prefers-reduced-transparency` and missing `backdrop-filter` by falling back to elevated solid.

### Card

```tsx
<Card title="Title" description="…" footer={…} glass>
  Body
</Card>
```

| Prop | Notes |
|------|-------|
| `title` / `description` / `footer` | Optional slots |
| `glass` | boolean; uses glass material |

Classes: `ds-card`, optional `ds-card--glass`, slots `ds-card__header|title|description|body|footer`.

### Badge

```tsx
<Badge tone="success">ok</Badge>
```

| Prop | Values | Default |
|------|--------|---------|
| `tone` | `success` \| `warn` \| `danger` \| `info` \| `neutral` | `neutral` |

Classes: `ds-badge`, `ds-badge--{tone}`.

### Input

```tsx
<Input label="Name" hint="…" error="…" />
```

| Prop | Notes |
|------|-------|
| `label` | Associated via `htmlFor` / `id` |
| `hint` / `error` | Mutual description; error sets `aria-invalid` |
| `inputClassName` | Extra class on the `<input>` |

Classes: `ds-input-field`, `ds-input-label`, `ds-input`, `ds-input-hint`.

### Stack

Vertical flex layout.

| Prop | Values | Default |
|------|--------|---------|
| `gap` | `0`–`6`, `8` (space scale) | `3` |
| `align` | `start` \| `center` \| `end` \| `stretch` \| `baseline` | `stretch` |
| `justify` | `start` \| `center` \| `end` \| `between` \| `around` | `start` |

Classes: `ds-stack`, `ds-gap-*`, `ds-align-*`, `ds-justify-*`.

### Inline

Horizontal flex layout (wraps by default).

| Prop | Values | Default |
|------|--------|---------|
| `gap` | same as Stack | `2` |
| `align` | same as Stack | `center` |
| `justify` | same as Stack | `start` |
| `wrap` | boolean | `true` |

Classes: `ds-inline`, plus gap/align/justify and `ds-wrap` / `ds-nowrap`.

## Conventions

1. **Prefix**: only `ds-` for design-system classes.
2. **Tokens**: no hard-coded brand hex in components; use `var(--color-*)` / glass vars.
3. **Accessibility**: focus-visible rings, label associations, `aria-invalid` on errors, reduced-transparency fallbacks.
4. **Imports**: from `web/design-system/index.js` (or package-relative path with `.js` extension for NodeNext).
5. **Migration**: existing `.btn` / page CSS stays until page-level refresh issues land; new UI should start here.

## Visual acceptance

1. Run Vite dev server.
2. Open `/__design__` (DEV) or set `localStorage.metapi_design_gallery = '1'`.
3. Toggle Light / Dark; confirm Button, Surface, Card, Badge, Input, Stack, Inline variants.

## Tests

- `web/design-system/Button.test.tsx` — class composition + disabled click.
- Typecheck: `cd web && npm run typecheck:web`
- Unit: `cd web && npm test -- --run`
