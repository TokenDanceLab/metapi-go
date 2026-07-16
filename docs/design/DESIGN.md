# MetAPI Admin — Design System (DESIGN.md)

**Status**: Foundation draft (Lane **UI** / Issue **#12 U0** owns completion)  
**Last updated**: 2026-07-16  
**Consumers**: `web/styles/tokens.css`, components, pages  
**Non-goals**: Marketing site; Electron chrome

> 本文件是前端视觉与交互的 **唯一 SSOT**。页面不得引入未登记的色值/字号/间距。  
> 实现顺序：U0 tokens → U1 components → U2 pages → U3 a11y。

---

## 1. Product design intent

| Pillar | Meaning for MetAPI admin |
|:-------|:-------------------------|
| **Ops-dense** | 高信息密度，表/筛选/批处理优先，装饰让路 |
| **Trustworthy** | 危险操作明确；状态色语义稳定；无静默失败 UI |
| **Modern enterprise** | 清晰层级、一致控件、光暗可切换、键盘可达 |
| **Calm chrome** | 侧栏/顶栏安静；内容区对比足够 |

Visual metaphor: **control room** — not consumer social app.

---

## 2. Color tokens

### 2.1 Neutrals (light)

| Token | Role | Suggested value (draft) |
|:------|:-----|:------------------------|
| `--color-bg` | App background | light `#f4f6fa` |
| `--color-surface` | Card / panel | light `#ffffff` |
| `--color-surface-2` | Nested panel | light `#f0f3f8` |
| `--color-border` | Default border | `#d8dee8` |
| `--color-border-strong` | Emphasis | `#b4bfcf` |
| `--color-text` | Primary text | `#0f172a` |
| `--color-text-muted` | Secondary | `#64748b` |
| `--color-text-inverse` | On accent | `#ffffff` |

### 2.2 Semantic

| Token | Role |
|:------|:-----|
| `--color-accent` | Primary actions / links |
| `--color-accent-hover` | Hover |
| `--color-danger` | Delete / fatal |
| `--color-warn` | Degraded / cooldown |
| `--color-success` | Healthy / enabled |
| `--color-info` | Neutral info |

**U0 must pick concrete hex/oklch values** from current `index.css` + brand, and document light/dark pairs.

### 2.3 Rules

1. No raw hex in components after U1 — only `var(--token)`.  
2. Charts (vchart) map series colors to semantic/accent scale.  
3. Status badges: success/warn/danger/info only — no ad-hoc pinks.

---

## 3. Typography

| Token | Size / weight | Use |
|:------|:--------------|:----|
| `--font-sans` | system-ui stack | UI |
| `--font-mono` | ui-monospace | keys, ids, logs |
| `--text-xs` | 12 | meta |
| `--text-sm` | 13–14 | table body |
| `--text-md` | 14–15 | body |
| `--text-lg` | 16–18 | section title |
| `--text-xl` | 20–24 | page title |
| `--font-weight-medium` | 500 | labels |
| `--font-weight-semibold` | 600 | titles |

Line-height: dense tables `1.35`, prose `1.5`.

---

## 4. Spacing, radius, elevation

| Token | Value | Use |
|:------|:------|:----|
| `--space-1` … `--space-8` | 4px grid | padding/gap |
| `--radius-sm` | 6 | chips |
| `--radius-md` | 10 | buttons/inputs |
| `--radius-lg` | 14 | cards |
| `--shadow-sm` | soft | dropdown |
| `--shadow-md` | medium | modal |

Density mode: **comfortable** default; optional compact later (not this wave).

---

## 5. Motion

| Token | Value | Use |
|:------|:------|:----|
| `--ease-standard` | cubic-bezier(0.2, 0.8, 0.2, 1) | most |
| `--duration-fast` | 120ms | hover |
| `--duration-normal` | 200ms | panels |

Respect `prefers-reduced-motion: reduce`.

---

## 6. Component inventory (to complete in U0/U1)

| Component area | Path hint | Token status |
|:---------------|:----------|:-------------|
| App shell / sidebar / topbar | `web/App.tsx` | pending |
| Buttons / icon buttons | components | pending |
| Forms / inputs / selects | components | pending |
| Tables / filters / batch bar | components | pending |
| Modals / drawers | components | pending |
| Toasts / tooltips | components | pending |
| Charts | Dashboard vchart | pending |
| Empty / error / loading | pages | pending |

U0 fills paths from repo inventory; U1 restyles.

---

## 7. Layout

- Sidebar: fixed width desktop; drawer mobile (existing tests must stay green).  
- Content max width: fluid for tables.  
- Page header: title + primary actions right-aligned.  
- Filters: one consistent panel pattern.

---

## 8. States

Every interactive control documents: default / hover / focus-visible / active / disabled / loading / error.  
Focus ring: `--color-accent` 2px offset — never remove outline without replacement.

---

## 9. Accessibility (U3 expands)

- Icon-only controls need `aria-label`.  
- Contrast: text/muted on surface ≥ WCAG AA where feasible.  
- Keyboard: modal focus trap, Esc close.  
- Checklist file: `docs/design/a11y-checklist.md` (U3).

---

## 10. Implementation mapping

| Spec section | Code |
|:-------------|:-----|
| §2–5 tokens | `web/styles/tokens.css` |
| Import | `web/index.css` → `@import "./styles/tokens.css"` |
| Tailwind | Prefer CSS variables in `@theme` / arbitrary values binding to tokens |

---

## 11. Visual review checklist (gate)

- [ ] No untokenized hex in new/changed components (U1+)  
- [ ] Primary button contrast OK  
- [ ] Table row hover readable  
- [ ] Modal/drawer same radius/shadow language  
- [ ] Dark/light (if both) pair documented  
- [ ] Mobile 375 sidebar behavior intact  
- [ ] Dashboard charts not neon-clash with chrome  

---

## 12. Ownership

| Role | Agent / Issue |
|:-----|:--------------|
| Author & SSOT | **lane-ui** / #12 U0 |
| Component apply | #13 U1 |
| Page apply | #14 U2 |
| A11y sign-off | #15 U3 |
| Dependency bumps | **lane-stack** only |

---

## 13. Changelog

| Date | Change |
|:-----|:-------|
| 2026-07-16 | Skeleton SSOT committed for enterprise program; U0 to finalize concrete values |
