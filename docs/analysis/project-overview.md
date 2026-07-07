# Project Overview

## Preliminary Direction

将 MetAPI 从 Node.js/TypeScript (Fastify + Drizzle ORM + React SPA) 完整重写为 **Go 单体二进制**，保持 SQLite/PostgreSQL 双方言、14 个上游平台适配器、4 个 AI 协议转换器（OpenAI/Anthropic/Gemini/Codex）、全量后台调度器的功能对等。React 前端编译为静态文件通过 `embed` 内嵌，单二进制部署。

## Current Architecture

TS 版 MetAPI 的三层架构：

```
┌─ React SPA (256 files, ~72k LOC) ──────────────────────┐
│  Vite 6 + Tailwind 4 + React Router 7                   │
│  20 pages: Dashboard/Sites/Accounts/Tokens/Logs/...     │
│  Build → dist/web/ (static HTML/JS/CSS)                 │
├─ Fastify HTTP Server (port 4000) ───────────────────────┤
│  ┌─ Admin API (/api/*) ─────────────────────────────┐   │
│  │  sites accounts tokens checkin stats settings     │   │
│  │  oauth downstreamKeys search events tasks test    │   │
│  │  monitor siteAnnouncements updateCenter auth       │   │
│  │  ~20 route modules, ~80 endpoints                  │   │
│  ├─ Proxy API (/v1/*) ──────────────────────────────┤   │
│  │  chat/completions/responses/embeddings/images      │   │
│  │  models/files/search/videos/gemini                 │   │
│  │  ~11 proxy surfaces                                │   │
│  └─ SPA fallback (/* → index.html)                    │   │
├─ Services Layer (~40 modules, ~61k LOC) ───────────────┤
│  tokenRouter (3800 LOC) — 路由选择/冷却/权重             │
│  siteProxy — 全出站 HTTP 代理/DNS/TLS                    │
│  modelService — 模型探测/发现/定价                        │
│  balanceService / checkinService / notifyService        │
│  oauth (codex/claude/gemini-cli/antigravity)            │
│  backupService / databaseMigrationService               │
├─ proxy-core (67 files, ~14k LOC) ───────────────────────┤
│  DefaultProxyConductor / channelSelection / endpointFlow │
│  sessions lease / sticky session / retry/downgrade      │
├─ transformers (119 files, ~34k LOC) ────────────────────┤
│  OpenAI ↔ Anthropic ↔ Gemini ↔ Codex ↔ canonical       │
│  SSE stream chunk 实时双向转换                            │
├─ Platform Adapters (14 platforms) ──────────────────────┤
│  newApi / oneApi / veloera / oneHub / doneHub / sub2api │
│  openai / claude / codex / gemini / geminiCli           │
│  antigravity / cliproxyapi / anyrouter                  │
└─ Drizzle ORM → SQLite (better-sqlite3) / MySQL / PG ────┘
   27 张表, schemaContract.json 为权威 DDL 源
```

## Technology Stack

| Layer        | Current (TS)                              | Target (Go)                              |
|:-------------|:------------------------------------------|:-----------------------------------------|
| Language     | TypeScript 6 + Node.js ≥25                | Go 1.22+                                 |
| HTTP Server  | Fastify 5                                 | chi v5 (stdlib-compatible router)        |
| Database     | Drizzle ORM (SQLite/MySQL/PG proxy)       | sqlx + 手写 migration (SQLite + PG)      |
| Auth         | Bearer token + IP CIDR                    | 同逻辑, net/netip 库                     |
| Cron         | node-cron                                 | robfig/cron v3                           |
| HTTP Client  | undici (fetch + ProxyAgent)                | net/http + golang.org/x/net/proxy        |
| Validation   | Zod                                       | go-playground/validator + 手写           |
| WebSocket    | ws                                        | gorilla/websocket 或 nhooyr.io/websocket |
| Frontend     | React 18 + Vite 6 + Tailwind 4            | 不变 — embed 静态文件                     |
| Deployment   | Docker (node:25-alpine)                   | Docker (scratch 或 alpine, 单二进制)      |
| Build Tool   | tsc + vite                                | go build                                 |
| Testing      | vitest                                    | go test + testify                         |

## Entry Points

### Server
- `node dist/server/index.js` — HTTP server on `:4000`
- 启动前执行 `node dist/server/db/migrate.js` — SQLite migration
- 12 个后台 scheduler 在 `app.listen` 后并行启动

