# P1: DB Schema (27 表) + SQLite/PG Migration + Store 方言层

**S.U.P.E.R**: S (单一职责) · P (端口优先) · E (环境无关) | **依赖**: P0 | **Size**: L

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\db\schema.ts` — 27 表 Drizzle 定义
- `D:\Code\TokenDance\metapi\src\server\db\index.ts` — 方言连接管理 (1525 行)
- `D:\Code\TokenDance\metapi\src\server\db\migrate.ts` — SQLite 迁移脚本
- `D:\Code\TokenDance\metapi\src\server\db\runtimeSchemaBootstrap.ts` — MySQL/PG bootstrap
- `D:\Code\TokenDance\metapi\src\server\db\generated\schemaContract.json` — **权威 DDL 源** (84KB JSON)
- `D:\Code\TokenDance\metapi\src\server\db\generated\postgres.bootstrap.sql` — PG DDL 参考
- `D:\Code\TokenDance\metapi\drizzle\` — Drizzle migration 历史

## Go 模块结构
```
store/
  schema.go          # 27 个 Go struct (sqlx tags)
  open.go            # Open(dialect, dsn) → *DB
  dialect.go         # Dialect 接口: DriverName, Migrate, Now, Placeholder, ...
  migrate.go         # 自动 migration (启动时)
  sqlite/
    driver.go        # SQLite 方言: modernc.org/sqlite (纯 Go, 无 CGO)
    migrate.go       # SQLite CREATE TABLE IF NOT EXISTS (幂等)
  postgres/
    driver.go        # PG 方言: jackc/pgx
    migrate.go       # PG CREATE TABLE IF NOT EXISTS (幂等)
  db.go              # *DB: sqlx.DB wrapper + 便利方法
  setting_store.go   # settings 表 KV 读写
```

## 数据库 Schema (27 表, 完整列)

必须完全匹配 TS 版的列名、类型、约束、索引。以下是 **精简映射** (完整 27 表细节见 TS 原始 schema.ts):

| # | 表 | Go struct | 关键约束 |
|---|-----|-----------|----------|
| 1 | `sites` | `Site` | UNIQUE(platform, url) |
| 2 | `site_api_endpoints` | `SiteAPIEndpoint` | UNIQUE(site_id, url) |
| 3 | `site_disabled_models` | `SiteDisabledModel` | UNIQUE(site_id, model_name) |
| 4 | `accounts` | `Account` | FK → sites(cascade) |
| 5 | `account_tokens` | `AccountToken` | FK → accounts(cascade) |
| 6 | `checkin_logs` | `CheckinLog` | FK → accounts(cascade) |
| 7 | `model_availability` | `ModelAvailability` | UNIQUE(account_id, model_name) |
| 8 | `token_model_availability` | `TokenModelAvailability` | UNIQUE(token_id, model_name) |
| 9 | `token_routes` | `TokenRoute` | — |
| 10 | `route_group_sources` | `RouteGroupSource` | UNIQUE(group_route_id, source_route_id) |
| 11 | `oauth_route_units` | `OAuthRouteUnit` | — |
| 12 | `oauth_route_unit_members` | `OAuthRouteUnitMember` | UNIQUE(unit_id, account_id); UNIQUE(account_id) |
| 13 | `route_channels` | `RouteChannel` | FK → token_routes + accounts + account_tokens |
| 14 | `proxy_logs` | `ProxyLog` | 7 composite indexes on created_at |
| 15 | `proxy_debug_traces` | `ProxyDebugTrace` | — |
| 16 | `proxy_debug_attempts` | `ProxyDebugAttempt` | UNIQUE(trace_id, attempt_index) |
| 17 | `proxy_video_tasks` | `ProxyVideoTask` | UNIQUE(public_id) |
| 18 | `proxy_files` | `ProxyFile` | UNIQUE(public_id) |
| 19 | `settings` | `Setting` | PK=key (text) |
| 20 | `admin_snapshots` | `AdminSnapshot` | UNIQUE(namespace, snapshot_key) |
| 21 | `analytics_projection_checkpoints` | `AnalyticsProjectionCheckpoint` | PK=projector_key (text) |
| 22 | `site_day_usage` | `SiteDayUsage` | UNIQUE(local_day, site_id); CHECK ≥0 |
| 23 | `site_hour_usage` | `SiteHourUsage` | UNIQUE(bucket_start_utc, site_id); CHECK ≥0 |
| 24 | `model_day_usage` | `ModelDayUsage` | UNIQUE(local_day, site_id, model); CHECK ≥0 |
| 25 | `downstream_api_keys` | `DownstreamAPIKey` | UNIQUE(key) |
| 26 | `site_announcements` | `SiteAnnouncement` | UNIQUE(site_id, source_key) |
| 27 | `events` | `Event` | — |

### 类型映射

| TS (Drizzle SQLite) | Go struct tag | PG type | 说明 |
|:---|:---|:---|:---|
| `integer` (autoIncrement PK) | `db:"id"` | `SERIAL PRIMARY KEY` | sqlx tag |
| `text` | `db:"..."` | `TEXT` | 字符串/JSON/日期 |
| `real` | `db:"..."` | `DOUBLE PRECISION` | 浮点数 |
| `integer` (mode:'boolean') | `db:"..."` | `BOOLEAN` | PG 用 BOOLEAN, SQLite 用 INTEGER 0/1 |
| `text` + `default sql\`(datetime('now'))\`` | — | `TEXT DEFAULT ...` | ISO 8601 字符串, 不用原生 TIMESTAMP |

