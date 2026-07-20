# cloud-ops UI 对齐

**Date**: 2026-07-21  
**Source**: `TokenDance/tokendance-design/styles/cloud-ops/`  
**Runtime SSOT**: `web/styles/tokens.css` + `web/index.css` + FOUC (`web/index.html` / `web/themeBootstrap.ts`)

## 目标

MetAPI 管理台采用 **GCP Console 运维台（cloud-ops）** 气质：侧栏 + 密表 + KPI 卡 + 状态 chip，少装饰、系统字体、中文优先。

## 已对齐

| 项 | cloud-ops | metapi-go |
|:---|:----------|:----------|
| Light canvas | `#f8f9fa` | `--color-bg` + FOUC |
| Dark canvas | `#202124` | `--color-bg` + FOUC |
| Primary | `#1a73e8` / dark `#8ab4f8` | `--color-primary` |
| Semantic | success `#1e8e3e` · warn `#f9ab00` · danger `#d93025` | 同 |
| Radius | sm 4 / md 8 / lg 12 | `--radius-*` |
| Shell | topbar 48 · sidebar 232 | `--topbar-height` · `--sidebar-width` |
| Elevation | Material e-1 浅阴影 | `--shadow-card` |
| Chrome | 实色侧栏 + 轻 blur 顶栏 | 去掉重 glass |
| Chip | 11px · pad 1×8 · full radius | `.badge` |
| Table | pad 10×12 · th medium text-2 | `.data-table` |
| Page title | 22 / weight 400 | `.page-title` |
| Density | `html[data-density=compact]` | tokens 可选 |

## 非目标

- 不照搬 workbench（Agent 协作 / 聊天）
- 不引入外部字体 CDN
- 不改业务布局逻辑；仅 tokens + shell/table/card/chip 视觉
- 生产 pin 与本波无关

## 后续可选

1. 设置页暴露 density toggle（读/写 `data-density` + localStorage）
2. Dashboard MetricCard 进度条（cloud-ops `progress` 轨）
3. e2e 视觉回归对照 `tokendance-design/styles/cloud-ops/board.html`