### API Endpoints (full list)
**管理 API** (需 admin Bearer token + IP allowlist):
- `GET|POST /api/sites`, `PUT|DELETE /api/sites/:id`, `POST /api/sites/detect`, `POST /api/sites/batch`, `GET|PUT /api/sites/:id/disabled-models`, `GET /api/sites/:id/available-models`, `POST /api/sites/:id/probe-now`
- `GET|POST /api/accounts`, `POST /api/accounts/login`, `POST /api/accounts/verify-token`, `POST /api/accounts/:id/rebind-session`, `PUT|DELETE /api/accounts/:id`, `POST /api/accounts/batch`, `POST /api/accounts/health/refresh`, `POST /api/accounts/:id/balance`, `GET /api/accounts/:id/models`, `POST /api/accounts/:id/models/manual`
- `GET|POST /api/account-tokens`, `POST /api/account-tokens/batch`, `PUT|DELETE /api/account-tokens/:id`, `POST /api/account-tokens/sync/:id`, `POST /api/account-tokens/sync-all`, `GET /api/account-tokens/groups/:accountId`, `GET /api/account-tokens/account/:id/default`
- `GET|POST /api/routes`, `GET /api/routes/lite`, `GET /api/routes/summary`, `GET /api/routes/:id/channels`, `POST /api/routes/:id/channels`, `POST /api/routes/:id/channels/batch`, `PUT /api/channels/:channelId`, `DELETE /api/channels/:channelId`, `POST /api/routes/rebuild`, `POST /api/routes/decision/*`
- `GET /api/stats/dashboard`, `GET /api/stats/proxy-logs/:id`, `GET /api/stats/proxy-debug/traces*`, `GET /api/models/marketplace`, `POST /api/models/check/:id`, `POST /api/models/probe`
- `GET|PUT /api/settings/runtime`, `GET|PUT /api/settings/database/runtime`, `POST /api/settings/database/test-connection`, `POST /api/settings/database/migrate`, `GET|POST /api/settings/backup/*`, `POST /api/settings/notify/test`, `POST /api/settings/maintenance/*`
- OAuth: `GET|POST /api/oauth/*` (providers, sessions, connections, route-units, import)
- Downstream keys: `GET|POST|PUT|DELETE /api/downstream-keys/*`
- Events: `GET /api/events`, `POST /api/events/:id/read`, `POST /api/events/read-all`
- 其他: `POST /api/checkin/trigger*`, `GET|PUT /api/checkin/schedule`, `POST /api/search`, `GET /api/tasks*`, `POST /api/site-announcements/*`, `GET|PUT /api/update-center/*`, `GET /api/settings/auth/info`, `GET|POST /api/monitor/*`, `POST /api/test/*`

**公开端点**: `GET /api/desktop/health`, `GET /api/oauth/callback/:provider`

**代理 API** (需下游认证: managed key 或 global proxy token):
- `POST /v1/chat/completions`, `POST /chat/completions`
- `POST /v1/messages`, `POST /v1/messages/count_tokens`
- `POST /v1/completions`
- `POST /v1/responses`, `GET /v1/responses`, `POST /v1/responses/compact`, `POST /responses*`, `GET /responses*`
- `GET /v1/models`
- `POST /v1/embeddings`
- `POST /v1/search`
- `POST /v1/images/generations`, `POST /v1/images/edits`, `POST /v1/images/variations`
- `POST /v1/videos`, `GET /v1/videos/:id`, `DELETE /v1/videos/:id`
- Gemini native surface
- `POST /v1/files/*` (file upload/resolve)

## Build & Run

### Current (TS)
```bash
npm ci
npm run build:web    # vite → dist/web
npm run build:server # tsc → dist/server
node dist/server/db/migrate.js  # SQLite migration (Drizzle)
node dist/server/index.js       # Server on :4000
```

### Target (Go)
```bash
cd web && npm ci && npm run build:web   # 复用现有前端构建
go build -o metapi ./cmd/server
./metapi                                 # 单二进制, :4000, migration 自动跑
```

## Testing Baseline

TS 版有广泛测试 (vitest), 每个 route/service 几乎都有 `.test.ts` 配套文件。Go 版需从零建立测试基础设施。

## Project Governance Baseline

### 现有 surface
- `AGENTS.md` — 工程规则 (Golden Principles, Server Layers, DB Rules, Web Rules)
- `CLAUDE.md` → 重定向到 `@AGENTS.md`
- metapi skill (`<agent-skill-dir>\metapi\SKILL.md`) — 运维/API 操作参考

### Target surface
- Go 版 `AGENTS.md` — Go 版特定的工程规则
- `CLAUDE.md` — Go 版特定的 Claude Code 指令

## External Integrations

| Integration        | Purpose                            | Protocol      |
|:-------------------|:-----------------------------------|:--------------|
| 外部 NewAPI 站点    | 上游 API 代理、签到、余额查询        | HTTPS + REST   |
| OAuth Providers    | Codex/Claude/Gemini CLI/Antigravity | OAuth 2.0 PKCE|
| Webhook/Bark/ServerChan/Telegram/SMTP | 通知推送               | HTTPS/SMTP    |
| WebDAV             | 备份同步                            | HTTPS/WebDAV  |
| GitHub/Docker Hub  | Update Center 版本检查              | HTTPS API     |
| PostgreSQL/SQLite  | 本地持久化                          | TCP / File    |
