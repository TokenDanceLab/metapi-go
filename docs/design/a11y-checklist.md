# MetAPI Accessibility & Responsive Checklist (U3)

**Product**: TokenDance / MetAPI admin  
**Issue**: #15 (U3)  
**Related SSOT**: `docs/design/DESIGN.md`, `web/styles/tokens.css`  
**Scope**: Enterprise ops control-plane chrome + shared components  
**Last updated**: 2026-07-17  
**Status**: checklist + critical chrome fixes landed; page-level residual debt tracked below

This document is the U3 acceptance checklist. It records keyboard, name, contrast, and responsive expectations, plus residual debt that is intentionally out of scope for this issue.

---

## 1. Acceptance criteria (#15)

| AC | Status | Evidence / notes |
|----|--------|------------------|
| Keyboard focus order on primary flows | **partial / pass with debt** | Shell + shared modals: Tab reaches topbar tools, sidebar/nav, main content; Esc closes search / drawer / escape-enabled modals. Full page-form roving index not done. |
| `aria-label` on icon-only controls | **pass for chrome** | Topbar icon buttons labeled; mobile nav open/close labeled; SearchModal close labeled; sidebar collapse labeled when icon-only. Page action grids still have mixed coverage (debt). |
| Contrast notes for primary text/surfaces | **documented** | §4 below; body primary ≥ 4.5:1 both themes; muted/meta may fall near threshold. |
| Responsive checklist 375 / 768 / 1280 | **documented + shell pass** | §5; shell breakpoints via `useIsMobile` (~768) and CSS media queries; no wholesale page redesign in U3. |
| Residual a11y debt documented | **yes** | §7 |

---

## 2. Keyboard focus & interaction

### 2.1 Primary shell flow (must pass)

| Step | Expected behavior | Current |
|------|-------------------|---------|
| Login | Tab: theme tools → token field → submit → external GitHub link; Enter submits | Pass |
| Authenticated topbar | Tab order: mobile hamburger (if any) → language → search → notifications → theme → avatar | Pass |
| Search (`Ctrl/Cmd+K` or search trigger) | Focus moves to search input on open; Esc closes | Pass |
| Mobile nav | Hamburger opens drawer; Esc / backdrop / close button dismisses | Pass |
| Sidebar collapse (desktop) | Button remains focusable when collapsed (icon-only) | Pass after U3 aria fix |
| Profile modal | Esc + explicit close when enabled; cancel/save in footer | Pass (`CenteredModal`) |

### 2.2 Focus visibility

| Rule | Expectation |
|------|-------------|
| `:focus-visible` | Visible ring on interactive chrome (buttons, close controls, nav items) |
| Mouse users | Prefer `:focus-visible` over always-on `:focus` to avoid sticky outlines |
| Token | Prefer `--color-focus-ring` / primary outline (`DESIGN.md` §2.8) |
| Hit target | Icon-only controls ≥ ~36px (topbar already ~36) |

**Current**: `.modal-close-button:focus-visible` uses primary outline. Broader global focus-ring utility is residual (not all controls share one rule).

### 2.3 Keyboard traps

| Surface | Expected | Notes |
|---------|----------|-------|
| Search modal | Esc exits; Tab should stay useful within modal | No full focus trap yet → residual |
| Centered modal | Esc optional via `closeOnEscape`; close button always named | Pass for name; trap residual |
| Mobile drawer | Esc exits; role=`dialog` + `aria-modal` | Pass |
| Dropdowns (theme/user/notifications) | Click-outside closes; Esc residual | Residual |

### 2.4 Focus order anti-patterns (do not introduce)

1. Positive `tabIndex` > 0
2. Icon-only `<button>` / `<a>` without accessible name
3. Removing outline without a visible replacement ring
4. `pointer-events: none` on the only focusable control
5. Opening a modal without moving focus into it (search already focuses input)

---

## 3. Accessible names (`aria-label` / visible text)

### 3.1 Shell chrome (required)

| Control | Accessible name | Location |
|---------|-----------------|----------|
| Mobile hamburger | `打开导航` | `web/App.tsx` |
| Language toggle | bilingual explicit labels | `web/App.tsx` |
| Search trigger | `搜索 (Ctrl+K)` | `web/App.tsx` |
| Notifications | `通知` | `web/App.tsx` |
| Theme menu trigger | mode label (+ resolved system theme) | `web/App.tsx` |
| Avatar menu | display name | `web/App.tsx` |
| Sidebar item (collapsed) | item label | `web/App.tsx` |
| Sidebar collapse | `收起侧边栏` / `展开侧边栏` | `web/App.tsx` (U3) |
| Mobile drawer close | `关闭导航` (or `closeLabel`) | `MobileDrawer` |
| Modal close (×) | `关闭弹框` | `CenteredModal` |
| Search modal close | `关闭` | `SearchModal` (U3) |
| Login GitHub icon link | `GitHub` | `web/App.tsx` |
| Login theme tools group | `外观设置` | `web/App.tsx` |

