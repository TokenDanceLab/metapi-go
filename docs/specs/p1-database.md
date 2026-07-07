# P1: DB Schema (27 表) + SQLite/PG Migration + Store 方言层

**S.U.P.E.R**: S (单一职责) · P (端口优先) · E (环境无关) | **依赖**: P0 | **Size**: L

## 原始 TS 参考
- `<metapi-ts>\src\server\db\schema.ts` -- 27 表 Drizzle SQLite 定义 (权威列/索引/约束源)
- `<metapi-ts>\src\server\db\index.ts` -- 方言连接管理 (1525 行), 含 SQLite `ensure*Schema()` 兼容层
- `<metapi-ts>\src\server\db\migrate.ts` -- SQLite Drizzle 迁移引擎 (journal/recovery/dedup)
- `<metapi-ts>\src\server\db\runtimeSchemaBootstrap.ts` -- MySQL/PG 运行时 schema bootstrap (introspect + diff + upgrade)
- `<metapi-ts>\src\server\db\generated\schemaContract.json` -- **权威 DDL 源** (84KB JSON: tables + indexes + foreignKeys + uniques)
- `<metapi-ts>\src\server\db\generated\postgres.bootstrap.sql` -- PG DDL 参考
- `<metapi-ts>\drizzle\` -- Drizzle migration 历史 (0000-0026+)

## 关键设计决定

### MySQL 方言已放弃
原始 TS 支持三种方言: SQLite, MySQL, PG。Go 端口**仅支持 SQLite + PG**。MySQL 支持被显式移除。理由:
- PG 已是生产标准 (example-host 跑 PG, example-host 冷备 PG)
- MySQL 的 index prefix (TEXT 列需要 `(191)` 前缀) 和 datetime 处理与 PG/SQLite 差异太大
- 减少 33% 的方言维护负担

### 不做 Drizzle 式 journal/自修复 recovery loop
Go 端口从 scratch 干净设计，使用 `CREATE TABLE IF NOT EXISTS` (幂等) 而非 Drizzle 的 migration journal + recovery loop。详见 [Migration 策略](#migration-策略)。

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

---

## 类型映射

| TS (Drizzle SQLite) | schemaContract logicalType | Go type | PG type | SQLite type | 说明 |
|:---|:---|:---|:---|:---|:---|
| `integer` (autoIncrement PK) | `integer` (primaryKey: true) | `int64` / `sql.NullInt64` | `SERIAL PRIMARY KEY` | `INTEGER PRIMARY KEY AUTOINCREMENT` | auto-increment 主键 |
| `text` | `text` | `string` / `sql.NullString` | `TEXT` | `TEXT` | 字符串/日期/JSON |
| `real` | `real` | `float64` / `sql.NullFloat64` | `DOUBLE PRECISION` | `REAL` | **见下方 PG REAL 警告** |
| `integer` (mode:'boolean') | `boolean` | `bool` / `sql.NullBool` | `BOOLEAN` | `INTEGER` (0/1) | PG 用原生 BOOLEAN, SQLite 用 INTEGER 0/1 |
| `text` (datetime) | `datetime` | `string` / `sql.NullString` | `TEXT` | `TEXT` | ISO 8601 字符串存储, 见日期约定 |
| `text` (JSON) | `json` | `string` (raw JSON) 或 `json.RawMessage` | `TEXT` | `TEXT` | 存储格式为 JSON 字符串, 应用层 marshal/unmarshal |

### PG `REAL` 警告 (CRITICAL)

PostgreSQL 中 `REAL` (不加限定符) 表示 `float4` (4 字节), `DOUBLE PRECISION` 表示 `float8` (8 字节)。
SQLite 的 `REAL` **始终是 8 字节 IEEE 754** (等同于 PG `DOUBLE PRECISION`)。

因此 Go PG migration 的 DDL **必须使用 `DOUBLE PRECISION` (或 `FLOAT8`)**, **禁止使用裸 `REAL`**。
使用裸 `REAL` 会导致精度损失 (从 8 字节降到 4 字节), 破坏与 SQLite 的数值兼容性。

### 日期约定 (关键--与 TS 一致)
- 所有日期/时间存储为 **ISO 8601 字符串** (如 `"2026-07-04T12:00:00.000Z"`)
- Go: `time.Now().UTC().Format("2006-01-02T15:04:05.000Z")`
- PG **不用 `TIMESTAMPTZ`**, 用 `TEXT` 以与 SQLite 一致
- 旧 DB 已有数据使用此格式--必须兼容

### 日期默认值: PG vs SQLite 策略

TS Drizzle 使用 `sql\`(datetime('now'))\`` 作为 SQLite 的列默认值。PG 没有等价的简单表达式。

**Go 端口策略: 应用层填充**

datetime 列在 PG 中定义为 `TEXT` 类型, **不使用 DB 层面的 DEFAULT**。Go 应用层在 INSERT 前负责填充 `created_at`/`updated_at` 等字段为当前 UTC ISO 8601 字符串。

对于 SQLite, 可以保留 `DEFAULT (datetime('now'))` 作为兜底, 但 Go 应用层仍应在 INSERT 时显式设置时间戳。

**为什么不用 PG 函数作为 DEFAULT:**
- 格式一致性差: `to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS".000Z"')` 笨重且容易出错
- 应用层填充保证在所有 dialect 下行为一致
- 测试可控制时间戳 (mock clock)

### JSON 列约定

存储为 JSON 字符串的列 (logicalType `json`) 在 PG 和 SQLite 中使用 `TEXT` 类型。Go 代码负责 marshal/unmarshal。
这些列包括:
- `accounts.extra_config`
- `downstream_api_keys`: `supported_models`, `allowed_route_ids`, `site_weight_multipliers`, `excluded_site_ids`, `excluded_credential_refs`
- `token_routes`: `model_mapping`, `decision_snapshot`
- `proxy_logs.billing_details`
- `proxy_video_tasks`: `status_snapshot`, `upstream_response_meta`
- `proxy_debug_traces`: `request_headers_json`, `request_body_json`, `endpoint_candidates_json`, `endpoint_runtime_state_json`, `decision_summary_json`, `final_response_headers_json`, `final_response_body_json`
- `proxy_debug_attempts`: `request_headers_json`, `request_body_json`, `response_headers_json`, `response_body_json`, `memory_write_json`

---

## 数据库 Schema (27 表)

以下是所有 27 张表的完整定义, 可直接用于生成 CREATE TABLE 语句。

### 表 1: `sites` (Go: `Site`)

