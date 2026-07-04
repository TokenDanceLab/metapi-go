# Audit: Backup/Import Data Integrity

**Date**: 2026-07-04  
**Scope**: `handler/admin/settings_backup.go`, `handler/admin/settings_maintenance.go`, `scheduler/backup_webdav.go`  
**Database**: 27 tables (defined in `store/schema.go`, migrated in `store/migrate.go`)

---

## Finding Summary

| # | Severity | Finding |
|:--|:---------|:--------|
| 1 | CRITICAL | Backup export returns empty data -- no tables are queried |
| 2 | CRITICAL | Backup import is a no-op -- no data is inserted |
| 3 | CRITICAL | Factory reset is a no-op -- no tables are truncated |
| 4 | HIGH | WebDAV handlers are disconnected from the scheduler implementation |
| 5 | HIGH | WebDAV upload scheduler has no retry logic |
| 6 | MEDIUM | `proxy_files.content_base64` binary column has no size limit or streaming consideration |
| 7 | MEDIUM | No conflict resolution strategy for import (UPSERT, ON CONFLICT) |
| 8 | MEDIUM | `saveWebdavConfig` does not persist config to database |
| 9 | LOW | `getWebdavConfig` returns hardcoded defaults, never reads from DB |
| 10 | LOW | Timestamps are not preserved because no data is exported at all |

---

## 1. Does backup export ALL tables? -- NO (CRITICAL)

**File**: `handler/admin/settings_backup.go`, lines 29-48 (`exportBackup`)

The export handler is a complete stub. It accepts a `?type=` query parameter (`all`, `accounts`, `preferences`) and validates it, but then unconditionally returns an empty data payload:

```go
backup := map[string]any{
    "version":   1,
    "exportedAt": "",
    "type":      exportType,
    "data":      map[string]any{},
}
```

**The following 27 tables receive zero rows in the export:**

| # | Table | Category |
|:--|:------|:---------|
| 1 | `sites` | Core config |
| 2 | `site_api_endpoints` | Core config |
| 3 | `site_disabled_models` | Core config |
| 4 | `accounts` | Accounts (would be `type=accounts`) |
| 5 | `account_tokens` | Accounts |
| 6 | `checkin_logs` | Accounts/logs |
| 7 | `model_availability` | Cache |
| 8 | `token_model_availability` | Cache |
| 9 | `token_routes` | Routes (cache) |
| 10 | `route_group_sources` | Routes |
| 11 | `oauth_route_units` | Routes |
| 12 | `oauth_route_unit_members` | Routes |
| 13 | `route_channels` | Routes (cache) |
| 14 | `proxy_logs` | Logs |
| 15 | `proxy_debug_traces` | Debug |
| 16 | `proxy_debug_attempts` | Debug |
| 17 | `proxy_video_tasks` | Core config |
| 18 | `proxy_files` | Binary data |
| 19 | `settings` | Preferences (would be `type=preferences`) |
| 20 | `admin_snapshots` | Cache |
| 21 | `analytics_projection_checkpoints` | Internal |
| 22 | `site_day_usage` | Analytics |
| 23 | `site_hour_usage` | Analytics |
| 24 | `model_day_usage` | Analytics |
| 25 | `downstream_api_keys` | Core config |
| 26 | `site_announcements` | Core config |
| 27 | `events` | Core config |

The `type` filter suggests a design intent to split into `accounts` (tables 4-6, possibly 1-3) vs `preferences` (table 19), but no queries are executed.

**The WebDAV scheduler** (`scheduler/backup_webdav.go`, lines 112-172) is equally hollow -- it builds a payload with only `version`, `timestamp`, and `type` fields, with zero database queries:

```go
payload := map[string]any{
    "version":   backupVersion,       // "2.1"
    "timestamp": time.Now().UnixMilli(),
}
```

---

## 2. Is binary data (`proxy_files.content_base64`) handled? -- NO (MEDIUM)

**File**: `store/schema.go`, line 310; `store/migrate.go`, lines 787, 804

The `proxy_files` table stores binary file content as a base64-encoded TEXT column:

```go
ContentBase64 string `db:"content_base64" json:"contentBase64"`
```

