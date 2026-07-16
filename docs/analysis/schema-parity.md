# Schema Parity Audit: metapi-go vs original MetAPI (SC0)

**Issue:** [#20 SC0](https://github.com/TokenDanceLab/metapi-go/issues/20)  
**Date:** 2026-07-16  
**Scope:** DOCS ONLY — no schema/code changes  
**Verdict:** **PARITY PASS** on table/column inventory (27 tables, 354 columns). Remaining differences are intentional dialect/runtime design choices, not missing product columns.

## Sources compared

| Layer | Path | Role |
|:------|:-----|:-----|
| Go structs | `store/schema.go` | Runtime row mapping (`db` / `json` tags) |
| Go DDL | `store/migrate.go` | Dual-dialect `CREATE TABLE IF NOT EXISTS` (SQLite + PostgreSQL) |
| Original TS | `D:/Code/TokenDance/metapi/src/server/db/schema.ts` | Drizzle SQLite table definitions |
| Contract (authoritative) | `D:/Code/TokenDance/metapi/src/server/db/generated/schemaContract.json` | Generated tables + indexes + uniques + FKs |
| Original PG bootstrap | `D:/Code/TokenDance/metapi/src/server/db/generated/postgres.bootstrap.sql` | TS multi-DB bootstrap reference |
| Original migrations | `D:/Code/TokenDance/metapi/drizzle/0000_*.sql` … `0026_*.sql` | Historical additive evolution |
| Prior review (stale) | `docs/specs/review/schema-config-parity.md` | 2026-07-04 note; **site_announcements extra cols already fixed** |

Method: parse Go `// ---- Table N: name ----` structs, Drizzle `sqliteTable(...)` columns, and contract `tables[*].columns`, then diff names set-wise; cross-check Go DDL column lists from `migrate.go`.

---

## 1. Summary counts

| Metric | Original (TS + contract) | Go (`schema.go` + `migrate.go`) | Result |
|:-------|-------------------------:|--------------------------------:|:-------|
| Tables | 27 | 27 | **PASS** |
| Columns (all tables) | 354 | 354 | **PASS** |
| Column name mismatches | — | 0 | **PASS** |
| Missing tables in Go | — | 0 | **PASS** |
| Extra tables in Go | — | 0 | **PASS** |
| Contract indexes | 84 (17 unique + 67 non-unique) | Present in `migrate.go` (not in structs) | **PASS** (encoding location differs) |
| Contract uniques | 17 | Present as UNIQUE / UNIQUE INDEX | **PASS** |
| Contract foreign keys | 20 | Present in DDL | **PASS** |
| Usage CHECK constraints | 3 tables | 3 tables × dual dialect | **PASS** |
| Product column gaps (missing feature fields) | — | 0 vs original | **PASS** |
| Intentional design divergences | — | 8 classes (see §4) | Documented |
| Proposed additive upgrades (enterprise) | — | ≥3 columns / surfaces (see §5) | Forward work for SC1/SC2 |

**Supersedes** older review claim that `site_announcements` had extra `created_at` / `updated_at` in Go: current `SiteAnnouncement` is **17 columns**, matching contract and TS.

---

## 2. Table list parity

All 27 tables exist in TS `schema.ts`, contract, Go `schema.go`, and Go `migrate.go`.

| # | Table | Cols | Go struct | Notes |
|--:|:------|-----:|:----------|:------|
| 1 | `sites` | 19 | `Site` | Site proxy + probe settings |
| 2 | `site_api_endpoints` | 11 | `SiteAPIEndpoint` | Multi-endpoint failover |
| 3 | `site_disabled_models` | 4 | `SiteDisabledModel` | Per-site model denylist |
| 4 | `accounts` | 22 | `Account` | Credentials + OAuth identity |
| 5 | `account_tokens` | 11 | `AccountToken` | Upstream token inventory |
| 6 | `checkin_logs` | 6 | `CheckinLog` | Check-in history |
| 7 | `model_availability` | 7 | `ModelAvailability` | Account-level probe |
| 8 | `token_model_availability` | 6 | `TokenModelAvailability` | Token-level probe |
| 9 | `token_routes` | 12 | `TokenRoute` | Pattern / group routes |
| 10 | `route_group_sources` | 3 | `RouteGroupSource` | Explicit group composition |
| 11 | `oauth_route_units` | 8 | `OAuthRouteUnit` | OAuth unit strategy |
| 12 | `oauth_route_unit_members` | 16 | `OAuthRouteUnitMember` | Unit membership + cooldown |
| 13 | `route_channels` | 20 | `RouteChannel` | Route → credential channel |
| 14 | `proxy_logs` | 24 | `ProxyLog` | Request billing / client meta |
| 15 | `proxy_debug_traces` | 26 | `ProxyDebugTrace` | Debug capture header |
| 16 | `proxy_debug_attempts` | 18 | `ProxyDebugAttempt` | Per-attempt debug body |
| 17 | `proxy_video_tasks` | 15 | `ProxyVideoTask` | Async video task map |
| 18 | `proxy_files` | 13 | `ProxyFile` | Uploaded file store |
| 19 | `settings` | 2 | `Setting` | Text PK KV |
| 20 | `admin_snapshots` | 9 | `AdminSnapshot` | Cached admin payloads |
| 21 | `analytics_projection_checkpoints` | 17 | `AnalyticsProjectionCheckpoint` | Text PK projector lease |
| 22 | `site_day_usage` | 13 | `SiteDayUsage` | Day aggregate + CHECK |
| 23 | `site_hour_usage` | 13 | `SiteHourUsage` | Hour aggregate + CHECK |
| 24 | `model_day_usage` | 13 | `ModelDayUsage` | Model-day aggregate + CHECK |
| 25 | `downstream_api_keys` | 20 | `DownstreamAPIKey` | Downstream key policy |
| 26 | `site_announcements` | 17 | `SiteAnnouncement` | Upstream announcement cache |
| 27 | `events` | 9 | `Event` | Admin event feed |

**Dialect coverage:** Go supports **SQLite + PostgreSQL** only. Original TS also had MySQL bootstrap paths — **MySQL intentionally dropped** in Go (see `docs/specs/p1-database.md`).

---

## 3. Column-level notes (key tables)

Logical types below follow `schemaContract.json`. Go mapping: `integer→int64`, `real→float64`, `boolean→bool` (SQLite stored as INTEGER 0/1), `text/datetime/json→string` with `*T` for true nullability.

### 3.1 `sites` (19)

| Column | Logical | Null | Default | Go field | Notes |
|:-------|:--------|:-----|:--------|:---------|:------|
| `id` | integer | NN | auto | `ID` | PK |
| `name` | text | NN | — | `Name` | |
| `url` | text | NN | — | `URL` | |
| `external_checkin_url` | text | NULL | — | `ExternalCheckinURL` | |
| `platform` | text | NN | — | `Platform` | new-api / one-api / oauth platforms… |
| `proxy_url` | text | NULL | — | `ProxyURL` | **Site-level** upstream egress proxy (already present) |
| `use_system_proxy` | boolean | NULL | false | `UseSystemProxy` | Go non-pointer zero-fill |
| `custom_headers` | json | NULL | — | `CustomHeaders` | TEXT JSON string in Go DDL |
| `status` | text | NN | `'active'` | `Status` | |
| `is_pinned` | boolean | NULL | false | `IsPinned` | |
| `sort_order` | integer | NULL | 0 | `SortOrder` | |
| `global_weight` | real | NULL | 1 | `GlobalWeight` | Routing weight |
| `api_key` | text | NULL | — | `APIKey` | |
| `post_refresh_probe_enabled` | boolean | NULL | false | `PostRefreshProbeEnabled` | |
| `post_refresh_probe_model` | text | NULL | `''` | `PostRefreshProbeModel` | |
| `post_refresh_probe_scope` | text | NULL | `'single'` | `PostRefreshProbeScope` | |
| `post_refresh_probe_latency_threshold_ms` | integer | NULL | 0 | `PostRefreshProbeLatencyThresholdMs` | |
| `created_at` | datetime | NULL | contract: now | `CreatedAt` | App-filled ISO string in Go |
| `updated_at` | datetime | NULL | contract: now | `UpdatedAt` | App-filled ISO string in Go |

**Parity:** full column match.  
**Not present (enterprise upgrade candidates):** `max_concurrency` / site concurrency caps — see §5.

### 3.2 `accounts` (22)

| Column | Logical | Null | Default | Go field | Notes |
|:-------|:--------|:-----|:--------|:---------|:------|
| `id` | integer | NN | auto | `ID` | |
| `site_id` | integer | NN | — | `SiteID` | FK → sites CASCADE |
| `username` | text | NULL | — | `Username` | |
| `access_token` | text | NN | — | `AccessToken` | |
| `api_token` | text | NULL | — | `APIToken` | |
| `balance` | real | NULL | 0 | `Balance` | |
| `balance_used` | real | NULL | 0 | `BalanceUsed` | |
| `quota` | real | NULL | 0 | `Quota` | |
| `unit_cost` | real | NULL | — | `UnitCost` | pointer (true null) |
| `value_score` | real | NULL | 0 | `ValueScore` | |
| `status` | text | NULL | `'active'` | `Status` | active/disabled/expired |
| `is_pinned` | boolean | NULL | false | `IsPinned` | |
| `sort_order` | integer | NULL | 0 | `SortOrder` | |
| `checkin_enabled` | boolean | NULL | true | `CheckinEnabled` | |
| `last_checkin_at` | datetime | NULL | — | `LastCheckinAt` | |
| `last_balance_refresh` | datetime | NULL | — | `LastBalanceRefresh` | |
| `oauth_provider` | text | NULL | — | `OAuthProvider` | |
| `oauth_account_key` | text | NULL | — | `OAuthAccountKey` | |
| `oauth_project_id` | text | NULL | — | `OAuthProjectID` | |
| `extra_config` | json | NULL | — | `ExtraConfig` | free-form account JSON |
| `created_at` / `updated_at` | datetime | NULL | now | `CreatedAt` / `UpdatedAt` | |

**Parity:** full. No original columns missing.

### 3.3 `downstream_api_keys` (20)

| Column | Logical | Null | Default | Go field | Notes |
|:-------|:--------|:-----|:--------|:---------|:------|
| `id` | integer | NN | auto | `ID` | |
| `name` | text | NN | — | `Name` | |
| `key` | text | NN | — | `Key` | UNIQUE |
| `description` | text | NULL | — | `Description` | |
| `group_name` | text | NULL | — | `GroupName` | |
| `tags` | text | NULL | — | `Tags` | documented as JSON array string |
| `enabled` | boolean | NULL | true | `Enabled` | |
| `expires_at` | datetime | NULL | — | `ExpiresAt` | |
| `max_cost` | real | NULL | — | `MaxCost` | soft quota |
| `used_cost` | real | NULL | 0 | `UsedCost` | |
| `max_requests` | integer | NULL | — | `MaxRequests` | |
| `used_requests` | integer | NULL | 0 | `UsedRequests` | |
| `supported_models` | json | NULL | — | `SupportedModels` | allowlist |
| `allowed_route_ids` | json | NULL | — | `AllowedRouteIDs` | route ACL |
| `site_weight_multipliers` | json | NULL | — | `SiteWeightMultipliers` | per-site weight |
| `excluded_site_ids` | json | NULL | — | `ExcludedSiteIDs` | denylist |
| `excluded_credential_refs` | json | NULL | — | `ExcludedCredentialRefs` | credential denylist |
| `last_used_at` | datetime | NULL | — | `LastUsedAt` | |
| `created_at` / `updated_at` | datetime | NULL | now | … | |

**Parity:** full vs original.  
**Gap vs enterprise P0 (not in original either):** no **per-key proxy URL** column — today proxy is site-level (`sites.proxy_url`) or global system proxy. Candidate for SC2 additive column (see §5.1).

### 3.4 `token_routes` (12)

| Column | Logical | Null | Default | Go field | Notes |
|:-------|:--------|:-----|:--------|:---------|:------|
| `id` | integer | NN | auto | `ID` | |
| `model_pattern` | text | NN | — | `ModelPattern` | |
| `display_name` | text | NULL | — | `DisplayName` | |
| `display_icon` | text | NULL | — | `DisplayIcon` | |
| `route_mode` | text | NULL | `'pattern'` | `RouteMode` | pattern vs group |
| `model_mapping` | json | NULL | — | `ModelMapping` | rewrite map |
| `decision_snapshot` | json | NULL | — | `DecisionSnapshot` | cached decision |
| `decision_refreshed_at` | datetime | NULL | — | `DecisionRefreshedAt` | |
| `routing_strategy` | text | NULL | `'weighted'` | `RoutingStrategy` | |
| `enabled` | boolean | NULL | true | `Enabled` | |
| `created_at` / `updated_at` | datetime | NULL | now | … | |

**Parity:** full. No `context_length` on routes in either codebase.

### 3.5 `route_channels` (20)

| Column | Logical | Null | Default | Go field | Notes |
|:-------|:--------|:-----|:--------|:---------|:------|
| `id` | integer | NN | auto | `ID` | |
| `route_id` | integer | NN | — | `RouteID` | FK → token_routes CASCADE |
| `account_id` | integer | NN | — | `AccountID` | FK → accounts CASCADE |
| `token_id` | integer | NULL | — | `TokenID` | FK → account_tokens SET NULL |
| `oauth_route_unit_id` | integer | NULL | — | `OAuthRouteUnitID` | **No FK** in TS or Go (intentional soft link) |
| `source_model` | text | NULL | — | `SourceModel` | |
| `priority` | integer | NULL | 0 | `Priority` | |
| `weight` | integer | NULL | 10 | `Weight` | |
| `enabled` | boolean | NULL | true | `Enabled` | |
| `manual_override` | boolean | NULL | false | `ManualOverride` | |
| `success_count` / `fail_count` | integer | NULL | 0 | … | runtime stats |
| `total_latency_ms` | integer | NULL | 0 | `TotalLatencyMs` | |
| `total_cost` | real | NULL | 0 | `TotalCost` | |
| `last_used_at` / `last_selected_at` / `last_fail_at` | datetime | NULL | — | … | |
| `consecutive_fail_count` | integer | NN | 0 | `ConsecutiveFailCount` | |
| `cooldown_level` | integer | NN | 0 | `CooldownLevel` | |
| `cooldown_until` | datetime | NULL | — | `CooldownUntil` | |

**Parity:** full. Soft `oauth_route_unit_id` (index present, no FK) matches original Drizzle definition.

### 3.6 Adjacent key tables (brief)

| Table | Cols | Status | Highlight |
|:------|-----:|:-------|:----------|
| `account_tokens` | 11 | PASS | `token_group`, `value_status` present |
| `site_api_endpoints` | 11 | PASS | cooldown / selection timestamps |
| `oauth_route_units` | 8 | PASS | strategy default `round_robin` |
| `oauth_route_unit_members` | 16 | PASS | cooldown stats mirror channels |
| `proxy_logs` | 24 | PASS | client_* + first_byte + billing_details |
| `settings` | 2 | PASS | text PK |
| `site_announcements` | 17 | PASS | no local created/updated (uses first/last_seen) |
| `analytics_projection_checkpoints` | 17 | PASS | lease + recompute fields |

---

## 4. Intentional divergences

These are **not** missing original MetAPI product columns. They are design/runtime differences that must remain documented for SC1/SC2.

| # | Class | Original TS | Go | Why intentional |
|--:|:------|:------------|:---|:----------------|
| D1 | Supported DB dialects | SQLite + MySQL + PostgreSQL | **SQLite + PostgreSQL only** | MySQL dropped to cut dialect tax; PG is production path (`docs/specs/p1-database.md`) |
| D2 | Migration machinery | Drizzle journal + recovery + runtime bootstrap introspect/diff | Idempotent `CREATE TABLE IF NOT EXISTS` + separate indexes | Clean-room rewrite; additive versioned migrator is **SC1** |
| D3 | Boolean storage | SQLite INTEGER 0/1; PG BOOLEAN; Drizzle `mode:'boolean'` | Same split in `migrate.go` (`btype`) | Dialect-correct storage |
| D4 | Real storage | SQLite REAL (8-byte); PG bootstrap DOUBLE PRECISION | SQLite REAL / PG **DOUBLE PRECISION** (never bare PG REAL) | Avoid float4 precision loss |
| D5 | JSON storage | Contract logicalType `json`; TS PG bootstrap often **JSONB** | Always **TEXT** JSON strings | Cross-dialect simplicity; app marshal/unmarshal |
| D6 | Datetime defaults | SQLite `datetime('now')`; PG `to_char(timezone('UTC', …))` | Columns `TEXT` **without** DB default; app fills ISO-8601 | Format consistency across dialects |
| D7 | Nullable-with-default Go types | Contract marks many defaulted cols `notNull:false` | Go uses non-pointer `bool`/`int64`/`float64`/`string` for defaulted fields (**87** such fields) | Zero-value == SQL default; true optionals stay pointers |
| D8 | Schema encoding location | Columns+indexes+FKs in Drizzle schema | Columns in `schema.go`; indexes/FKs/CHECKs in `migrate.go` | Expected Go layout; inventory still matches |

### 4.1 Shared intentional soft-FK

`route_channels.oauth_route_unit_id` is indexed in both trees but **has no FOREIGN KEY** in contract (20 FKs total) or Go DDL. Comment in Go migrate: "oauth_route_unit_id has NO FK constraint." Keep as soft association unless product requires hard cascade.

### 4.2 Stale prior finding (closed)

`docs/specs/review/schema-config-parity.md` reported Go `site_announcements.created_at/updated_at`. **Current code:** 17 columns ending at `raw_payload` — aligned with TS/contract. No action for SC0.

---

## 5. Upgrade opportunities (additive only)

Enterprise program (M-SCHEMA / SC2, Wave E4) calls for **additive** columns with defaults that preserve old behavior so original clients remain compatible (ignore unknown columns). None of the following exist in original MetAPI schema either — they are forward upgrades, not parity fixes.

### 5.1 Per-key proxy URL — `downstream_api_keys`

| Item | Proposal |
|:-----|:---------|
| Column | `proxy_url TEXT NULL` (optional: `use_system_proxy` bool default false) |
| Default | `NULL` → fall back to site proxy / system proxy (current behavior) |
| Why | Site-level `sites.proxy_url` is too coarse for multi-tenant downstream keys needing distinct egress |
| Compat | Existing rows NULL; proxy resolver: key → site → system |
| Owner | SC2 |

### 5.2 Site max concurrency — `sites`

| Item | Proposal |
|:-----|:---------|
| Column | `max_concurrency INTEGER NULL` (or `NOT NULL DEFAULT 0` meaning unlimited) |
| Default | `NULL`/`0` → unlimited (current behavior); positive N caps concurrent upstream calls for that site |
| Why | Enterprise program lists "site concurrency"; today only process-global / session channel limits via config (`PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT`) |
| Compat | Unset = no new throttling |
| Owner | SC2 + runtime limiter in proxy/routing |

### 5.3 Context length — model metadata

| Item | Proposal |
|:-----|:---------|
| Option A | Additive on `token_routes`: `context_length INTEGER NULL` for route-level display/validation |
| Option B | New thin table e.g. `model_catalog(model_name PK, context_length, …)` if multi-route reuse needed |
| Default | `NULL` → unknown / no enforcement (current behavior) |
| Why | Clients and admin UI benefit from known context windows; neither original nor Go store this today |
| Compat | Purely additive; enforcement optional behind feature flag |
| Owner | SC2 + feature lane |

### 5.4 Related non-schema notes (out of SC0 code change)

- **Migration strategy** for the above must be dual-dialect and versioned (**SC1**), not only `CREATE IF NOT EXISTS` (which does not alter existing tables).
- **Indexes:** consider `(downstream_api_keys.proxy_url)` only if filtered queries appear; not required at add time.
- **Do not** invent breaking renames or type changes to match TS PG JSONB — keep TEXT JSON for Go dual-dialect parity.

---

## 6. Constraints inventory (parity)

### Foreign keys (20) — match contract

All CASCADE except `route_channels.token_id` → `account_tokens(id)` **ON DELETE SET NULL**.

### Unique indexes (17) — match contract names/columns

Includes `sites_platform_url_unique`, `downstream_api_keys_key_unique`, `oauth_route_unit_members_account_unique`, usage day/hour uniques, etc.

### Non-unique indexes (67) + unique (17) = 84

Present in TS schema + contract; Go creates via `buildIndexes()` / inline UNIQUE. Encoding location differs (D8), membership matches.

### CHECK constraints

`site_day_usage_non_negative`, `site_hour_usage_non_negative`, `model_day_usage_non_negative` — present in both TS and Go dual-dialect DDL.

---

## 7. Type mapping quick reference

| Contract logicalType | TS Drizzle | Go Go-type | SQLite DDL | PG DDL |
|:---------------------|:-----------|:-----------|:-----------|:-------|
| integer (PK) | `integer().primaryKey({autoIncrement:true})` | `int64` | `INTEGER PRIMARY KEY AUTOINCREMENT` | `SERIAL PRIMARY KEY` |
| integer | `integer()` | `int64` / `*int64` | `INTEGER` | `INTEGER` |
| real | `real()` | `float64` / `*float64` | `REAL` | `DOUBLE PRECISION` |
| boolean | `integer({mode:'boolean'})` | `bool` / `*bool` | `INTEGER` | `BOOLEAN` |
| text | `text()` | `string` / `*string` | `TEXT` | `TEXT` |
| datetime | `text()` + default now | `string` / `*string` | `TEXT` | `TEXT` |
| json | `text()` (PG bootstrap JSONB) | `*string` | `TEXT` | `TEXT` (Go) |

---

## 8. Recommendations / exit criteria

| Priority | Action | Issue |
|:---------|:-------|:------|
| Done (this doc) | Table/column parity report vs original | **SC0 #20** |
| Next | Additive migration strategy (version table, dual dialect, safe upgrade for existing DBs) | **SC1 #21** |
| Next | Implement §5 columns with compat defaults + reverse-documented migrations | **SC2 #22** |
| Optional | Refresh or archive stale `docs/specs/review/schema-config-parity.md` site_announcements finding | docs hygiene |

**SC0 acceptance checklist**

- [x] Table/column parity report vs original MetAPI schema (SQLite + PG)
- [x] Document intentional divergences
- [x] Output `docs/analysis/schema-parity.md`
- [x] No schema code changes

---

## 9. Appendix — verification commands (reproducible)

Compared artifacts at audit time:

- Go HEAD branch: `feat/sc0-schema-parity` (from `origin/master`)
- Go structs: 27 tables / 354 `db` tags in `store/schema.go`
- Go DDL: 27 `CREATE TABLE IF NOT EXISTS` builders in `store/migrate.go`
- TS: 27 `sqliteTable` definitions in original `schema.ts`
- Contract: 27 tables, 354 columns, 84 indexes, 17 uniques, 20 foreignKeys

Column-name set equality: **Go == TS == contract** for every table.