**列 (19)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `name` | TEXT | YES | | |
| `url` | TEXT | YES | | |
| `external_checkin_url` | TEXT | NO | NULL | |
| `platform` | TEXT | YES | | 'new-api' / 'one-api' / 'veloera' / 'one-hub' / 'done-hub' / 'sub2api' / 'openai' / 'claude' / 'gemini' / 'codex' / 'gemini-cli' / 'antigravity' |
| `proxy_url` | TEXT | NO | NULL | |
| `use_system_proxy` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `custom_headers` | TEXT | NO | NULL | JSON string |
| `status` | TEXT | YES | 'active' | 'active' / 'disabled' |
| `is_pinned` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `sort_order` | INTEGER | NO | 0 | |
| `global_weight` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 1 | |
| `api_key` | TEXT | NO | NULL | |
| `post_refresh_probe_enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `post_refresh_probe_model` | TEXT | NO | '' | |
| `post_refresh_probe_scope` | TEXT | NO | 'single' | |
| `post_refresh_probe_latency_threshold_ms` | INTEGER | NO | 0 | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- UNIQUE: `sites_platform_url_unique` ON (`platform`, `url`)
- INDEX: `sites_status_idx` ON (`status`)

---

### 表 2: `site_api_endpoints` (Go: `SiteAPIEndpoint`)

**列 (11)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `url` | TEXT | YES | | |
| `enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | true | |
| `sort_order` | INTEGER | NO | 0 | |
| `cooldown_until` | TEXT | NO | NULL | ISO 8601 |
| `last_selected_at` | TEXT | NO | NULL | ISO 8601 |
| `last_failed_at` | TEXT | NO | NULL | ISO 8601 |
| `last_failure_reason` | TEXT | NO | NULL | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- UNIQUE: `site_api_endpoints_site_url_unique` ON (`site_id`, `url`)
- INDEX: `site_api_endpoints_site_enabled_sort_idx` ON (`site_id`, `enabled`, `sort_order`)
- INDEX: `site_api_endpoints_site_cooldown_idx` ON (`site_id`, `cooldown_until`) -- **关键**: cooldown-aware endpoint 选择核心索引

---

### 表 3: `site_disabled_models` (Go: `SiteDisabledModel`)

**列 (4)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `model_name` | TEXT | YES | | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- UNIQUE: `site_disabled_models_site_model_unique` ON (`site_id`, `model_name`)
- INDEX: `site_disabled_models_site_id_idx` ON (`site_id`)

---

### 表 4: `accounts` (Go: `Account`)

**列 (22)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `username` | TEXT | NO | NULL | |
| `access_token` | TEXT | YES | | |
| `api_token` | TEXT | NO | NULL | |
| `balance` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 0 | |
| `balance_used` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 0 | |
| `quota` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 0 | |
| `unit_cost` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | NULL | |
| `value_score` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 0 | |
| `status` | TEXT | NO | 'active' | 'active' / 'disabled' / 'expired' |
| `is_pinned` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `sort_order` | INTEGER | NO | 0 | |
| `checkin_enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | true | |
| `last_checkin_at` | TEXT | NO | NULL | ISO 8601 |
| `last_balance_refresh` | TEXT | NO | NULL | ISO 8601 |
| `oauth_provider` | TEXT | NO | NULL | |
| `oauth_account_key` | TEXT | NO | NULL | |
| `oauth_project_id` | TEXT | NO | NULL | |
| `extra_config` | TEXT | NO | NULL | JSON string |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- INDEX: `accounts_site_id_idx` ON (`site_id`)
- INDEX: `accounts_status_idx` ON (`status`)
- INDEX: `accounts_site_status_idx` ON (`site_id`, `status`)
- INDEX: `accounts_oauth_provider_idx` ON (`oauth_provider`)
- INDEX: `accounts_oauth_identity_idx` ON (`oauth_provider`, `oauth_account_key`, `oauth_project_id`)

---

### 表 5: `account_tokens` (Go: `AccountToken`)

**列 (11)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `account_id` | INTEGER | YES | | FK → accounts(id) ON DELETE CASCADE |
| `name` | TEXT | YES | | |
| `token` | TEXT | YES | | |
| `token_group` | TEXT | NO | NULL | 后加列 (migration 0007) |
| `value_status` | TEXT | YES | 'ready' | 后加列 (migration 0012); 'ready' / ... |
| `source` | TEXT | NO | 'manual' | 'manual' / 'sync' / 'legacy' |
| `enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | true | |
| `is_default` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `account_id` → `accounts(id)` ON DELETE CASCADE
- INDEX: `account_tokens_account_id_idx` ON (`account_id`)
- INDEX: `account_tokens_account_enabled_idx` ON (`account_id`, `enabled`)
- INDEX: `account_tokens_enabled_idx` ON (`enabled`)

---

### 表 6: `checkin_logs` (Go: `CheckinLog`)

**列 (6)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `account_id` | INTEGER | YES | | FK → accounts(id) ON DELETE CASCADE |
| `status` | TEXT | YES | | 'success' / 'failed' / 'skipped' |
| `message` | TEXT | NO | NULL | |
| `reward` | TEXT | NO | NULL | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `account_id` → `accounts(id)` ON DELETE CASCADE
- INDEX: `checkin_logs_account_created_at_idx` ON (`account_id`, `created_at`)
- INDEX: `checkin_logs_created_at_idx` ON (`created_at`)
- INDEX: `checkin_logs_status_idx` ON (`status`)

---

### 表 7: `model_availability` (Go: `ModelAvailability`)

**列 (7)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `account_id` | INTEGER | YES | | FK → accounts(id) ON DELETE CASCADE |
| `model_name` | TEXT | YES | | |
| `available` | BOOLEAN (PG) / INTEGER (SQLite) | NO | NULL | |
| `is_manual` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | 后加列 (migration 0009) |
| `latency_ms` | INTEGER | NO | NULL | |
| `checked_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `account_id` → `accounts(id)` ON DELETE CASCADE
- UNIQUE: `model_availability_account_model_unique` ON (`account_id`, `model_name`)
- INDEX: `model_availability_account_available_idx` ON (`account_id`, `available`)
- INDEX: `model_availability_model_name_idx` ON (`model_name`)

---

### 表 8: `token_model_availability` (Go: `TokenModelAvailability`)

**列 (6)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `token_id` | INTEGER | YES | | FK → account_tokens(id) ON DELETE CASCADE |
| `model_name` | TEXT | YES | | |
| `available` | BOOLEAN (PG) / INTEGER (SQLite) | NO | NULL | |
| `latency_ms` | INTEGER | NO | NULL | |
| `checked_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `token_id` → `account_tokens(id)` ON DELETE CASCADE
- UNIQUE: `token_model_availability_token_model_unique` ON (`token_id`, `model_name`)
- INDEX: `token_model_availability_token_available_idx` ON (`token_id`, `available`)
- INDEX: `token_model_availability_model_name_idx` ON (`model_name`)
- INDEX: `token_model_availability_available_idx` ON (`available`)

---

### 表 9: `token_routes` (Go: `TokenRoute`)