### 3.2 Shared component rules

1. **Icon-only button** → required `aria-label` (or `aria-labelledby`).
2. **Icon + visible text** → name may come from text; decorative SVG `aria-hidden="true"`.
3. **Close affordances** → never rely on `×` glyph alone.
4. **Collapsed rail** → every nav glyph must keep a name (`aria-label` or tooltip + label).
5. **Dynamic state** → prefer state in the name (`展开侧边栏` vs `收起侧边栏`, unread counts may stay visual if parent is named).

### 3.3 Decorative media

| Element | Rule |
|---------|------|
| Inline SVG in labeled buttons | `aria-hidden="true"` |
| Logo mark next to product name | `alt="Metapi"` or empty alt if adjacent text duplicates |
| Status color dots | Not sole channel; pair with text/badge |

---

## 4. Contrast notes (primary text / surfaces)

Values from `web/styles/tokens.css` / `DESIGN.md`. Ratios are approximate WCAG 2.x relative luminance checks for operator review (not a lab measurement suite).

### 4.1 Light theme

| Pair | Approx ratio | WCAG AA body (4.5:1) | Notes |
|------|--------------|----------------------|-------|
| `--color-text-primary` `#1a1a1a` on `--color-bg-card` `#ffffff` | ~16.5:1 | Pass | Titles, primary values |
| `--color-text-primary` on `--color-bg` `#f5f5f5` | ~15.0:1 | Pass | Page canvas |
| `--color-text-secondary` `#666666` on card | ~5.7:1 | Pass | Labels / nav |
| `--color-text-tertiary` `#8a8a8a` on card | ~3.5:1 | Fail body / pass large | Helper only |
| `--color-text-muted` `#999999` on card | ~2.8:1 | Fail body | Placeholders, meta — not body copy |
| Primary button text inverse on `--color-primary` `#4f46e5` | ~5.0:1+ | Pass | CTA |
| Danger solid `#dc2626` on white (text) | ~4.6:1 | Borderline pass | Prefer solid on soft fills for badges |

### 4.2 Dark theme

| Pair | Approx ratio | AA body | Notes |
|------|--------------|---------|-------|
| `--color-text-primary` `#f0f0f0` on `--color-bg-card` `#1a1a1a` | ~15.0:1 | Pass | |
| `--color-text-secondary` `#a0a0a0` on card | ~7.0:1 | Pass | |
| `--color-text-tertiary` `#7a7a7a` on card | ~4.0:1 | Borderline / fail small | Avoid as sole body text |
| `--color-text-muted` `#666666` on card | ~3.0:1 | Fail body | Meta only |
| Primary `#6366f1` on dark card (as text) | ~4.5:1± | Verify in UI | Prefer primary for actions/links, not long body |

### 4.3 Contrast rules for implementers

1. Body copy and table primary cells → `--color-text-primary` or `--color-text-secondary` only.
2. Never place `--color-text-muted` on large reading blocks.
3. Soft semantic fills (`*-soft`) pair with solid semantic text, not muted gray.
4. Chart axis labels use theme-aware hook colors (`useThemeLabelColor`) — verify both themes.
5. Focus rings must remain visible on both themes (`--color-focus-ring`).

---

## 5. Responsive checklist (375 / 768 / 1280)

Breakpoints used by product:

- **Mobile shell**: `useIsMobile` and layout CSS around **768px** (`data-layout="mobile|desktop"`).
- **Dense ops desktop**: ≥1280 typical laptop/monitor admin width.
- **375**: iPhone-class width; must not require horizontal page scroll for shell.

### 5.1 375px (mobile)

| Check | Expected | Status |
|-------|----------|--------|
| Topbar | Hamburger + logo + compact tools; search may iconify | Pass (shell) |
| Sidebar | Hidden; content via `MobileDrawer` | Pass |
| Main padding | Reduced; no clipped primary CTA | Pass / page debt |
| Tables | Card/list alternative or horizontal scroll inside table region only | Partial (page-dependent) |
| Batch actions | `MobileBatchBar` / responsive batch bar | Partial |
| Filters | `ResponsiveFilterPanel` → bottom/side sheet | Partial |
| Touch targets | ≥36–44px for chrome icons | Pass topbar |
| Safe areas | Avoid fixed bars covering primary content | Residual on some pages |

### 5.2 768px (tablet / breakpoint edge)

| Check | Expected | Status |
|-------|----------|--------|
| Layout switch | Mobile drawer path active at ≤768 | Pass |
| Topbar density | Tools remain usable without overlap | Pass |
| Modals | Max-width constrained; close control reachable | Pass shared modal |
| Two-column forms | Stack via `ResponsiveFormGrid` where used | Partial adoption |

