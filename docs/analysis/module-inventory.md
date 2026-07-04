# Module Inventory

> S.U.P.E.R Score 格式: `S🟢 U🟡 P🔴 E🟢 R🟡` = Single Purpose 🟢 / Unidirectional 🟡 / Ports 🟠 / Environment 🟢 / Replaceable 🟡

## Summary Table

| Module | Responsibility | Dependencies | Files | Lines | Complexity | S.U.P.E.R Score |
|:-------|:---------------|:-------------|------:|------:|:-----------|:----------------|
| `src/web/` | React SPA 前端 | React, Vite, Tailwind | 256 | ~72k | High | 不变(GO) |
| `src/server/routes/api/` | Admin REST API 端点 | Fastify, services/* | 78 | ~40k | High | `S🟢 U🟢 P🟡 E🟡 R🟡` |
| `src/server/routes/proxy/` | 代理 API 端点 | Fastify, proxy-core, services/* | 51 | ~18k | High | `S🟢 U🟢 P🟡 E🟡 R🟢` |
| `src/server/services/tokenRouter.ts` | 路由选择引擎 | DB, modelService, accountTokenService | 1 | ~3.8k | **Critical** | `S🟡 U🟡 P🟠 E🟡 R🟠` |
| `src/server/services/siteProxy.ts` | 出站 HTTP 代理 | undici, node:dns, node:tls | 1 | ~0.5k | Medium | `S🟢 U🟢 P🟡 E🟠 R🟡` |
| `src/server/services/platforms/` | 14 个平台适配器 | siteProxy, base.ts | 31 | ~8k | High | `S🟢 U🟢 P🟡 E🟢 R🟢` |
| `src/server/services/checkinService.ts` | 签到逻辑 | platforms/*, balanceService | 1 | ~0.8k | Medium | `S🟢 U🟢 P🟡 E🟢 R🟢` |
| `src/server/services/balanceService.ts` | 余额刷新 | platforms/*, siteProxy | 1 | ~0.8k | Medium | `S🟢 U🟢 P🟡 E🟢 R🟢` |
| `src/server/services/notifyService.ts` | 五通道通知 | undici, nodemailer | 1 | ~0.5k | Medium | `S🟢 U🟢 P🟡 E🟡 R🟢` |
| `src/server/services/oauth/` | OAuth 流程 (4 提供方) | siteProxy, config | 12 | ~5k | High | `S🟡 U🟢 P🟡 E🟡 R🟢` |
| `src/server/services/modelService.ts` | 模型发现/探测 | siteProxy, platforms/*, tokenRouter | 1 | ~4k | **Critical** | `S🟠 U🟡 P🟠 E🟡 R🟠` |
| `src/server/services/backupService.ts` | 备份导出/导入/WebDAV | DB, WebDAV client | 1 | ~3k | High | `S🟡 U🟢 P🟡 E🟡 R🟢` |
| `src/server/services/databaseMigrationService.ts` | 跨方言数据迁移 | DB (所有方言) | 1 | ~2k | High | `S🟢 U🟢 P🟡 E🟡 R🟢` |
| `src/server/services/usageAggregationService.ts` | 用量聚合投影 | DB, proxyLogStore | 1 | ~2k | Medium | `S🟢 U🟢 P🟡 E🟡 R🟢` |
| `src/server/proxy-core/` | 代理编排核心 | services/*, transformers/* | 67 | ~14k | **Critical** | `S🟡 U🟡 P🟡 E🟡 R🟡` |
| `src/server/transformers/` | 协议转换 (4 协议) | proxy-core, canonical format | 119 | ~34k | **Critical** | `S🟢 U🟢 P🟢 E🟢 R🟢` |
| `src/server/db/` | 数据库层 | Drizzle ORM, better-sqlite3, mysql2, pg | 38 | ~8k | High | `S🟢 U🟢 P🟡 E🟡 R🟡` |
| `src/server/config.ts` | 环境配置 | dotenv, process.env | 1 | ~0.2k | Low | `S🟢 U🟢 P🟢 E🟢 R🟢` |
| `src/desktop/` | Electron 桌面壳 | Electron | 4 | ~0.5k | Low | 丢弃(GO) |

---

## Module Details

### `src/web/` — React SPA 前端
- **Path**: `src/web/`
- **Responsibility**: 管理面板 UI, 20 页面 (Dashboard/Sites/Accounts/Tokens/Routes/Logs/Monitor/Settings/...), React + Vite + Tailwind + React Router
- **Public API**: 无 — 纯前端, 仅消费 `/api/*` REST API
- **Internal Dependencies**: 无
- **External Dependencies**: react, react-dom, react-router-dom, @visactor/react-vchart, @dnd-kit/*, tailwindcss, vite
- **Complexity Rating**: High
- **Transformation Notes**: **不重写**。Go 版通过 `embed` 内嵌 `dist/web/` 目录, http.FileServer 服务。前端代码本身不动。
- **S.U.P.E.R Assessment**:
  - **S 🟢**: 纯 UI 层, 职责单一
  - **U 🟢**: 单向数据流 (API → state → render)
  - **P 🟡**: REST API 消费端有隐式约定, 无正式 OpenAPI schema
  - **E 🟢**: 纯静态文件, 任何 HTTP server 都能服务
  - **R 🟢**: 可替换为任何 SPA 框架, API 不变即可

### `src/server/routes/api/` — Admin REST API
- **Path**: `src/server/routes/api/`
- **Responsibility**: 所有管理端 REST 端点, ~80 路由, 20 个路由模块
- **Public API**: 每个模块 export 一个 Fastify plugin
- **Internal Dependencies**: `services/*`, `contracts/*`, `middleware/auth`
- **External Dependencies**: Fastify
- **Complexity Rating**: High
- **Transformation Notes**: 直接翻译到 Go chi router handler。每个路由模块 → 一个 Go handler 文件。Contracts (Zod) → Go struct + validator tags。
- **S.U.P.E.R Assessment**:
  - **S 🟢**: 每个 route 文件职责明确 (accounts.ts 只做 account CRUD 路由注册)
  - **U 🟢**: 路由 → service → DB 单向依赖
  - **P 🟡**: Zod validation 定义了输入 schema, 但输出无正式 schema
  - **E 🟡**: 紧耦合 Fastify plugin 注册机制
  - **R 🟡**: 替换 web 框架需要重写所有路由注册

### `src/server/routes/proxy/` — 代理 API 端点
- **Path**: `src/server/routes/proxy/`
- **Responsibility**: OpenAI 兼容 `/v1/*` 代理端点, 含 Anthropic/Codex/Gemini native 表面
- **Public API**: `proxyRoutes` Fastify plugin
- **Internal Dependencies**: `proxy-core/*`, `services/tokenRouter`, `services/siteProxy`
- **External Dependencies**: Fastify
- **Complexity Rating**: High
- **Transformation Notes**: 这些是薄层 — 实际逻辑在 proxy-core。Go 版同样只需要 thin handler 调用 proxy-core。
- **S.U.P.E.R Assessment**:
  - **S 🟢**: 代理表面, 职责单一
  - **U 🟢**: proxy route → proxy-core → tokenRouter → DB, 单向
  - **P 🟡**: OpenAI API 就是 schema (隐含标准), 但无显式 Go struct 定义
  - **E 🟡**: 紧耦合 Fastify
  - **R 🟢**: 代理端点可增删而不影响其他系统

### `src/server/services/tokenRouter.ts` — 路由选择引擎 ⚠️
- **Path**: `src/server/services/tokenRouter.ts`
- **Responsibility**: 通道选择, 加权路由, 断路器, 冷却, 粘滞会话, 模型模式匹配, 缓存
- **Public API**: `selectChannel`, `selectNextChannel`, `selectPreferredChannel`, `recordSuccess`, `recordFailure`, `recordProbeSuccess`, `getAvailableModels`, `refreshPricingReferenceCosts*`, `getSiteRuntimeHealthMultiplier`, `isSiteRuntimeBreakerOpen`, `isChannelRecentlyFailed`
- **Internal Dependencies**: DB (route_channels, account_tokens, model_availability, token_model_availability, token_routes), modelService
- **External Dependencies**: minimatch (glob matching)
- **Complexity Rating**: **Critical** (~3800 LOC single file — 最大单体)
- **Transformation Notes**: **最高优先级的重写目标**。Go 实现时需拆分为多个文件: `matcher.go`, `selector.go`, `cooldown.go`, `cache.go`, `circuit_breaker.go`。单文件 3800 行是明显的 S 原则违规。
- **S.U.P.E.R Assessment**:
  - **S 🟡**: 单个文件承载路由选择 + 冷却管理 + 断路器 + 模型匹配 + 成本计算 + 缓存 — 职责过载, 需拆分
  - **U 🟡**: 与 modelService 存在相互调用 (circular dependency risk)
  - **P 🟠**: 无显式接口契约, 直接读写 DB + 内存缓存, 难以测试和替换
  - **E 🟡**: 硬编码 SQLite datetime 函数依赖, 内存缓存无抽象
  - **R 🟠**: 替换成本极高 — 是整个系统的中枢, 耦合到 DB schema + modelService + accountTokenService

### `src/server/services/modelService.ts` — 模型服务 ⚠️
- **Path**: `src/server/services/modelService.ts`
- **Responsibility**: 模型探测, 刷新, 路由重建, 定价参考
- **Public API**: `probeSiteModels`, `refreshModelsForAccount`, `rebuildTokenRoutesFromAvailability`, `refreshModelsAndRebuildRoutes`
- **Internal Dependencies**: platforms/*, siteProxy, tokenRouter (circular!), accountTokenService
- **External Dependencies**: undici (HTTP probes)
- **Complexity Rating**: **Critical** (~4k LOC)
- **Transformation Notes**: 与 tokenRouter 存在相互依赖 — Go 设计时必须消除此循环。提出 `RouteRebuilder` 接口打破循环。
- **S.U.P.E.R Assessment**:
  - **S 🟠**: 模型探测 + 定价 + 路由重建混在一起
  - **U 🟡**: 与 tokenRouter 有循环依赖 (modelService ↔ tokenRouter)
  - **P 🟠**: 无接口契约, 直接修改全局 DB 状态
  - **E 🟡**: HTTP probe 逻辑与 DB 写入逻辑耦合
  - **R 🟠**: 替换 modelService 需要同时改 tokenRouter

### `src/server/services/platforms/` — 平台适配器
- **Path**: `src/server/services/platforms/`
- **Responsibility**: 14 个上游平台的统一适配接口
- **Public API**: `PlatformAdapter` 接口: `detect`, `login`, `getUserInfo`, `verifyToken`, `checkin`, `getBalance`, `getModels`, `getApiToken(s)`, `getSiteAnnouncements`, `getUserGroups`, `createApiToken`, `deleteApiToken`
- **Internal Dependencies**: siteProxy, config
- **External Dependencies**: undici
- **Complexity Rating**: High (31 files, 继承链复杂)
- **Transformation Notes**: Go 接口模型天然适合此场景。`BasePlatformAdapter` → Go embedded struct 提供默认实现, 各平台只 override 差异方法。适配器注册表 → `map[string]PlatformAdapter`。
- **S.U.P.E.R Assessment**:
  - **S 🟢**: 每个适配器单一平台, 职责清晰。继承链 (NewApi → OneApi → OneHub) 是合理的 DRY
  - **U 🟢**: 适配器 → siteProxy → HTTP, 单向
  - **P 🟡**: `PlatformAdapter` 接口定义了契约, 但无 schema-validated 的输入输出
  - **E 🟢**: 适配器不依赖环境, 只依赖注入的 HTTP client
  - **R 🟢**: 新增/删除平台只需修改注册表, 不影响其他模块 — **这是整个代码库中最符合 S.U.P.E.R 的模块**

### `src/server/proxy-core/` — 代理编排
- **Path**: `src/server/proxy-core/`
- **Responsibility**: 代理请求的完整编排: 通道选择 → 端点执行 → 重试/降级/恢复 → 日志/计费/调试追踪
- **Public API**: `executeEndpointFlow`, `chatSurface`, `sharedSurface`
- **Internal Dependencies**: tokenRouter, transformers/*, siteProxy, proxyLogStore, proxyBilling
- **External Dependencies**: undici, ws (WebSocket)
- **Complexity Rating**: **Critical** (67 files)
- **Transformation Notes**: 核心编排逻辑独立于协议。Go 设计: `ProxyConductor` 接口 + `EndpointFlow` pipeline。与 transformers 通过 `io.Reader` (SSE stream) 解耦。
- **S.U.P.E.R Assessment**:
  - **S 🟡**: orchestrator 职责偏多 (编排 + 会话管理 + 调试追踪), 但已按 surfacing/selection/execution 分目录
  - **U 🟡**: 与 tokenRouter/transformers 有双向依赖 (proxy-core ← tokenRouter, proxy-core → transformers)
  - **P 🟡**: 有内部接口但无显式 schema 边界
  - **E 🟡**: 依赖 undici 特定行为 (stream body, proxy agent)
  - **R 🟡**: 耦合到具体 executor/cli profile 实现

### `src/server/transformers/` — 协议转换
- **Path**: `src/server/transformers/`
- **Responsibility**: OpenAI ↔ Anthropic ↔ Gemini ↔ Codex ↔ canonical 格式的双向转换, 含 SSE stream chunk 实时转换
- **Public API**: 每个 transformer 导出 `toXxx(stream)` 和 `fromXxx(response)` 系列函数
- **Internal Dependencies**: 无 (协议纯函数)
- **External Dependencies**: 无
- **Complexity Rating**: **Critical** (119 files, 纯逻辑量最大)
- **Transformation Notes**: **最复杂的子系统** — 4 个 AI 提供商的请求/响应格式互转, 每个协议有独特的 SSE event 格式、token 计数约定、工具调用表达。Go 实现: 每个方向一个 `Transform(io.Reader) io.Reader` pipeline, 零外部依赖的纯函数。
- **S.U.P.E.R Assessment**:
  - **S 🟢**: 纯转换函数, 每个文件做一件事 (openai/chat 目录仅 chat 相关转换)
  - **U 🟢**: 单向: 输入格式 → canonical → 输出格式
  - **P 🟢**: 输入输出都是 JSON stream, 天然 schema-defined
  - **E 🟢**: 零环境依赖, 纯数据转换 — 可在任何平台测试
  - **R 🟢**: 替换任一协议转换器不影响其他 — **最佳 S.U.P.E.R 合规模块**。Go 重写中应以此为架构模板。

### `src/server/db/` — 数据库层
- **Path**: `src/server/db/`
- **Responsibility**: Schema 定义 (27 表), 三方言连接管理, Migration, Schema contract 生成
- **Public API**: `db` (Drizzle proxy), `schema`, `runtimeDbDialect`, `switchRuntimeDatabase`
- **Internal Dependencies**: 无
- **External Dependencies**: better-sqlite3, mysql2, pg, drizzle-orm
- **Complexity Rating**: High
- **Transformation Notes**: Go 用 sqlx + 手写 migration。`generated/schemaContract.json` 是权威 DDL 源, 可直接生成 Go struct。Drizzle proxy 模式 → sqlx 方言化查询。
- **S.U.P.E.R Assessment**:
  - **S 🟢**: 纯数据层
  - **U 🟢**: DB → services, 单向
  - **P 🟡**: schemaContract.json 是显式合约, 但运行时查询无 schema
  - **E 🟡**: 特定于 Drizzle 的 proxy driver 机制
  - **R 🟡**: 替换 ORM (e.g. sqlc) 需要改所有查询

### `src/server/services/checkinService.ts` + `balanceService.ts` + `notifyService.ts`
- **Path**: `src/server/services/{checkin,balance,notify}Service.ts`
- **Responsibility**: 签到/余额刷新/通知推送
- **Public API**: `checkinAccount`, `checkinAll`, `refreshBalance`, `refreshAllBalances`, `sendNotification`
- **Internal Dependencies**: platforms/*, siteProxy, settings
- **External Dependencies**: undici, nodemailer (仅 notify)
- **Complexity Rating**: Medium
- **Transformation Notes**: 直接翻译, 逻辑清晰。
- **S.U.P.E.R Assessment**:
  - **S 🟢**: 每项服务单一职责
  - **U 🟢**: service → platform adapter → HTTP, 单向
  - **P 🟡**: 无 schema-validated 输出
  - **E 🟢**: 环境无关
  - **R 🟢**: 可独立替换通知通道/签到策略

### `src/server/services/oauth/` — OAuth 子系统
- **Path**: `src/server/services/oauth/`
- **Responsibility**: Codex/Claude/Gemini CLI/Antigravity 的 OAuth 2.0 PKCE 流程
- **Public API**: `startOauthProviderFlow`, `handleOauthCallback`, `listOauthConnections`, `refreshOauthAccessToken`, `buildOauthProviderHeaders`
- **Internal Dependencies**: siteProxy, config, DB
- **External Dependencies**: undici, node:crypto (PKCE)
- **Complexity Rating**: High
- **Transformation Notes**: Go `golang.org/x/oauth2` + 手写 PKCE。Loopback callback server → `net/http/httptest` 或独立 listener。
- **S.U.P.E.R Assessment**:
  - **S 🟡**: OAuth 流程 + session 管理 + token 刷新 + 路由单元混在一起
  - **U 🟢**: OAuth → siteProxy → 上游, 单向
  - **P 🟡**: OAuth 标准本身就是协议, 但内部 session/account 存储无显式合约
  - **E 🟡**: Loopback callback 依赖本机端口绑定
  - **R 🟢**: 新增 provider 只需实现 `OAuthProviderDefinition` 接口