**列 (12)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `model_pattern` | TEXT | YES | | |
| `display_name` | TEXT | NO | NULL | 后加列 |
| `display_icon` | TEXT | NO | NULL | 后加列 |
| `route_mode` | TEXT | NO | 'pattern' | 后加列; 'pattern' / 'group' |
| `model_mapping` | TEXT | NO | NULL | JSON string |
| `decision_snapshot` | TEXT | NO | NULL | JSON string; 后加列 |
| `decision_refreshed_at` | TEXT | NO | NULL | ISO 8601; 后加列 |
| `routing_strategy` | TEXT | NO | 'weighted' | 后加列 |
| `enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | true | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- (无 UNIQUE 约束, 无 FK 约束)
- INDEX: `token_routes_model_pattern_idx` ON (`model_pattern`)
- INDEX: `token_routes_enabled_idx` ON (`enabled`)

---

### 表 10: `route_group_sources` (Go: `RouteGroupSource`)

**列 (3)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `group_route_id` | INTEGER | YES | | FK → token_routes(id) ON DELETE CASCADE |
| `source_route_id` | INTEGER | YES | | FK → token_routes(id) ON DELETE CASCADE |

**约束与索引:**
- PK: `id`
- FK: `group_route_id` → `token_routes(id)` ON DELETE CASCADE
- FK: `source_route_id` → `token_routes(id)` ON DELETE CASCADE
- UNIQUE: `route_group_sources_group_source_unique` ON (`group_route_id`, `source_route_id`)
- INDEX: `route_group_sources_source_route_id_idx` ON (`source_route_id`)

---

### 表 11: `oauth_route_units` (Go: `OAuthRouteUnit`)

**列 (8)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `provider` | TEXT | YES | | |
| `name` | TEXT | YES | | |
| `strategy` | TEXT | YES | 'round_robin' | |
| `enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | true | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- (无 UNIQUE 约束)
- INDEX: `oauth_route_units_site_provider_idx` ON (`site_id`, `provider`)
- INDEX: `oauth_route_units_enabled_idx` ON (`enabled`)

---

### 表 12: `oauth_route_unit_members` (Go: `OAuthRouteUnitMember`)

**列 (16)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `unit_id` | INTEGER | YES | | FK → oauth_route_units(id) ON DELETE CASCADE |
| `account_id` | INTEGER | YES | | FK → accounts(id) ON DELETE CASCADE |
| `sort_order` | INTEGER | NO | 0 | |
| `success_count` | INTEGER | NO | 0 | |
| `fail_count` | INTEGER | NO | 0 | |
| `total_latency_ms` | INTEGER | NO | 0 | |
| `total_cost` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 0 | |
| `last_used_at` | TEXT | NO | NULL | ISO 8601 |
| `last_selected_at` | TEXT | NO | NULL | ISO 8601 |
| `last_fail_at` | TEXT | NO | NULL | ISO 8601 |
| `consecutive_fail_count` | INTEGER | YES | 0 | |
| `cooldown_level` | INTEGER | YES | 0 | |
| `cooldown_until` | TEXT | NO | NULL | ISO 8601 |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `unit_id` → `oauth_route_units(id)` ON DELETE CASCADE
- FK: `account_id` → `accounts(id)` ON DELETE CASCADE
- UNIQUE: `oauth_route_unit_members_unit_account_unique` ON (`unit_id`, `account_id`)
- UNIQUE: `oauth_route_unit_members_account_unique` ON (`account_id`)
- INDEX: `oauth_route_unit_members_unit_sort_idx` ON (`unit_id`, `sort_order`)
- INDEX: `oauth_route_unit_members_unit_cooldown_idx` ON (`unit_id`, `cooldown_until`)

---

### 表 13: `route_channels` (Go: `RouteChannel`)

