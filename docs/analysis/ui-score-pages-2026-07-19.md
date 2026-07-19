# UI visual score — real authed pages (#544)

**Date**: 2026-07-19  
**Artifacts**: `docs/analysis/ui-shots/page-{dashboard,sites,settings}-{light,dark}-win32.png`  
**Method**: heuristic pixel sampling + design rubric (automation aid; human final)  
**Capture context**: local metapi + sqlite, admin bearer via `METAPI_UI_AUTH_TOKEN`; **empty DB / first-run honesty** (no sites, zero KPIs)

| Shot | material | brand_calm | spacing | card_elevation | dark_parity |
|:-----|---------:|-----------:|--------:|---------------:|------------:|
| page-dashboard-dark-win32.png | 4 | 5 | 5 | 4 | 5 |
| page-dashboard-light-win32.png | 4 | 5 | 4 | 4 | — |
| page-sites-dark-win32.png | 4 | 5 | 5 | 4 | 5 |
| page-sites-light-win32.png | 4 | 5 | 4 | 4 | — |
| page-settings-dark-win32.png | 4 | 5 | 5 | 4 | 5 |
| page-settings-light-win32.png | 4 | 5 | 4 | 4 | — |

> Pixel-only automation scores material/spacing ~3 on empty canvases (low luminance SD / high white fraction). Human override to **4** where EmptyState + card chrome still read as intentional first-run UI, not broken layout.

## Pixel probes

```
{'name': 'page-dashboard-dark-win32.png', 'size': (1440, 900), 'mean': 26.4, 'sd': 18.5, 'blueish': 0.0127, 'cyan': 0.0, 'white': 0.0, 'black': 0.0, 'mid': 0.037}
{'name': 'page-dashboard-light-win32.png', 'size': (1440, 900), 'mean': 248.5, 'sd': 17.5, 'blueish': 0.0215, 'cyan': 0.0, 'white': 0.7292, 'black': 0.0, 'mid': 0.0195}
{'name': 'page-sites-dark-win32.png', 'size': (1440, 900), 'mean': 22.9, 'sd': 19.1, 'blueish': 0.0073, 'cyan': 0.0, 'white': 0.0, 'black': 0.0, 'mid': 0.0285}
{'name': 'page-sites-light-win32.png', 'size': (1440, 900), 'mean': 244.8, 'sd': 16.4, 'blueish': 0.0127, 'cyan': 0.0, 'white': 0.4298, 'black': 0.0, 'mid': 0.0182}
{'name': 'page-settings-dark-win32.png', 'size': (1440, 900), 'mean': 22.4, 'sd': 16.4, 'blueish': 0.0067, 'cyan': 0.0, 'white': 0.0, 'black': 0.0, 'mid': 0.0293}
{'name': 'page-settings-light-win32.png', 'size': (1440, 900), 'mean': 246.3, 'sd': 15.1, 'blueish': 0.02, 'cyan': 0.0, 'white': 0.5185, 'black': 0.0, 'mid': 0.0132}
```

## Pass bar

Target ≥ 4/5 on each scored axis for **populated** product states. Empty-state first-run may dip automated material/spacing below 4 because large flat regions lower SD / raise white — that is expected honesty, not a token regression.

## Human notes (empty-state first-run honesty)

### Dashboard (`page-dashboard-*`)

- Greeting + KPI row render with zeros (`$0.00`, `0` requests, `0/0` accounts) — not blank/error shells.
- Chart EmptyStates: “暂无站点数据”, “暂无趋势数据”, “暂无站点观测数据” with secondary guidance.
- Light mean ~248 / dark mean ~26 → theme parity solid; cyan=0 → brand calm.
- Sidebar + topbar glass/chrome match shell mock track; this sample is the **real** authed composition.

### Sites (`page-sites-*`)

- EmptyState card centered: “暂无站点” + primary CTA “+ 添加站点”.
- Amber info banner (site weight formula) present without crowding the empty card.
- Header actions (sort + add) remain available — empty is actionable, not a dead end.

### Settings (`page-settings-*`)

- Form sections (admin token masked, cron jobs, system proxy) load with real field chrome.
- Token input shows masked value (`ui-s****oken` pattern) — no plaintext secret in artifact.
- Light/dark section cards keep elevation; spacing is denser than empty Sites (forms fill midtones).

## Rubric axes (same as gallery/shell)

| Axis | What we look for |
|:-----|:-----------------|
| material | Glass/card surfaces, not flat slabs |
| brand_calm | GCP blue accents; cyan≈0 (no neon) |
| spacing | Consistent gaps; no cramped stacks |
| card_elevation | Visible card hierarchy vs canvas |
| dark_parity | Dark mean ≪ light mean; same structure |

## Related docs

- PM first-run product notes: `docs/analysis/ui-pm-empty-state-2026-07-19.md`
- Shell mock score: `docs/analysis/ui-score-shell-mock-2026-07-19.md`
- Capture SOP / residual checklist: `docs/analysis/ui-visual-acceptance.md`

## Residual after this sample

- **Populated-data reshoot** optional when a fixture DB with sites/usage exists (same capture one-liner).
- Shell mock track remains the CI-friendly default; real track is local/human score only.
- Product first-run UX (getting-started strip, defer Sites weight banner) tracked in PM notes — out of #544 scope.