### 5.3 1280px (desktop)

| Check | Expected | Status |
|-------|----------|--------|
| Sidebar | Expanded 220px default; collapsible to 64px | Pass |
| Collapsed rail | Icon-only items named | Pass |
| Tables | Full columns; sticky header optional | Page debt |
| Topbar nav + search | Visible labels where designed | Pass |
| KPI + charts | No overflow of card grid | Partial |

### 5.4 Responsive anti-patterns

1. Hiding critical actions with `display: none` and no mobile equivalent.
2. Desktop-only hover menus without a tap path.
3. Fixed pixel widths that force document-level horizontal scroll at 375.
4. Relying on tooltip-only labels when the rail collapses.

---

## 6. Reduced motion & semantics (U3 alignment)

| Topic | Expectation | Status |
|-------|-------------|--------|
| `prefers-reduced-motion: reduce` | Collapse non-essential transitions/animations | Residual (called out in DESIGN.md §7) |
| Dialog semantics | `role="dialog"` + `aria-modal` for blocking overlays | Mobile drawer pass; SearchModal improved in U3; not all legacy overlays |
| Live regions | Toasts/errors announced | Residual |
| Language | `t()` for user-visible chrome strings; `aria-label` included in i18n attr list | Pass pattern |

---

## 7. Residual a11y debt (explicit non-goals for this PR)

Tracked for follow-up issues (not blocking U3 checklist doc):

1. **Focus trap** inside SearchModal / CenteredModal / notification panel (Tab cycles within overlay).
2. **Global `:focus-visible`** utility applied to all `.btn`, `.sidebar-item`, topbar controls.
3. **`prefers-reduced-motion`** hard cutover for `fade-in` / `slide-up` / drawer transitions.
4. **Page-level icon actions** (copy, open external, row kebab, route drag handles) — many labeled, inventory incomplete across Accounts/Sites/Routes/Logs.
5. **Muted/tertiary text** used as primary content in a few dense tables — content audit.
6. **Notification panel** keyboard model (arrow keys, Esc, focus return to bell).
7. **ModernSelect** listbox semantics (`role="listbox"/"option"`, typeahead, aria-controls).
8. **Charts**: non-color status encoding for availability buckets; keyboard access to series.
9. **Skip link** to `#main-content` for keyboard users.
10. **i18n entries** for newer chrome strings (e.g. `展开侧边栏`) if EN surface shows Chinese fallback.
11. **Automated axe/playwright a11y CI** gate — not wired.
12. **Full 375 walkthrough** of every page table → card path (U2 density work residual).

---

## 8. Manual test script (release smoke)

Run against both light and dark themes.

### 8.1 Keyboard

1. Login with keyboard only; confirm error text is readable.
2. Tab through topbar; open search; type query; Esc closes; focus returns to trigger (ideal) or remains usable.
3. Open notifications; Tab within panel; click-outside/Esc dismiss.
4. Toggle theme menu; select Dark/Light/System.
5. Desktop: collapse sidebar; Tab to icon rail; confirm names via screen reader or accessibility inspector.
6. Mobile width 375: open nav drawer; Esc closes; navigate to Sites.

### 8.2 Names

1. Accessibility tree: no unlabeled buttons in topbar/sidebar/search header.
2. Modal × announces close.
3. Search close announces close.

### 8.3 Contrast

1. Dashboard KPI labels and table body in both themes.
2. Danger/success badges on soft fills remain readable.
3. Placeholder text is allowed to be muted; form labels must not be.

### 8.4 Responsive

1. **375**: login, dashboard, one table page (Accounts or Sites), search modal.
2. **768**: drawer vs sidebar switch; no overlapping topbar controls.
3. **1280**: expanded sidebar, full tables, modals centered with margin.

---

## 9. Code touchpoints for this issue

| Change | File | Why |
|--------|------|-----|
| Checklist SSOT | `docs/design/a11y-checklist.md` | U3 deliverable |
| Search modal close + dialog semantics | `web/components/SearchModal.tsx` | Icon/header lacked explicit close control name |
| Search modal test | `web/components/search-modal.results.test.tsx` | Guard close `aria-label` |
| Sidebar collapse name | `web/App.tsx` | Icon-only when collapsed |

No package bumps. No Go changes. No wholesale page redesign.

---

## 10. Definition of done (U3)

- [x] Checklist committed under `docs/design/`
- [x] Critical shared/chrome icon-only gaps fixed (search close, sidebar collapse)
- [x] Topbar existing labels verified (no regression)
- [x] Residual debt listed for follow-up
- [ ] Full page inventory + focus trap + reduced-motion CSS (future issues)