**列 (20)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `route_id` | INTEGER | YES | | FK → token_routes(id) ON DELETE **CASCADE** |
| `account_id` | INTEGER | YES | | FK → accounts(id) ON DELETE **CASCADE** |
| `token_id` | INTEGER | NO | NULL | FK → account_tokens(id) ON DELETE **SET NULL** |
| `oauth_route_unit_id` | INTEGER | NO | NULL | **无 FK 约束**--仅作数据引用 |
| `source_model` | TEXT | NO | NULL | 后加列 |
| `priority` | INTEGER | NO | 0 | |
| `weight` | INTEGER | NO | 10 | |
| `enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | true | |
| `manual_override` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `success_count` | INTEGER | NO | 0 | |
| `fail_count` | INTEGER | NO | 0 | |
| `total_latency_ms` | INTEGER | NO | 0 | |
| `total_cost` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 0 | |
| `last_used_at` | TEXT | NO | NULL | ISO 8601 |
| `last_selected_at` | TEXT | NO | NULL | ISO 8601; 后加列 |
| `last_fail_at` | TEXT | NO | NULL | ISO 8601 |
| `consecutive_fail_count` | INTEGER | YES | 0 | 后加列 |
| `cooldown_level` | INTEGER | YES | 0 | 后加列 |
| `cooldown_until` | TEXT | NO | NULL | ISO 8601 |

**FK ON DELETE 语义 (CRITICAL):**
删除 `token_route` → 级联删除关联的 `route_channels` (CASCADE)
删除 `account` → 级联删除关联的 `route_channels` (CASCADE)
删除 `account_token` → 将关联 channel 的 `token_id` 置为 NULL (SET NULL), **保留 channel 本身**

如果 Go migration 对所有三个 FK 都使用 CASCADE, 删除 account_token 会错误地删除 channel, 导致生产数据丢失。

**约束与索引:**
- PK: `id`
- FK: `route_id` → `token_routes(id)` ON DELETE CASCADE
- FK: `account_id` → `accounts(id)` ON DELETE CASCADE
- FK: `token_id` → `account_tokens(id)` ON DELETE SET NULL
- INDEX: `route_channels_route_id_idx` ON (`route_id`)
- INDEX: `route_channels_account_id_idx` ON (`account_id`)
- INDEX: `route_channels_token_id_idx` ON (`token_id`)
- INDEX: `route_channels_oauth_route_unit_id_idx` ON (`oauth_route_unit_id`)
- INDEX: `route_channels_route_enabled_idx` ON (`route_id`, `enabled`)
- INDEX: `route_channels_route_token_idx` ON (`route_id`, `token_id`)

---

### 表 14: `proxy_logs` (Go: `ProxyLog`)

**列 (23)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `route_id` | INTEGER | NO | NULL | |
| `channel_id` | INTEGER | NO | NULL | |
| `account_id` | INTEGER | NO | NULL | |
| `downstream_api_key_id` | INTEGER | NO | NULL | 后加列 (migration 0010) |
| `model_requested` | TEXT | NO | NULL | |
| `model_actual` | TEXT | NO | NULL | |
| `status` | TEXT | NO | NULL | 'success' / 'failed' / 'retried' |
| `http_status` | INTEGER | NO | NULL | |
| `is_stream` | BOOLEAN (PG) / INTEGER (SQLite) | NO | NULL | 后加列 (migration 0019) |
| `first_byte_latency_ms` | INTEGER | NO | NULL | 后加列 (migration 0019) |
| `latency_ms` | INTEGER | NO | NULL | |
| `prompt_tokens` | INTEGER | NO | NULL | |
| `completion_tokens` | INTEGER | NO | NULL | |
| `total_tokens` | INTEGER | NO | NULL | |
| `estimated_cost` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | NULL | |
| `billing_details` | TEXT | NO | NULL | JSON string; 后加列 |
| `client_family` | TEXT | NO | NULL | 后加列 |
| `client_app_id` | TEXT | NO | NULL | 后加列 |
| `client_app_name` | TEXT | NO | NULL | 后加列 |
| `client_confidence` | TEXT | NO | NULL | 后加列 |
| `error_message` | TEXT | NO | NULL | |
| `retry_count` | INTEGER | NO | 0 | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引 (7 个 -- 1 single-column + 6 composite):**
- PK: `id`
- (无 UNIQUE, 无 FK)
- INDEX: `proxy_logs_created_at_idx` ON (`created_at`) -- **single-column, 非 composite**
- INDEX: `proxy_logs_account_created_at_idx` ON (`account_id`, `created_at`)
- INDEX: `proxy_logs_status_created_at_idx` ON (`status`, `created_at`)
- INDEX: `proxy_logs_model_actual_created_at_idx` ON (`model_actual`, `created_at`)
- INDEX: `proxy_logs_downstream_api_key_created_at_idx` ON (`downstream_api_key_id`, `created_at`)
- INDEX: `proxy_logs_client_app_id_created_at_idx` ON (`client_app_id`, `created_at`)
- INDEX: `proxy_logs_client_family_created_at_idx` ON (`client_family`, `created_at`)

---

### 表 15: `proxy_debug_traces` (Go: `ProxyDebugTrace`)

**列 (24)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `downstream_path` | TEXT | YES | | |
| `client_kind` | TEXT | NO | NULL | |
| `session_id` | TEXT | NO | NULL | |
| `trace_hint` | TEXT | NO | NULL | |
| `requested_model` | TEXT | NO | NULL | |
| `downstream_api_key_id` | INTEGER | NO | NULL | |
| `request_headers_json` | TEXT | NO | NULL | JSON string |
| `request_body_json` | TEXT | NO | NULL | JSON string |
| `sticky_session_key` | TEXT | NO | NULL | |
| `sticky_hit_channel_id` | INTEGER | NO | NULL | |
| `selected_channel_id` | INTEGER | NO | NULL | |
| `selected_route_id` | INTEGER | NO | NULL | |
| `selected_account_id` | INTEGER | NO | NULL | |
| `selected_site_id` | INTEGER | NO | NULL | |
| `selected_site_platform` | TEXT | NO | NULL | |
| `endpoint_candidates_json` | TEXT | NO | NULL | JSON string |
| `endpoint_runtime_state_json` | TEXT | NO | NULL | JSON string |
| `decision_summary_json` | TEXT | NO | NULL | JSON string |
| `final_status` | TEXT | NO | NULL | |
| `final_http_status` | INTEGER | NO | NULL | |
| `final_upstream_path` | TEXT | NO | NULL | |
| `final_response_headers_json` | TEXT | NO | NULL | JSON string |
| `final_response_body_json` | TEXT | NO | NULL | JSON string |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- (无 UNIQUE, 无 FK)
- INDEX: `proxy_debug_traces_created_at_idx` ON (`created_at`)
- INDEX: `proxy_debug_traces_session_created_at_idx` ON (`session_id`, `created_at`)
- INDEX: `proxy_debug_traces_model_created_at_idx` ON (`requested_model`, `created_at`)
- INDEX: `proxy_debug_traces_final_status_created_at_idx` ON (`final_status`, `created_at`)

---

### 表 16: `proxy_debug_attempts` (Go: `ProxyDebugAttempt`)

**列 (18)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `trace_id` | INTEGER | YES | | FK → proxy_debug_traces(id) ON DELETE CASCADE |
| `attempt_index` | INTEGER | YES | | |
| `endpoint` | TEXT | YES | | |
| `request_path` | TEXT | YES | | |
| `target_url` | TEXT | YES | | |
| `runtime_executor` | TEXT | NO | NULL | |
| `request_headers_json` | TEXT | NO | NULL | JSON string |
| `request_body_json` | TEXT | NO | NULL | JSON string |
| `response_status` | INTEGER | NO | NULL | |
| `response_headers_json` | TEXT | NO | NULL | JSON string |
| `response_body_json` | TEXT | NO | NULL | JSON string |
| `raw_error_text` | TEXT | NO | NULL | |
| `recover_applied` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `downgrade_decision` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `downgrade_reason` | TEXT | NO | NULL | |
| `memory_write_json` | TEXT | NO | NULL | JSON string |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `trace_id` → `proxy_debug_traces(id)` ON DELETE CASCADE
- UNIQUE: `proxy_debug_attempts_trace_attempt_unique` ON (`trace_id`, `attempt_index`)
- INDEX: `proxy_debug_attempts_trace_created_at_idx` ON (`trace_id`, `created_at`)

---

### 表 17: `proxy_video_tasks` (Go: `ProxyVideoTask`)

**列 (14)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `public_id` | TEXT | YES | | |
| `upstream_video_id` | TEXT | YES | | |
| `site_url` | TEXT | YES | | |
| `token_value` | TEXT | YES | | |
| `requested_model` | TEXT | NO | NULL | |
| `actual_model` | TEXT | NO | NULL | |
| `channel_id` | INTEGER | NO | NULL | |
| `account_id` | INTEGER | NO | NULL | |
| `status_snapshot` | TEXT | NO | NULL | JSON string; 后加列 |
| `upstream_response_meta` | TEXT | NO | NULL | JSON string; 后加列 |
| `last_upstream_status` | INTEGER | NO | NULL | 后加列 |
| `last_polled_at` | TEXT | NO | NULL | ISO 8601; 后加列 |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- (无 FK 约束)
- UNIQUE: `proxy_video_tasks_public_id_unique` ON (`public_id`)
- INDEX: `proxy_video_tasks_upstream_video_id_idx` ON (`upstream_video_id`)
- INDEX: `proxy_video_tasks_created_at_idx` ON (`created_at`)

---

### 表 18: `proxy_files` (Go: `ProxyFile`)

**列 (12)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `public_id` | TEXT | YES | | |
| `owner_type` | TEXT | YES | | |
| `owner_id` | TEXT | YES | | |
| `filename` | TEXT | YES | | |
| `mime_type` | TEXT | YES | | |
| `purpose` | TEXT | NO | NULL | |
| `byte_size` | INTEGER | YES | | |
| `sha256` | TEXT | YES | | |
| `content_base64` | TEXT | YES | | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `deleted_at` | TEXT | NO | NULL | ISO 8601; 软删除 |

**约束与索引:**
- PK: `id`
- (无 FK 约束)
- UNIQUE: `proxy_files_public_id_unique` ON (`public_id`)
- INDEX: `proxy_files_owner_lookup_idx` ON (`owner_type`, `owner_id`, `deleted_at`)

---

### 表 19: `settings` (Go: `Setting`) -- **文本主键表**

**列 (2)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `key` | TEXT PK | YES | | **文本主键 (非 SERIAL)** |
| `value` | TEXT | NO | NULL | JSON string |

**约束与索引:**
- PK: `key` -- **文本主键, 非 auto-increment**
- (无其他约束/索引)

---

### 表 20: `admin_snapshots` (Go: `AdminSnapshot`)

**列 (9)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `namespace` | TEXT | YES | | |
| `snapshot_key` | TEXT | YES | | |
| `payload` | TEXT | YES | | |
| `generated_at` | TEXT | YES | | ISO 8601 (NOT NULL, 无默认值) |
| `expires_at` | TEXT | YES | | ISO 8601 (NOT NULL, 无默认值) |
| `stale_until` | TEXT | YES | | ISO 8601 (NOT NULL, 无默认值) |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- (无 FK 约束)
- UNIQUE: `admin_snapshots_namespace_key_unique` ON (`namespace`, `snapshot_key`)
- INDEX: `admin_snapshots_expires_at_idx` ON (`expires_at`)
- INDEX: `admin_snapshots_stale_until_idx` ON (`stale_until`)

---

### 表 21: `analytics_projection_checkpoints` (Go: `AnalyticsProjectionCheckpoint`) -- **文本主键表**

**列 (17)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `projector_key` | TEXT PK | YES | | **文本主键 (非 SERIAL)** |
| `time_zone` | TEXT | YES | 'Local' | |
| `last_proxy_log_id` | INTEGER | YES | 0 | |
| `watermark_created_at` | TEXT | NO | NULL | ISO 8601 |
| `lease_owner` | TEXT | NO | NULL | |
| `lease_token` | TEXT | NO | NULL | |
| `lease_expires_at` | TEXT | NO | NULL | ISO 8601 |
| `recompute_from_id` | INTEGER | NO | NULL | |
| `recompute_requested_at` | TEXT | NO | NULL | ISO 8601 |
| `recompute_reason` | TEXT | NO | NULL | |
| `recompute_started_at` | TEXT | NO | NULL | ISO 8601 |
| `recompute_completed_at` | TEXT | NO | NULL | ISO 8601 |
| `last_projected_at` | TEXT | NO | NULL | ISO 8601 |
| `last_successful_at` | TEXT | NO | NULL | ISO 8601 |
| `last_error` | TEXT | NO | NULL | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `projector_key` -- **文本主键, 非 auto-increment**
- (无 FK/UNIQUE 约束)
- INDEX: `analytics_projection_checkpoints_recompute_from_id_idx` ON (`recompute_from_id`)
- INDEX: `analytics_projection_checkpoints_lease_expires_at_idx` ON (`lease_expires_at`)

---

### 表 22: `site_day_usage` (Go: `SiteDayUsage`)

**列 (12)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `local_day` | TEXT | YES | | 'YYYY-MM-DD' 格式 |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `total_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `success_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `failed_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `total_tokens` | INTEGER | YES | 0 | **CHECK >= 0** |
| `total_summary_spend` | DOUBLE PRECISION (PG) / REAL (SQLite) | YES | 0 | **CHECK >= 0** |
| `total_site_spend` | DOUBLE PRECISION (PG) / REAL (SQLite) | YES | 0 | **CHECK >= 0** |
| `total_latency_ms` | INTEGER | YES | 0 | **CHECK >= 0** |
| `latency_count` | INTEGER | YES | 0 | **CHECK >= 0** |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- UNIQUE: `site_day_usage_day_site_unique` ON (`local_day`, `site_id`)
- CHECK: `site_day_usage_non_negative` -- **以下 8 列均 >= 0**: `total_calls`, `success_calls`, `failed_calls`, `total_tokens`, `total_summary_spend`, `total_site_spend`, `total_latency_ms`, `latency_count`
- INDEX: `site_day_usage_day_idx` ON (`local_day`)
- INDEX: `site_day_usage_site_id_idx` ON (`site_id`)

