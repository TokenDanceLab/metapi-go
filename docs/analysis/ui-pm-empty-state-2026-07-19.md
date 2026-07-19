# PM / UX notes — real empty-DB shell (2026-07-19)

**Source**: local `metapi` sqlite + `AUTH_TOKEN` page shots  
`docs/analysis/ui-shots/page-{dashboard,sites,settings}-{light,dark}-win32.png`  
**Ops pin**: v0.8.44 · **UI tip**: unreleased polish  
**Related**: M52 #544 (commit/score) · product residual below is **not** all in M52

## 1. What the first-run user actually sees

| Surface | First-run content | Feeling |
|:--------|:------------------|:--------|
| Dashboard | 5 KPI cards at **$0 / 0** · pie “暂无站点数据” · trend “暂无趋势数据” · observability empty | Calm but **sparse**; many equal-weight cards |
| Sites | EmptyState **暂无站点** + primary CTA · long amber **weight formula** banner always on | CTA clear; banner competes with empty |
| Settings | Full form density (token masked, cron, cleanup, proxy) | Most “complete” of the three; ops-native |

Shell chrome (topbar/sidebar glass, active rail, search) is **production-quality**. Empty interiors are the gap between mock gallery score and live trust.

## 2. Product problems (ranked)

### P0 — Dashboard zero-state hierarchy
- Five KPI cards with all zeros read as “system broken” more than “not configured”.
- Chart wells use different empty patterns (pie glyph vs plain text) without a single **next step**.
- **PM ask**: one **Getting started** strip above KPIs: “添加站点 → 连接账号 → 同步令牌” with deep links; collapse zero KPIs into a single “尚未产生流量” summary until first site.

### P0 — Sites amber banner vs EmptyState
- Weight formula banner is **always-on education** on an empty list → cognitive tax before first value.
- EmptyState CTA is good (`+ 添加站点` twice: header + empty) — slightly redundant but acceptable.
- **PM ask**: hide weight banner until `sites.length > 0`, or collapse to “了解站点权重” link.

### P1 — Settings first-run trust
- Masked admin token + “修改登录令牌” is clear.
- Cron defaults visible without explainers of “safe to leave” vs “must set for check-in”.
- System proxy empty field OK; ensure help text doesn’t look like an error.

### P1 — Mock vs real composition gap
- Gallery shell mock shows **dense healthy table + KPI deltas** (marketing of ops maturity).
- Real empty DB shows **zeros + empty charts**.
- Risk: internal score on mock overstates production first-run UX.
- **M52 #543** fixes mock Traffic trend placeholder; still need honest first-run story on real Dashboard (future issue, not #543).

### P2 — Nav information architecture
- Full sidebar (站点公告 / OAuth / 下游密钥 / …) on empty install is enterprise-complete but **onboarding-hostile**.
- Optional later: progressive disclosure / “setup mode” highlight only Sites + Settings.

## 3. UX patterns that already work

1. **EmptyState primitive** on Sites (icon well + title + description + primary) — matches design system #541.
2. **Token masking** on Settings admin token.
3. **Semantic badge colors** unused on empty Sites but ready on mock table (healthy/idle/53300).
4. **Dual theme** page shots: cyan≈0, brand calm holds on real chrome.

## 4. Heuristic scores (real pages, empty DB)

| Shot | material | brand_calm | spacing | notes |
|:-----|---------:|-----------:|--------:|:------|
| page-dashboard-light | 4 | 5 | 4 | high white; low structure until data |
| page-sites-light | 4 | 5 | 5 | EmptyState + banner clash |
| page-settings-light | 5 | 5 | 5 | best real density |
| dark variants | 5 | 5 | 4–5 | mean ~22–26, no cyan |

Pass bar ≥4 met; **human** still flags Dashboard zero-state and Sites banner.

## 5. Recommended backlog (beyond M52 Wave1)

| ID | Idea | Why |
|:---|:-----|:----|
| FIRST-RUN-1 | Dashboard getting-started strip | Trust on day 0 |
| FIRST-RUN-2 | Defer Sites weight banner until data | Focus CTA |
| FIRST-RUN-3 | KPI “no traffic yet” consolidated card | Less zero noise |
| MOCK-REAL-1 | Gallery note: mock ≠ first-run | Score honesty |
| NAV-1 | Setup-mode nav emphasis | Optional |

M52 Wave1 does **not** implement FIRST-RUN-* (scope control). Track as follow-ups after #544 honesty lands.

## 6. Decision log

| Decision | Rationale |
|:---------|:----------|
| Capture empty DB as official real-shot sample | Reproducible without prod `_td_auth` |
| Keep UI unreleased | Ops pin 0.8.44; no silent product release |
| Parallel M52 lanes by file ownership | Gallery vs docs vs css vs e2e |