DDL: `content_base64 TEXT NOT NULL`

Since the export does not query any tables at all, binary data handling is not implemented. When export IS implemented, this column requires special attention:

- **Size**: `content_base64` can contain arbitrarily large base64 strings (e.g., multi-MB files). Exporting these into a single JSON payload could cause memory pressure or serialization failures.
- **JSON encoding**: Base64 strings contain `+`, `/`, `=` characters which are valid JSON but inflate the payload size by ~33% over raw binary.
- **Recommended approach**: Consider splitting proxy_files into a separate export with streaming, or omit binary content from the default export scope and offer a separate "with-files" option.

---

## 3. Are timestamps preserved? -- NO (LOW, subsumed by Finding 1)

All 27 tables use ISO 8601 TEXT columns for timestamps (`created_at`, `updated_at`, various `*_at` columns). The DDL convention (per `docs/specs/p1-database.md`) is to store times as strings like `"2026-07-04T12:00:00.000Z"`, with application-layer population rather than DB defaults.

Since no data is exported, timestamps are not preserved. The `exportedAt` field in the backup envelope is hardcoded as `""` (empty string) instead of being set to `time.Now().UTC().Format(...)`.

When export is implemented, the "date convention" section of P1-database.md confirms that all timestamps are plain TEXT -- no type coercion is needed across SQLite/PG dialects during import. This is architecturally sound and will simplify implementation.

---

## 4. Does import handle conflicts (existing rows)? -- NO (MEDIUM)

**File**: `handler/admin/settings_backup.go`, lines 51-68 (`importBackup`)

The import handler is a complete stub. It decodes the JSON body but does not insert or update any rows:

```go
writeJSON(w, http.StatusOK, map[string]any{
    "success":         true,
    "message":         "导入完成",
    "appliedSettings": []any{},
})
```

There is no UPSERT logic, no `ON CONFLICT DO UPDATE`, no "skip existing" strategy, no dry-run mode, and no row-level reporting. When import is implemented, a conflict resolution strategy must be chosen for each table category:

| Table category | Recommended strategy |
|:---------------|:---------------------|
| `sites`, `accounts`, `account_tokens`, `downstream_api_keys`, `token_routes` | UPSERT by natural key or `ON CONFLICT (id) DO UPDATE` |
| `settings` | `INSERT OR REPLACE` (SQLite) / `ON CONFLICT (key) DO UPDATE` (PG) |
| `proxy_logs`, `checkin_logs`, events | Append-only (INSERT with new IDs) or skip entirely for import |
| `model_availability`, `token_model_availability`, `route_channels` | Skip (cache tables, regenerated after import) |

---

## 5. Is factory reset complete (all tables)? -- NO (CRITICAL)

**File**: `handler/admin/settings_maintenance.go`, lines 71-74 (`factoryReset`)

```go
func (h *maintenanceHandler) factoryReset(w http.ResponseWriter, r *http.Request) {
    // Stub: factory reset not yet implemented
    writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
```

This returns HTTP 200 with `{"success": true}` but deletes **zero rows** from any table. It does not even have a safety confirmation mechanism (no confirmation token, no "are you sure" check, no reading of a request body field).

For reference, the sibling maintenance handlers (`clearCache`, `clearUsage`) ARE fully implemented with real DELETE/UPDATE queries. By contrast:

- `clearCache` (line 24-46) correctly DELETEs from `model_availability`, `route_channels`, `token_routes`
- `clearUsage` (line 49-68) correctly DELETEs from `proxy_logs` and resets `route_channels` stats and `accounts.balance_used`
- `factoryReset` (line 71-74) is a stub

A complete factory reset should TRUNCATE all 27 tables (or at minimum all user-data tables, excluding migration bookkeeping). It should also require a confirmation payload to prevent accidental invocation.

---

## 6. Does WebDAV upload handle auth and retry? -- PARTIAL (HIGH)

### 6a. Handler layer (stubs)

**File**: `handler/admin/settings_backup.go`

All four WebDAV HTTP handlers are stubs:

| Handler | Line | Behavior |
|:--------|:-----|:---------|
| `getWebdavConfig` | 72-82 | Returns hardcoded `enabled: false, fileUrl: ""` -- never reads from DB |
| `saveWebdavConfig` | 85-102 | Parses body but never persists to `settings` table |
| `exportToWebdav` | 105-109 | Returns `success: true` without any WebDAV operation |
| `importFromWebdav` | 113-117 | Returns `success: true` without any WebDAV operation |

These handlers are completely disconnected from the scheduler implementation.

### 6b. Scheduler layer (partial implementation)

**File**: `scheduler/backup_webdav.go`

The scheduler does have a real WebDAV client implementation:

- **Auth**: YES (partial). Supports HTTP Basic Auth via `req.SetBasicAuth(cfg.Username, cfg.Password)` (line 151). The `saveWebdavConfig` handler accepts a `password` field in the request body, but never persists it. Thus the scheduler cannot receive credentials configured through the API.
- **Config persistence**: The scheduler reads from `settings` table key `backup_webdav_config_v1` (line 18), but the HTTP handler never writes to this key.
- **Retry**: NO. The scheduler makes a single `PUT` request with a 15-second timeout (line 21). On failure, it logs the error and updates state -- it does not retry. There is no exponential backoff, no retry count limit, no circuit breaker.
- **Export content**: The scheduler builds an export payload with only `version` and `timestamp` (lines 122-125) -- no actual table data is queried or included.

### 6c. WebDAV import

The scheduler has no import capability at all. There is only `runExport` (line 112); no `runImport` exists. The API handler `importFromWebdav` is a stub.

---

## Structural Issues

### Disconnected layers

The backup system is split across three disconnected layers:

1. **HTTP handlers** (`settings_backup.go`) -- All stubs, return hardcoded responses
2. **Scheduler** (`scheduler/backup_webdav.go`) -- Has real HTTP client and config loading, but its own `runExport` also builds a hollow payload
3. **Settings store** (`store/setting_store.go`) -- Provides KV persistence that the scheduler uses but the handlers ignore

None of these layers share code. The handlers do not call the scheduler. The scheduler does not use any shared export logic. There is no `ExportService` or `BackupService` that both layers could consume.

### Missing shared export engine

A proper implementation would extract a shared `BackupExporter` that:
- Queries all (or filtered) tables
- Serializes rows with timestamp preservation
- Handles `content_base64` as a special case (size check, optional omit)
- Produces a versioned JSON envelope
- Is consumed by both the HTTP export handler and the WebDAV scheduler

### Clear-cache scope mismatch

The `clearCache` handler in `settings_maintenance.go` only targets 3 tables (`model_availability`, `route_channels`, `token_routes`) but the full set of cache/recompute tables includes:

- `model_availability` (cleared)
- `token_model_availability` (NOT cleared)
- `route_channels` (cleared)
- `token_routes` (cleared)
- `admin_snapshots` (NOT cleared)
- `analytics_projection_checkpoints` (NOT cleared)
- `site_day_usage`, `site_hour_usage`, `model_day_usage` (NOT cleared -- these are analytics, debatable whether they are "cache")

Whether the omission of `token_model_availability`, `admin_snapshots`, and `analytics_projection_checkpoints` is intentional or an oversight should be confirmed.

---

## Recommendations

| Priority | Action |
|:---------|:-------|
| P0 | Implement actual export: query all 27 tables (or subset by `type`), serialize rows with timestamps |
| P0 | Implement actual import: INSERT/UPDATE rows with conflict resolution strategy per table category |
| P0 | Implement factory reset: TRUNCATE all tables with a mandatory confirmation token in the request body |
| P1 | Unify HTTP handlers and scheduler to share an export engine |
| P1 | Add retry with exponential backoff to WebDAV upload (3 attempts, 1s/2s/4s) |
| P1 | Wire `saveWebdavConfig` to persist to `settings` table key `backup_webdav_config_v1` |
| P1 | Wire `getWebdavConfig` to read from `settings` table |
| P2 | Add size guard for `proxy_files.content_base64` during export (warn or skip rows > N MB) |
| P2 | Implement WebDAV import in both the scheduler and handler |
| P2 | Review `clearCache` table scope -- should `token_model_availability`, `admin_snapshots`, `analytics_projection_checkpoints` be included? |