---

### 表 23: `site_hour_usage` (Go: `SiteHourUsage`)

**列 (12)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `bucket_start_utc` | TEXT | YES | | ISO 8601 (小时桶起始) |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `total_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `success_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `failed_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `total_tokens` | INTEGER | YES | 0 | **CHECK >= 0** |
| `total_summary_spend` | DOUBLE PRECISION (PG) / REAL (SQLite) | YES | 0 | **CHECK >= 0** |
| `total_site_spend` | DOUBLE PRECISION (PG) / REAL (SQLite) | YES | 0 | **CHECK >= 0** |
| `total_latency_ms` | INTEGER | YES | 0 | **CHECK >= 0** |
| `latency_count` | INTEGER | YES | 0 | **CHECK >= 0** |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- UNIQUE: `site_hour_usage_hour_site_unique` ON (`bucket_start_utc`, `site_id`)
- CHECK: `site_hour_usage_non_negative` -- **以下 8 列均 >= 0**: `total_calls`, `success_calls`, `failed_calls`, `total_tokens`, `total_summary_spend`, `total_site_spend`, `total_latency_ms`, `latency_count`
- INDEX: `site_hour_usage_hour_idx` ON (`bucket_start_utc`)
- INDEX: `site_hour_usage_site_id_idx` ON (`site_id`)

---

### 表 24: `model_day_usage` (Go: `ModelDayUsage`)

**列 (13)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `local_day` | TEXT | YES | | 'YYYY-MM-DD' 格式 |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `model` | TEXT | YES | | |
| `total_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `success_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `failed_calls` | INTEGER | YES | 0 | **CHECK >= 0** |
| `total_tokens` | INTEGER | YES | 0 | **CHECK >= 0** |
| `total_spend` | DOUBLE PRECISION (PG) / REAL (SQLite) | YES | 0 | **CHECK >= 0** |
| `total_latency_ms` | INTEGER | YES | 0 | **CHECK >= 0** |
| `latency_count` | INTEGER | YES | 0 | **CHECK >= 0** |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- UNIQUE: `model_day_usage_day_site_model_unique` ON (`local_day`, `site_id`, `model`)
- CHECK: `model_day_usage_non_negative` -- **以下 7 列均 >= 0**: `total_calls`, `success_calls`, `failed_calls`, `total_tokens`, `total_spend`, `total_latency_ms`, `latency_count`
- INDEX: `model_day_usage_day_idx` ON (`local_day`)
- INDEX: `model_day_usage_site_id_idx` ON (`site_id`)
- INDEX: `model_day_usage_model_idx` ON (`model`)

---

### 表 25: `downstream_api_keys` (Go: `DownstreamAPIKey`)

**列 (20)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `name` | TEXT | YES | | |
| `key` | TEXT | YES | | |
| `description` | TEXT | NO | NULL | |
| `group_name` | TEXT | NO | NULL | 后加列 (migration 0011) |
| `tags` | TEXT | NO | NULL | JSON array<string>; 后加列 (migration 0011) |
| `enabled` | BOOLEAN (PG) / INTEGER (SQLite) | NO | true | |
| `expires_at` | TEXT | NO | NULL | ISO 8601 |
| `max_cost` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | NULL | |
| `used_cost` | DOUBLE PRECISION (PG) / REAL (SQLite) | NO | 0 | |
| `max_requests` | INTEGER | NO | NULL | |
| `used_requests` | INTEGER | NO | 0 | |
| `supported_models` | TEXT | NO | NULL | JSON array<string> |
| `allowed_route_ids` | TEXT | NO | NULL | JSON array<number> |
| `site_weight_multipliers` | TEXT | NO | NULL | JSON object { [siteId]: multiplier } |
| `excluded_site_ids` | TEXT | NO | NULL | JSON array<number>; 后加列 |
| `excluded_credential_refs` | TEXT | NO | NULL | JSON array<DownstreamExcludedCredentialRef>; 后加列 |
| `last_used_at` | TEXT | NO | NULL | ISO 8601 |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- (无 FK 约束)
- UNIQUE: `downstream_api_keys_key_unique` ON (`key`)
- INDEX: `downstream_api_keys_name_idx` ON (`name`)
- INDEX: `downstream_api_keys_enabled_idx` ON (`enabled`)
- INDEX: `downstream_api_keys_expires_at_idx` ON (`expires_at`)

注意: TS `ensureDownstreamApiKeySchema()` (index.ts 行 561-609) 初始 migration 仅创建 16 列。`excluded_site_ids` 和 `excluded_credential_refs` 在更晚的 schema 中添加。从 scratch 建表时应包含全部 20 列。

---

### 表 26: `site_announcements` (Go: `SiteAnnouncement`)

**列 (18)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `site_id` | INTEGER | YES | | FK → sites(id) ON DELETE CASCADE |
| `platform` | TEXT | YES | | |
| `source_key` | TEXT | YES | | |
| `title` | TEXT | YES | | |
| `content` | TEXT | YES | | |
| `level` | TEXT | YES | 'info' | |
| `source_url` | TEXT | NO | NULL | |
| `starts_at` | TEXT | NO | NULL | ISO 8601 |
| `ends_at` | TEXT | NO | NULL | ISO 8601 |
| `upstream_created_at` | TEXT | NO | NULL | ISO 8601 |
| `upstream_updated_at` | TEXT | NO | NULL | ISO 8601 |
| `first_seen_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `last_seen_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `read_at` | TEXT | NO | NULL | ISO 8601 |
| `dismissed_at` | TEXT | NO | NULL | ISO 8601 |
| `raw_payload` | TEXT | NO | NULL | |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |
| `updated_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- FK: `site_id` → `sites(id)` ON DELETE CASCADE
- UNIQUE: `site_announcements_site_source_key_unique` ON (`site_id`, `source_key`)
- INDEX: `site_announcements_site_id_first_seen_at_idx` ON (`site_id`, `first_seen_at`)
- INDEX: `site_announcements_read_at_idx` ON (`read_at`)

