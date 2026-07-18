# UI/UX refresh — GCP × frosted glass × Apple detail

**Date**: 2026-07-19  
**Status**: Phase 1 foundation **in tree** (unreleased) — Milestone [51 UI-REFRESH](https://github.com/TokenDanceLab/metapi-go/milestone/51); issues #532–#536  
**Product**: MetAPI admin (`web/`)  
**Supersedes direction of**: closed U0–U2 token polish (#12/#14 等) — **new visual language**, keep token-first architecture  
**Related**: [`../design/DESIGN.md`](../design/DESIGN.md) · [`../design/a11y-checklist.md`](../design/a11y-checklist.md) · `web/styles/tokens.css` · [`formal-readiness.md`](./formal-readiness.md)

---

## 1. Why

### 1.1 已知痛点（产品侧已见 + 社区诟病模式）

| Pain | Mechanism today | User feel |
|:-----|:----------------|:----------|
| **夜间白色闪光弹** | FOUC：`index.html` 只读 legacy `localStorage.theme`；CSS/字体未到前默认浅色底；`theme_mode=system` 时与 React hydrate 时序竞态 | 暗色用户进页先闪白 |
| **双主题不彻底** | token 已有，但 `index.css`/部分组件仍混 hex / 浅色假设 | 暗色“补丁感” |
| **偏旧后台审美** | 纯平 indigo + 硬边卡片 +  dense 灰底 | 不像现代云控台 |
| **玻璃未体系化** | topbar 有 ad-hoc glass rgba，无 token | 亮/暗玻璃不一致 |
| **信息密度 vs 呼吸感** | ops 密度正确，但层级/阴影/圆角缺少苹果式细节 | “能用不好看” |

### 1.2 目标体验（一句话）

> **GCP 云控台的信息架构 + 白色/浅色磨砂玻璃材质 + 苹果级间距/圆角/动效克制**；暗色是一等公民，首屏零闪光弹。

非目标：

- 不做消费级营销落地页风  
- 不引入重型 UI kit 重写（优先 token + 壳层 + 关键组件）  
- 不借 UI 波偷偷做 WS-1 / sticky 等产品 residual  

---

## 2. Design principles

1. **Dark-first FOUC** — 在任何 CSS 前用 inline script + `color-scheme` 定底色；禁止白屏闪。  
2. **Token-only surfaces** — 新视觉只进 `tokens.css` / semantic 名；页面禁止新 hex。  
3. **One glass system** — `--glass-bg` / `--glass-border` / `--glass-blur` / `--glass-shadow` 亮暗双表。  
4. **Calm brand** — 从“饱和 indigo 营销渐变”收到 **GCP 蓝灰**：primary 更接近 Google Blue / Cloud console，少霓虹。  
5. **Apple detail, not Apple clone** — 大圆角、细分割线、连续阴影、弹簧感短动效；保留 ops 表密度。  
6. **Progressive disclosure** — 壳层 → 登录/Dashboard → 表格页 → 表单/抽屉；可中断可回滚。  
7. **a11y non-negotiable** — 对比度、focus ring、reduced-motion；磨砂不减可读。  

---

## 3. Visual direction

### 3.1 References（气质，不抄像素）

| Source | Take |
|:-------|:-----|
| **Google Cloud Console** | 顶栏+侧栏 IA、蓝主色、表+滤镜、冷静状态色 |
| **Apple Settings / VisionOS glass** | 材质、模糊、细边、层级阴影 |
| **Linear / Vercel dashboard** | 现代细节、克制动效（次要参考） |

### 3.2 Light theme（主推“白磨砂”）

| Role | Direction |
|:-----|:----------|
| Canvas | 极浅冷灰 `#f4f6f8` → 略带蓝，不是纯 `#f5f5f5` 暖脏灰 |
| Card | 半透明白 `rgba(255,255,255,0.72–0.86)` + `backdrop-filter` |
| Sidebar / Topbar | 更强玻璃；底部分割用 hairline，不靠重阴影堆叠 |
| Primary | GCP-ish blue（例 `#1a73e8` 系），hover 略深 |
| Accent | 克制青灰，少双色大渐变 logo |
| Radius | 控制面 10–14px；按钮 8–10px；芯片 999 |
| Shadow | 双层软阴影（ambient + key），暗色改用 emissive border |

### 3.3 Dark theme（一等公民）

| Role | Direction |
|:-----|:----------|
| Canvas | 深空灰蓝 `#0b0f14` / `#0f141a`（避免纯黑死板） |
| Card | `rgba(22,27,34,0.72)` + blur；**禁止**落到未 token 的纯白 |
| Text | 主字近 `#e8eaed`；次级降低 alpha，不靠纯 `#666` |
| Glass | 暗玻璃边 `rgba(255,255,255,0.08)`；blur 略减以免糊 |
| 状态色 | 保持语义；soft fill 用低 alpha，不出现“浅色徽章贴在深底上刺眼” |

### 3.4 材质 token（新增家族，名称可微调）

```text
--glass-bg
--glass-bg-strong
--glass-border
--glass-blur          /* e.g. 16px / 20px */
--glass-saturate      /* e.g. 1.2 */
--shadow-glass
--shadow-elevated
--radius-control
--radius-card
--radius-shell
--motion-swift / --motion-soft
```

`prefers-reduced-transparency: reduce` 时：玻璃退化为实色 surface（可访问性）。

---

## 4. FOUC / 夜间闪光弹 — 硬修复规格

### 4.1 Root cause（代码事实）

- `web/index.html` FOUC 只读 **`localStorage.theme`**  
- App 权威是 **`theme_mode`** = `system | light | dark`（system 时会 **remove** legacy `theme`）  
- `<body>` / `<html>` 在 CSS 到达前无背景 → 浏览器默认白  
- Google Fonts 外链增加首屏延迟  

### 4.2 AC（必须测）

| ID | Acceptance |
|:---|:-----------|
| FOUC-1 | `theme_mode=dark` 硬刷新：首屏 **无** 可见白闪（录像/人工） |
| FOUC-2 | `theme_mode=system` + OS dark：同上 |
| FOUC-3 | `theme_mode=light`：无黑闪 |
| FOUC-4 | FOUC script 与 React hydrate 最终 `data-theme` 一致 |
| FOUC-5 | 慢 3G / 禁用缓存下仍成立 |
| FOUC-6 | `color-scheme` 与 `data-theme` 同步，表单控件不反色闪 |

### 4.3 Implementation sketch（落地时）

1. Inline **blocking** script in `<head>`（越早越好）：  
   - 读 `theme_mode` 优先，其次 legacy `theme`，再次 `prefers-color-scheme`  
   - 设 `data-theme` + `style.colorScheme` + `html,body{background:...}` 内联关键色  
2. 可选：`<meta name="color-scheme" content="light dark">`  
3. 字体：系统栈优先；Inter 改为 `font-display: optional` 或自托管 subset，避免白屏等字体  
4. 测试：Playwright 暗色硬刷新截首帧；或 `document.documentElement` 在 CSS 前断言  

---

## 5. Information architecture（少动结构）

保持现有导航信息架构（Dashboard / Sites / Accounts / Tokens / Routes / Logs / Settings…）。  
刷新的是 **chrome + density + component chrome**，不是信息架构大搬迁。

| Layer | Change |
|:------|:-------|
| Shell | Topbar/Sidebar 玻璃化、更细 active 指示、折叠动效 |
| Page header | 标题+描述+主操作的苹果式工具条 |
| Tables | 表头 sticky 半透明；行 hover 更轻；状态 pill |
| Forms | 更大点击目标、更清晰校验、分段卡片 |
| Login | 独立沉浸玻璃卡；与壳层同一 token |
| Empty/Error | 插画克制 + 单一主操作 |

---

## 6. Phased delivery（建议 Milestone **UI-REFRESH**）

### Phase 0 — Spec freeze（0.5–1d）

- [x] 本文 + formal-readiness 定位  
- [x] 开 Milestone + Issue 史诗（#532–#536）  
- [x] 冻结：token 命名表、FOUC AC、非目标  

### Phase 1 — Foundation（P0）

| Work | AC | Status |
|:-----|:---|:------:|
| FOUC 修复 | FOUC-1…6 | **done** (`themeBootstrap` + head script + unit/e2e) |
| tokens 视觉重映射 + glass 家族 | 亮/暗对照表进 DESIGN.md | **partial** — glass family + FOUC canvas colors in `tokens.css`; DESIGN.md full rewrite residual |
| `color-scheme` / reduced-transparency | checklist | **partial** — color-scheme wired; reduced-transparency residual |
| 去首屏阻塞字体风险 | 系统栈或 optional | residual |
| Design system + Vite gallery | primitives + `/__design__` | **done** scaffold (#533) |
| Visual + UX e2e harness | Playwright list green | **done** harness (#534/#536); baselines not committed |

**Exit (Phase 1 code)**: dark hard-refresh zero-flash path in tree; gallery + e2e harness present. **Ship residual**: gallery snapshot commit, DESIGN.md polish, close issues on release.

### Phase 2 — Shell（P0）

| Work | AC | Status |
|:-----|:---|:------:|
| Topbar / Sidebar / Main canvas | 玻璃 + 新圆角阴影 | **done** CSS (topbar/sidebar glass tokens) |
| Theme 菜单 / 用户菜单 | elev glass popover | **done** user-dropdown glass |
| Mobile drawer | 同材质；无双滚动陷阱 | **done** drawer panel glass |
| Login surface | 与 shell 一致 | **done** login-surface/auth-panel glass |

**Exit residual**: 登录后 3 个主页（Dashboard / Sites / Settings）亮暗截图人工过审；Linux visual baselines。

### Phase 3 — Data surfaces（P1）

| Work | AC |
|:-----|:---|
| 通用 Table / Filter bar / Pagination | token 化 |
| KPI cards | 轻玻璃或实色二选一（性能：表多页可实色） |
| Status badges / alerts / toasts | 语义 soft 全暗可用 |
| Charts 轴色 | 已有 theme hook；校验新底色对比度 |

### Phase 4 — Forms & density（P1）

| Work | AC |
|:-----|:---|
| Drawer / Modal / 表单控件 | focus、错误态、disabled |
| 设置页分段 | 苹果式 group list 可选 |
| 密度：默认 ops-comfortable；不先做 compact 模式 |

### Phase 5 — Polish & a11y（P1）

| Work | AC |
|:-----|:---|
| reduced-motion | 非必要动画关闭 |
| 对比度抽检 | a11y-checklist 双主题 |
| 残留 hex 清扫 | hygiene / lint 可循序 |
| 文档 | DESIGN.md 全面改写视觉章；README 不吹未做动效 |

---

## 7. Engineering rules

1. **先 token 后组件**；禁止在 `pages/*` 写死新品牌色。  
2. **backdrop-filter** 仅 shell / modal / dropdown；大表格行不要 blur（性能）。  
3. 低端/无 GPU：`@supports` 回退实色。  
4. 不新增 React UI 框架；继续现有栈（React 19 + Vite + Tailwind v4 + 自研 class）。  
5. 每个 Phase 可独立发版（建议夹在 patch：如 `v0.8.45` FOUC+token → `v0.9.0` 若视觉破坏性大再用 minor）。  
6. 视觉 PR 必须附 **light/dark 截图**（登录 + shell + 一表一页）。  
7. 中文文案与 i18n key 不借机重写业务文案，除非 UI 结构变化。  

### 7.1 建议 semver 策略

| 变更 | 版本 |
|:-----|:-----|
| 仅 FOUC + token 兼容映射 | patch `0.8.x` |
| Shell 玻璃化但 class 名兼容 | patch 或 minor |
| 大规模 class/DOM 结构变化 | **minor `0.9.0`** + CHANGELOG 迁移说明 |

---

## 8. Issue breakdown（史诗下的子项草案）

| Issue title (draft) | Phase | Priority |
|:--------------------|:-----:|:--------:|
| `[ui] FOUC: theme_mode-first bootstrap + no white flash` | 1 | P0 |
| `[ui] tokens: GCP blue-gray + glass material family` | 1 | P0 |
| `[ui] shell: topbar/sidebar frosted glass` | 2 | P0 |
| `[ui] login: glass card parity with shell` | 2 | P0 |
| `[ui] tables/filters on new surfaces` | 3 | P1 |
| `[ui] badges/alerts/toasts semantic dark-safe` | 3 | P1 |
| `[ui] forms/drawers Apple-detail controls` | 4 | P1 |
| `[ui] a11y + reduced-transparency/motion pass` | 5 | P1 |
| `[docs] DESIGN.md refresh for UI-REFRESH language` | 1–5 | P1 |

---

## 9. Out of scope（本波明确不做）

- resin / Redis / 出口池  
- WS-1 / STICKY-B / UC-1  
- 计费 P0-555 产品逻辑  
- 移动端原生 App  
- 用户自定义主题色编辑器（可未来）  

---

## 10. Success metrics

| Metric | Target |
|:-------|:-------|
| 暗色首屏白闪 | **0** 可感知闪（FOUC AC） |
| 新 hex 进入 pages | **0**（token only） |
| 亮/暗关键路径截图 | 登录/壳/表/设置 全有 |
| Lighthouse a11y（抽查） | 不回退；focus 可见 |
| 大表滚动 | 60fps 可接受；行无 blur |
| 运维可读性 | 状态色 3m 内可辨，不靠“好看但看不清” |

---

## 11. Decision log

| Date | Decision |
|:-----|:---------|
| 2026-07-19 | 启动 UI-REFRESH 设计；方向 = GCP IA + 白磨砂玻璃 + 苹果细节 |
| 2026-07-19 | FOUC/夜间闪光弹列为 Phase 1 **P0**，先于审美大改 |
| 2026-07-19 | 与 Track B 功能 residual **分轨**；UI 波不宣称企业完备 |
| 2026-07-19 | 默认不引入新 UI 框架；token-first 延续 |

---

## 12. GitHub board

| Item | Link |
|:-----|:-----|
| Milestone | [UI-REFRESH #51](https://github.com/TokenDanceLab/metapi-go/milestone/51) |
| Epic | [#532](https://github.com/TokenDanceLab/metapi-go/issues/532) |
| FOUC | [#535](https://github.com/TokenDanceLab/metapi-go/issues/535) |
| Design system + gallery | [#533](https://github.com/TokenDanceLab/metapi-go/issues/533) |
| Visual acceptance | [#534](https://github.com/TokenDanceLab/metapi-go/issues/534) |
| UX e2e | [#536](https://github.com/TokenDanceLab/metapi-go/issues/536) |

## 13. Next action

1. ~~开 Milestone + Issues~~ **done** M51 · #532–#536  
2. ~~Phase 1 FOUC + DS + harness~~ **in tree** (commit next)  
3. Commit/push Phase 1; comment issues with evidence; close #535 when FOUC e2e green on CI  
4. Generate + commit gallery baselines (`npm run test:visual:update` on Linux) → close #534 fragment  
5. Phase 2 shell glass PR (topbar/sidebar/login) with light/dark screenshots
