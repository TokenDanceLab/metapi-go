# Lane Charters — 每条线独立小组 / Workflow

**规则**：一条线 = 一个 Milestone + 一组 Issues + **一个专属 Workflow 舰队** + 硬文件所有权。  
主 session 只编排，不跨线改代码。两条线不得写同一文件。

## 七条线

| 线 | Milestone | Owner WF name | Issues | 写域（独占） | 禁止写 |
|:---|:----------|:--------------|:-------|:-------------|:-------|
| **STACK** | M1 | `lane-stack` | #3–#7 | `web/package.json`, lock, `tsconfig*`, `vite.config.ts`, stack CI 片段, stack tests | `docs/design/**`, Go 业务, gap 实现 |
| **GAP** | M2 | `lane-gap` | #8–#11 | `docs/analysis/original-gap-*.md`, `docs/plan/original-gap-backlog.md` | 任意 product 代码 |
| **UI** | M3 | `lane-ui` | #12–#15 | `docs/design/DESIGN.md`, `docs/design/**`, `web/styles/**`, `web/index.css`, `web/components/**`, `web/pages/**`（U1/U2 起） | `package.json` 主版本（STACK 独占）, Go |
| **BACKEND** | M4 | `lane-backend` | #16–#19 | `docs/design/BACKEND.md`, `docs/architecture.md`, `routing/**`, `proxy/**`, `handler/**`（B3） | `web/**`, schema 大改 |
| **SCHEMA** | M5 | `lane-schema` | #20–#22 | `docs/analysis/schema-parity.md`, `store/**`, `cmd/migrate/**`, `docs/migration.md` | `web/**`, proxy 算法 |
| **FEATURE** | M6 | `lane-feature` | #23 + backlog 实现 Issues | 按 F0 拆分的 feature 文件（handler/proxy/web 功能点） | 在 F0/G2 完成前禁止开工实现 |
| **RELIABILITY** | M7 | `lane-reliability` | #24–#26 | `auth/**`/`platform/**` 分类, e2e 隔离测试, metrics/health 文档 | UI token, schema 迁移 |

## 共享写域仲裁

| 路径 | 独占线 | 说明 |
|:-----|:-------|:-----|
| `web/package-lock.json` | **STACK only** | UI 线不得 `npm install` 改 lock；需要新依赖时开 Issue 给 STACK |
| `docs/progress/MASTER.md` | **主 session 串行** 或 单 `lane-gate` WF | 各线只 PR 自己的状态段落，合并时由 gate 合入 |
| `CHANGELOG.md` | 各线 PR 追加自己的 section | 冲突时 gate 串行 |
| `store/schema.go` | **SCHEMA only** | FEATURE 需要列时提需求给 SC2，不直改 |

## 每条线的交付物（细节文档）

| 线 | 必交设计/规范文档 |
|:---|:------------------|
| STACK | `docs/plan/milestones-stack-gap.md`（已有）+ 版本矩阵 |
| GAP | `original-gap-sources.md` / `taxonomy.md` / `matrix.md` / `backlog.md` |
| UI | **`docs/design/DESIGN.md`**（SSOT）+ tokens CSS + a11y checklist |
| BACKEND | **`docs/design/BACKEND.md`** + 更新后的 `architecture.md` |
| SCHEMA | `docs/analysis/schema-parity.md` + `docs/migration.md` 升级章 |
| FEATURE | `docs/plan/feature-complete-roadmap.md` + 每功能 DESIGN 小节 |
| RELIABILITY | `docs/analysis/error-classification.md` + failover 证明 |

## Workflow 命名约定

```
lane-stack      → S0–S4
lane-gap        → G1–G4
lane-ui         → U0–U3（DESIGN.md 为第一优先）
lane-backend    → B0–B3
lane-schema     → SC0–SC2
lane-feature    → F0 后开 feature fleets
lane-reliability→ R0–R2
lane-gate       → 跨线合并 / MASTER / 发布闸
```

每个 WF prompt 必须写死：`Lane: <name>`、`Issue #N`、`Allowed files`、`Forbidden files`、`Commit contains #N`。

## 并行矩阵（post enterprise residual — 2026-07-17）

历史七条线（STACK…RELIABILITY）**已闭环**。当前默认是 **residual 波次**：

```
lane-protocol     #309 Gemini thought_signature · #310 Responses multi-turn
lane-observability #311 usage accuracy follow-up
lane-gate         MASTER slim · CHANGELOG · tag · board hygiene
```

文件所有权仍按模块拆分（`transform/gemini/**` ∥ `transform/openai/responses/**` ∥ `handler/proxy` usage / `scheduler`），禁止两 WF 同写 `MASTER.md`。

完整程序图见 `docs/plan/enterprise-program.md`；状态见 `docs/progress/MASTER.md`。

## 反模式（禁止）

1. 一个 WF「顺手」改另一条线的文件  
2. UI 线升级 React/Vite（必须走 STACK）  
3. FEATURE 线在 matrix 完成前实现  
4. 两个 WF 同时写 `MASTER.md`  
5. agent 嵌套 agent 假装并行  
6. 重复开同一主题 Issue / 开已合入功能的重复 PR  
7. 把 MASTER 写成变更日志（写 `CHANGELOG.md` + Release）