---

### 表 27: `events` (Go: `Event`)

**列 (9)**:
| 列名 | 类型 | NOT NULL | 默认值 | 说明 |
|:---|:---|:---|:---|:---|
| `id` | SERIAL PK | YES | auto | |
| `type` | TEXT | YES | | 'checkin' / 'balance' / 'token' / 'proxy' / 'status' |
| `title` | TEXT | YES | | |
| `message` | TEXT | NO | NULL | |
| `level` | TEXT | YES | 'info' | 'info' / 'warning' / 'error' |
| `read` | BOOLEAN (PG) / INTEGER (SQLite) | NO | false | |
| `related_id` | INTEGER | NO | NULL | |
| `related_type` | TEXT | NO | NULL | 'account' / 'site' / 'route' |
| `created_at` | TEXT | NO | NULL (应用层填充) | ISO 8601 |

**约束与索引:**
- PK: `id`
- (无 UNIQUE 约束, 无 FK 约束)
- INDEX: `events_read_created_at_idx` ON (`read`, `created_at`)
- INDEX: `events_type_created_at_idx` ON (`type`, `created_at`)
- INDEX: `events_created_at_idx` ON (`created_at`)

---

## 完整 FK 汇总 (ON DELETE 语义)

| 表 | 列 | 引用 | ON DELETE | 说明 |
|:---|:---|:---|:---|:---|
| `accounts` | `site_id` | `sites(id)` | CASCADE | |
| `account_tokens` | `account_id` | `accounts(id)` | CASCADE | |
| `checkin_logs` | `account_id` | `accounts(id)` | CASCADE | |
| `model_availability` | `account_id` | `accounts(id)` | CASCADE | |
| `model_day_usage` | `site_id` | `sites(id)` | CASCADE | |
| `oauth_route_units` | `site_id` | `sites(id)` | CASCADE | |
| `oauth_route_unit_members` | `unit_id` | `oauth_route_units(id)` | CASCADE | |
| `oauth_route_unit_members` | `account_id` | `accounts(id)` | CASCADE | |
| `route_channels` | `route_id` | `token_routes(id)` | CASCADE | |
| `route_channels` | `account_id` | `accounts(id)` | CASCADE | |
| `route_channels` | `token_id` | `account_tokens(id)` | **SET NULL** | 唯一非 CASCADE FK |
| `route_group_sources` | `group_route_id` | `token_routes(id)` | CASCADE | |
| `route_group_sources` | `source_route_id` | `token_routes(id)` | CASCADE | |
| `site_announcements` | `site_id` | `sites(id)` | CASCADE | |
| `site_api_endpoints` | `site_id` | `sites(id)` | CASCADE | |
| `site_day_usage` | `site_id` | `sites(id)` | CASCADE | |
| `site_disabled_models` | `site_id` | `sites(id)` | CASCADE | |
| `site_hour_usage` | `site_id` | `sites(id)` | CASCADE | |
| `token_model_availability` | `token_id` | `account_tokens(id)` | CASCADE | |
| `proxy_debug_attempts` | `trace_id` | `proxy_debug_traces(id)` | CASCADE | |

共 19 个 FK: 18 个 CASCADE + 1 个 SET NULL。`route_channels.oauth_route_unit_id` 和 `proxy_video_tasks` / `proxy_files` 无 FK 约束。

## 完整索引汇总

以下仅列出非 UNIQUE 的 plain 索引 (UNIQUE 约束已在上方各表中标注):