### 日期约定 (关键——与 TS 一致)
- 所有日期/时间存储为 **ISO 8601 字符串** (如 `"2026-07-04T12:00:00.000Z"`)
- Go: `time.Now().UTC().Format("2006-01-02T15:04:05.000Z")`
- PG 不用 `TIMESTAMPTZ`, 用 `TEXT` 以与 SQLite 一致
- 旧 DB 已有数据使用此格式——必须兼容

### Migration 策略
- 镜像 `Dockerfile.slim` CMD: 启动时自动跑 migration
- SQLite: `CREATE TABLE IF NOT EXISTS` (幂等)
- PG: `CREATE TABLE IF NOT EXISTS` (幂等)
- 不做 Drizzle 式的 journal/自修复 recovery loop——从 scratch 干净 design

## Acceptance Criteria
- [ ] 27 张表在 SQLite 和 PG 上均能成功创建
- [ ] `store.Open("sqlite", ":memory:")` → 可立即查询
- [ ] `store.Open("postgres", "postgres://...")` → 可立即查询
- [ ] 所有列名、类型、约束、索引与 TS 版 schema.ts 完全一致
- [ ] `setting_store.Get("key")` / `Set("key", "value")` 工作
- [ ] 启动时 auto-migration 幂等 (重复运行不报错)
- [ ] 日期格式: 存储为 ISO 8601 字符串, 不是原生 timestamp
- [ ] PG 不用 `TIMESTAMPTZ`——用 `TEXT` 与 SQLite 一致
- [ ] `store.Close()` 干净关闭连接池
- [ ] 与现有生产 DB 文件兼容 (可直接打开 us1/hk2 的 hub.db)

## Test Plan
| 文件 | 内容 |
|------|------|
| `store/schema_test.go` | 所有 27 struct 的 sqlx tag 验证 |
| `store/sqlite/migrate_test.go` | SQLite :memory: 中创建所有表, 幂等性 |
| `store/postgres/migrate_test.go` | PG testcontainer 中创建所有表, 幂等性 |
| `store/setting_store_test.go` | KV 读写/覆盖/不存在 |
| `store/db_test.go` | Open + Close + 基本查询 |
| `store/dialect_test.go` | 方言 Now() 格式一致性 |

## Edge Cases
- SQLite 文件路径含空格/中文 → 正确处理
- `DB_URL` 为空 → 默认 `{DATA_DIR}/hub.db`
- `DB_URL=:memory:` → 内存 SQLite (测试用)
- PG `DB_SSL=true` → `sslmode=require`
- PG 连接失败 → 清晰报错 (含 host/dbname), 不要 panic trace
- 并发 Open → 单例/锁保护
- 旧 DB 已有部分表 → 幂等 CREATE IF NOT EXISTS