| 表 | 索引名 | 列 |
|:---|:---|:---|
| `sites` | `sites_status_idx` | (`status`) |
| `site_api_endpoints` | `site_api_endpoints_site_enabled_sort_idx` | (`site_id`, `enabled`, `sort_order`) |
| `site_api_endpoints` | `site_api_endpoints_site_cooldown_idx` | (`site_id`, `cooldown_until`) |
| `site_disabled_models` | `site_disabled_models_site_id_idx` | (`site_id`) |
| `accounts` | `accounts_site_id_idx` | (`site_id`) |
| `accounts` | `accounts_status_idx` | (`status`) |
| `accounts` | `accounts_site_status_idx` | (`site_id`, `status`) |
| `accounts` | `accounts_oauth_provider_idx` | (`oauth_provider`) |
| `accounts` | `accounts_oauth_identity_idx` | (`oauth_provider`, `oauth_account_key`, `oauth_project_id`) |
| `account_tokens` | `account_tokens_account_id_idx` | (`account_id`) |
| `account_tokens` | `account_tokens_account_enabled_idx` | (`account_id`, `enabled`) |
| `account_tokens` | `account_tokens_enabled_idx` | (`enabled`) |
| `checkin_logs` | `checkin_logs_account_created_at_idx` | (`account_id`, `created_at`) |
| `checkin_logs` | `checkin_logs_created_at_idx` | (`created_at`) |
| `checkin_logs` | `checkin_logs_status_idx` | (`status`) |
| `model_availability` | `model_availability_account_available_idx` | (`account_id`, `available`) |
| `model_availability` | `model_availability_model_name_idx` | (`model_name`) |
| `token_model_availability` | `token_model_availability_token_available_idx` | (`token_id`, `available`) |
| `token_model_availability` | `token_model_availability_model_name_idx` | (`model_name`) |
| `token_model_availability` | `token_model_availability_available_idx` | (`available`) |
| `token_routes` | `token_routes_model_pattern_idx` | (`model_pattern`) |
| `token_routes` | `token_routes_enabled_idx` | (`enabled`) |
| `route_group_sources` | `route_group_sources_source_route_id_idx` | (`source_route_id`) |
| `oauth_route_units` | `oauth_route_units_site_provider_idx` | (`site_id`, `provider`) |
| `oauth_route_units` | `oauth_route_units_enabled_idx` | (`enabled`) |
| `oauth_route_unit_members` | `oauth_route_unit_members_unit_sort_idx` | (`unit_id`, `sort_order`) |
| `oauth_route_unit_members` | `oauth_route_unit_members_unit_cooldown_idx` | (`unit_id`, `cooldown_until`) |
| `route_channels` | `route_channels_route_id_idx` | (`route_id`) |
| `route_channels` | `route_channels_account_id_idx` | (`account_id`) |
| `route_channels` | `route_channels_token_id_idx` | (`token_id`) |
| `route_channels` | `route_channels_oauth_route_unit_id_idx` | (`oauth_route_unit_id`) |
| `route_channels` | `route_channels_route_enabled_idx` | (`route_id`, `enabled`) |
| `route_channels` | `route_channels_route_token_idx` | (`route_id`, `token_id`) |
| `proxy_logs` | `proxy_logs_created_at_idx` | (`created_at`) |
| `proxy_logs` | `proxy_logs_account_created_at_idx` | (`account_id`, `created_at`) |
| `proxy_logs` | `proxy_logs_status_created_at_idx` | (`status`, `created_at`) |
| `proxy_logs` | `proxy_logs_model_actual_created_at_idx` | (`model_actual`, `created_at`) |
| `proxy_logs` | `proxy_logs_downstream_api_key_created_at_idx` | (`downstream_api_key_id`, `created_at`) |
| `proxy_logs` | `proxy_logs_client_app_id_created_at_idx` | (`client_app_id`, `created_at`) |
| `proxy_logs` | `proxy_logs_client_family_created_at_idx` | (`client_family`, `created_at`) |
| `proxy_debug_traces` | `proxy_debug_traces_created_at_idx` | (`created_at`) |
| `proxy_debug_traces` | `proxy_debug_traces_session_created_at_idx` | (`session_id`, `created_at`) |
| `proxy_debug_traces` | `proxy_debug_traces_model_created_at_idx` | (`requested_model`, `created_at`) |
| `proxy_debug_traces` | `proxy_debug_traces_final_status_created_at_idx` | (`final_status`, `created_at`) |
| `proxy_debug_attempts` | `proxy_debug_attempts_trace_created_at_idx` | (`trace_id`, `created_at`) |
| `proxy_video_tasks` | `proxy_video_tasks_upstream_video_id_idx` | (`upstream_video_id`) |
| `proxy_video_tasks` | `proxy_video_tasks_created_at_idx` | (`created_at`) |
| `proxy_files` | `proxy_files_owner_lookup_idx` | (`owner_type`, `owner_id`, `deleted_at`) |
| `admin_snapshots` | `admin_snapshots_expires_at_idx` | (`expires_at`) |
| `admin_snapshots` | `admin_snapshots_stale_until_idx` | (`stale_until`) |
| `analytics_projection_checkpoints` | `analytics_projection_checkpoints_recompute_from_id_idx` | (`recompute_from_id`) |
| `analytics_projection_checkpoints` | `analytics_projection_checkpoints_lease_expires_at_idx` | (`lease_expires_at`) |
| `site_day_usage` | `site_day_usage_day_idx` | (`local_day`) |
| `site_day_usage` | `site_day_usage_site_id_idx` | (`site_id`) |
| `site_hour_usage` | `site_hour_usage_hour_idx` | (`bucket_start_utc`) |
| `site_hour_usage` | `site_hour_usage_site_id_idx` | (`site_id`) |
| `model_day_usage` | `model_day_usage_day_idx` | (`local_day`) |
| `model_day_usage` | `model_day_usage_site_id_idx` | (`site_id`) |
| `model_day_usage` | `model_day_usage_model_idx` | (`model`) |
| `downstream_api_keys` | `downstream_api_keys_name_idx` | (`name`) |
| `downstream_api_keys` | `downstream_api_keys_enabled_idx` | (`enabled`) |
| `downstream_api_keys` | `downstream_api_keys_expires_at_idx` | (`expires_at`) |
| `site_announcements` | `site_announcements_site_id_first_seen_at_idx` | (`site_id`, `first_seen_at`) |
| `site_announcements` | `site_announcements_read_at_idx` | (`read_at`) |
| `events` | `events_read_created_at_idx` | (`read`, `created_at`) |
| `events` | `events_type_created_at_idx` | (`type`, `created_at`) |
| `events` | `events_created_at_idx` | (`created_at`) |

共 65 个 plain 索引 (不含 UNIQUE 约束索引)。

---

## Migration 策略

### 总体策略
- Go 端口启动时自动执行 migration (镜像 `Dockerfile.slim` CMD)
- SQLite: `CREATE TABLE IF NOT EXISTS` (幂等)
- PG: `CREATE TABLE IF NOT EXISTS` (幂等)
- **不做 Drizzle 式的 journal/自修复 recovery loop**--Go 端口从 scratch 干净设计

### SQLite 初始化 (CRITICAL)

打开 SQLite 连接后, **必须**立即执行两个 PRAGMA (TS `index.ts` 行 1356-1357):

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
```

- **WAL mode**: SQLite 默认 DELETE journal mode 会序列化所有读写操作。WAL mode 允许多个 reader 与一个 writer 并发, 显著提升 proxy_logs 等高频写入表的性能。
- **foreign_keys = ON**: SQLite **默认关闭 FK 约束**。不开启此 PRAGMA 则所有 FK CASCADE/SET NULL 行为被静默忽略, 导致数据完整性问题。

### PG 连接初始化

TS `index.ts` 行 1418 调用 `installPostgresJsonTextParsers()` (在 PG pool 创建时)。此函数注册自定义 PG type parsers (对 `json`/`jsonb` OID 返回 text 而非解析为 JS object), 确保 JSON 列以原始字符串形式返回, 与应用层 marshal/unmarshal 保持一致。

Go 端口需要在 pgx 连接配置中设置相应的 type 解析行为, 确保 JSON 列返回 Go `string` 而非自动解析为 `map[string]interface{}`。

### PG 增量升级策略

**当前 spec 的 `CREATE TABLE IF NOT EXISTS` 仅适用于 green-field DB。** 对于已存在表但没有最新列的 PG DB, 需要增量升级路径。

TS `runtimeSchemaBootstrap.ts` 的升级系统:
1. 读取 `schemaContract.json` 作为 desired state
2. 通过 `information_schema` 内省 live PG schema
3. 构建 compatible baseline (匹配列/索引/unique/FK)
4. 生成增量 DDL (`ALTER TABLE ADD COLUMN`, `CREATE INDEX IF NOT EXISTS`)

**Go 端口建议**: 初期使用 `CREATE TABLE IF NOT EXISTS` 处理 green-field。对于增量升级, 可以:
- 方案 A (推荐): 在启动时使用 `information_schema` 对比 `schemaContract.json`, 生成 `ALTER TABLE ADD COLUMN IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS` 增量 DDL
- 方案 B: 要求生产 DB 通过 export/import 重建 (停机窗口)
- 方案 C: 维护手写 migration 脚本 (与 Drizzle journal 本质上相同, 放弃 "从 scratch 干净 design" 的优势)

---

## 生产 DB 兼容性

### 现有 Drizzle-journaled DB

生产环境 (example-host PG, example-host PG, 以及可能的 SQLite hub.db 文件) 是通过 Drizzle 的增量 migration 系统创建的。关键兼容性问题:

1. **`__drizzle_migrations` 表**: 生产 DB 中存在此表。Go app 启动时发现它已存在, `CREATE TABLE IF NOT EXISTS` 会跳过所有表。然后 Go app 可以正常读写数据。此表可保留不动 (无副作用)。

2. **列缺失**: 如果某些列是后续 migration 添加的 (如 `proxy_logs.is_stream`), `CREATE TABLE IF NOT EXISTS` 不会添加这些列。Go app 访问缺失列会报错。

3. **索引缺失**: 同理, `CREATE TABLE IF NOT EXISTS` 不创建已存在表的索引。

4. **`sites_platform_url_unique` 重复键**: TS `deduplicateLegacySitesForUniqueIndex()` (migrate.ts 行 582-635) 处理了旧数据中 (platform, url) 重复的 sites。如果在有重复数据的 DB 上执行 `CREATE UNIQUE INDEX`, 会失败。

### 推荐方案

**Green-field 部署 (新 DB)**: 直接使用本 spec 的 `CREATE TABLE IF NOT EXISTS` 创建全部 27 表 + 所有索引/约束。完全可行。

**现有生产 DB 兼容**: Go 端口启动时, 对每个表:
- 检查表是否存在 → 不存在则 CREATE TABLE 含全部列和约束
- 检查每列是否存在 → 缺失则 ALTER TABLE ADD COLUMN
- 检查每个索引是否存在 → 缺失则 CREATE INDEX IF NOT EXISTS
- 检查 `sites_platform_url_unique` 是否重复 → 重复则执行 dedup 逻辑 (迁移 TS 的 `deduplicateLegacySitesForUniqueIndex`)

---

## Test Plan

| 文件 | 内容 |
|------|------|
| `store/schema_test.go` | 所有 27 struct 的 sqlx tag 验证; 列名/camelCase 映射一致性 |
| `store/sqlite/migrate_test.go` | SQLite :memory: 中创建所有表 + 索引/约束, 幂等性 (运行2次) |
| `store/postgres/migrate_test.go` | PG testcontainer 中创建所有表 + 索引/约束, 幂等性 |
| `store/setting_store_test.go` | KV 读写/覆盖/不存在 |
| `store/db_test.go` | Open + Close + 基本查询; WAL + foreign_keys PRAGMA 验证 |
| `store/dialect_test.go` | 方言 Now() 格式一致性 |
| `store/compat_test.go` | 对已存在部分列的 DB 做增量迁移验证 |

## Acceptance Criteria
- [ ] 27 张表在 SQLite 和 PG 上均能成功创建 (含全部列、索引、约束)
- [ ] 所有 19 个 FK 的 ON DELETE 行为正确 (18 CASCADE + 1 SET NULL)
- [ ] 所有 65 个 plain 索引 + UNIQUE 约束全部创建
- [ ] 3 个 CHECK 约束 (site_day_usage, site_hour_usage, model_day_usage) 列清单完全匹配 TS
- [ ] `store.Open("sqlite", ":memory:")` → 可立即查询; WAL + foreign_keys PRAGMA 已启用
- [ ] `store.Open("postgres", "postgres://...")` → 可立即查询; JSON 列返回 string 非 parsed object
- [ ] `setting_store.Get("key")` / `Set("key", "value")` 工作 (text PK 表)
- [ ] 启动时 auto-migration 幂等 (重复运行不报错)
- [ ] 日期格式: 存储为 ISO 8601 字符串, 不是原生 timestamp
- [ ] PG 不用 `TIMESTAMPTZ`--用 `TEXT` 与 SQLite 一致
- [ ] PG `REAL` 列使用 `DOUBLE PRECISION`, 不使用裸 `REAL`
- [ ] `store.Close()` 干净关闭连接池
- [ ] `settings` 和 `analytics_projection_checkpoints` 的 text PK 正确创建 (非 SERIAL)
- [ ] `route_channels.oauth_route_unit_id` 无 FK 约束 (与 TS 一致)

## Edge Cases
- SQLite 文件路径含空格/中文 → 正确处理 (modernc.org/sqlite 使用 Go os.Open, Windows 下原生支持 Unicode)
- `DB_URL` 为空 → 默认 `{DATA_DIR}/hub.db`
- `DB_URL=:memory:` → 内存 SQLite (测试用). PG 无等价物--PG 测试必须用 testcontainer 或独立 PG 实例
- PG `DB_SSL=true` → `sslmode=require`
- PG 连接失败 → 清晰报错 (pgx 默认错误信息含 host/port/dbname), 确保不裸 panic, 用 `log.Fatalf` 包装
- 并发 Open → 使用 `sync.Once` 保护 (TS 的 `initDb()` 在 module import 时执行一次, Go 用 `sync.Once` 等价)
- 旧 DB 已有部分表 → 幂等 CREATE IF NOT EXISTS + 列级补全 (见生产 DB 兼容性)
- 旧 DB 有 sites_platform_url_unique 重复 → 执行 dedup 逻辑 (合并重复 site, 重绑定 accounts/disabled_models/site_weight_multipliers)
- SQLite 大文件 (GB 级 proxy_logs) → WAL mode + 合理 busy timeout (建议 5000ms)
- PG 连接池大小: 建议默认 pool_max=20 (与 TS pg.Pool 默认一致)
