# Audit: Backup Export-Import Roundtrip Test

**Date**: 2026-07-05
**Test file**: `e2e/e2e_backup_test.go`
**Scope**: Full roundtrip -- seed all 27 tables, export JSON, factory-reset, import JSON, verify integrity
**Prior audit**: `docs/specs/review/audits/audit-backup.md` (2026-07-04) identified all three P0 gaps as fixed

---

## Test Suite Overview

| # | Test | Coverage |
|:--|:-----|:---------|
| 1 | `TestBackupExportImportRoundtrip` | Full roundtrip: seed 27 tables -> export -> reset -> import -> verify |
| 2 | `TestBackupExportImportAccountsOnly` | Accounts type filter exports correct table subset (17 tables) |
| 3 | `TestBackupExportImportPreferencesOnly` | Preferences type filter exports only `settings` table |
| 4 | `TestBackupExportInvalidType` | Invalid `?type=` returns 400 |
| 5 | `TestBackupEmptyDatabaseRoundtrip` | Empty DB export/import succeeds with zero rows |
| 6 | `TestBackupImportMalformedJSON` | Import with missing `tables` key returns 400 |
| 7 | `TestBackupSettingsJSONFidelity` | JSON array fields (`supported_models`, `custom_headers`) survive roundtrip exactly |
| 8 | `TestBackupRouteChannelTokenNullFK` | `route_channels.token_id` FK (ON DELETE SET NULL) survives roundtrip |

---

## Phase 1: Seed Data (all 27 tables)

The test seeds all 27 tables with FK-safe data in the canonical order defined by `allTables` in `handler/admin/settings_backup.go`:

```
sites (2) -> site_api_endpoints (2) -> site_disabled_models (1) ->
accounts (2) -> account_tokens (3) -> checkin_logs (2) ->
model_availability (2) -> token_model_availability (2) ->
token_routes (2) -> route_group_sources (2) ->
oauth_route_units (2) -> oauth_route_unit_members (2) ->
route_channels (2) -> proxy_logs (2) -> proxy_debug_traces (2) ->
proxy_debug_attempts (2) -> proxy_video_tasks (2) -> proxy_files (2) ->
settings (2) -> admin_snapshots (2) -> analytics_projection_checkpoints (1) ->
site_day_usage (2) -> site_hour_usage (1) -> model_day_usage (2) ->
downstream_api_keys (2) -> site_announcements (2) -> events (2)
```

**Total seed rows**: 53 rows across 27 tables.

### FK constraints exercised

| FK | From | To | Action |
|:---|:-----|:---|:-------|
| sites.id | site_api_endpoints, site_disabled_models, accounts, oauth_route_units, site_day_usage, site_hour_usage, model_day_usage, site_announcements | sites | CASCADE |
| accounts.id | account_tokens, checkin_logs, model_availability, oauth_route_unit_members, route_channels | accounts | CASCADE |
| account_tokens.id | token_model_availability, route_channels | account_tokens | CASCADE / SET NULL |
| token_routes.id | route_group_sources (x2), route_channels | token_routes | CASCADE |
| oauth_route_units.id | oauth_route_unit_members | oauth_route_units | CASCADE |
| proxy_debug_traces.id | proxy_debug_attempts | proxy_debug_traces | CASCADE |

---

## Phase 2: Export Verification

The export endpoint at `GET /api/settings/backup/export?type=all` returns:

```json
{
  "metadata": {
    "exported_at": "2026-07-05T...",
    "version": "1.0"
  },
  "type": "all",
  "tables": {
    "sites": [...],
    "site_api_endpoints": [...],
    ...
  }
}
```

**Verified**:
- All 27 tables present in export payload
- Each table has exactly the expected row count
- Metadata includes `version` and `exported_at` timestamp
- `type` field matches request parameter

---

## Phase 3: Factory Reset Verification

The factory reset endpoint at `POST /api/settings/maintenance/factory-reset`:

1. **Rejects unconfirmed requests**: `{"confirm":false}` returns 400 with message "工厂重置需要确认"
2. **Accepts confirmed requests**: `{"confirm":true}` returns 200 with `success:true` and per-table deleted counts
3. **Truncates all 27 tables**: Verified all tables have 0 rows after reset
4. **Resets auto-increment**: `DELETE FROM sqlite_sequence` resets all SERIAL IDs back to 1
5. **Deletes in reverse FK order**: Children before parents (children are deleted first)

---

## Phase 4: Import Verification

The import endpoint at `POST /api/settings/backup/import`:

1. **Restores all 27 tables**: All 53 seed rows restored
2. **Preserves row IDs**: Since auto-increment was reset, explicit `id` values from the export match the now-empty tables
3. **Uses INSERT OR IGNORE**: Non-conflicting strategy appropriate for a clean DB after factory reset
4. **Returns per-table counts**: Response includes `imported` object with row counts per table

---

## Phase 5: Data Integrity Verification

### Row count check

All 27 tables verified with exact expected row counts after import.

### Spot checks (value fidelity)

| Table | Field | Expected | Result |
|:------|:------|:---------|:-------|
| sites (id=1) | name | `Test Site A` | PASS |
| sites (id=1) | url | `https://api.openai.com` | PASS |
| accounts (id=1) | balance | `100.5` | PASS |
| checkin_logs (id=1) | status | `success` | PASS |
| checkin_logs (id=1) | reward | `100 credits` | PASS |
| proxy_files (id=1) | public_id | `file-pub-001` | PASS |
| proxy_files (id=1) | content_base64 | `dGVzdA==` | PASS |
| settings | key=`app.theme` | `dark` | PASS |
| analytics_projection_checkpoints | projector_key | `day-usage-projection` | PASS |
| downstream_api_keys | key=`sk-downstream-key-a` | present (1 row) | PASS |
| events (id=1) | type | `info` | PASS |

### Re-export structural comparison

After import, a second export was performed and compared field-by-field against the original export for the first row of each table. All 27 tables matched.

### Foreign key integrity

All FK relationships verified after import:
- 0 orphan `site_api_endpoints` (all site_ids valid)
- 0 orphan `accounts` (all site_ids valid)
- 0 orphan `account_tokens` (all account_ids valid)
- 0 orphan `route_channels` (all route_ids, account_ids valid)
- 0 orphan `oauth_route_unit_members` (all unit_ids valid)
- 0 orphan `proxy_debug_attempts` (all trace_ids valid)
- 0 orphan `site_day_usage` (all site_ids valid)
- 0 orphan `site_announcements` (all site_ids valid)

---

## Edge Cases Tested

### Empty database roundtrip

Exporting an empty database (27 tables with 0 rows each) succeeds:
- Export returns all 27 table keys with `[]` empty arrays
- Factory reset succeeds (DELETE from all tables with 0 rows)
- Import succeeds (no rows to import, all counts = 0)

### Malformed import rejection

Sending `{"not_tables": "wrong"}` to the import endpoint returns 400 with message "导入数据格式错误".

### Invalid export type rejection

Requesting `?type=invalid` returns 400 with message "导出类型无效，仅支持 all/accounts/preferences".

### Type filter correctness

- `?type=accounts`: exports 17 tables (sites, site_api_endpoints, site_disabled_models, accounts, account_tokens, checkin_logs, model_availability, token_model_availability, token_routes, route_group_sources, oauth_route_units, oauth_route_unit_members, route_channels, proxy_video_tasks, proxy_files, downstream_api_keys, site_announcements) -- excludes proxy_logs, proxy_debug_traces, proxy_debug_attempts, settings, admin_snapshots, analytics_projection_checkpoints, site_day_usage, site_hour_usage, model_day_usage, events
- `?type=preferences`: exports only `settings` table

### JSON field fidelity

Fields that store JSON arrays/objects as TEXT survive the roundtrip with exact character fidelity:
- `downstream_api_keys.supported_models`: `["gpt-4","gpt-3.5","claude-3-opus"]`
- `sites.custom_headers`: `{"X-Custom":"value"}`

### SET NULL foreign key

`route_channels.token_id` uses `ON DELETE SET NULL` (not CASCADE). After roundtrip, the token_id value is preserved correctly (verified: `token_id = 1` for route_channel id=1).

---

## Schema Quirks Discovered

During test development, a schema inconsistency was identified:

| Table | Has `created_at`? | Has `updated_at`? |
|:------|:-----------------:|:-----------------:|
| route_channels | NO | NO |
| route_group_sources | NO | NO |
| settings | NO (text PK) | NO |
| analytics_projection_checkpoints | NO (text PK) | YES |
| site_announcements | NO | NO |
| model_availability | NO (checked_at) | NO |
| token_model_availability | NO (checked_at) | NO |
| events | YES | NO |
| checkin_logs | YES | NO |
| proxy_debug_attempts | YES | NO |
| proxy_logs | YES | NO |

**Notable**: `route_channels` has no timestamp columns at all, making it impossible to track when channels were created. `route_group_sources` similarly lacks timestamps. These are schema-level issues outside the scope of the backup roundtrip test but worth tracking.

---

## Conclusion

**All 8 tests pass.** The backup export, factory reset, and import pipeline correctly handles all 27 tables through a full roundtrip cycle:

1. Export captures all tables with correct row counts and data
2. Factory reset truncates all tables and resets sequences with proper FK ordering
3. Import restores all data with preserved IDs, values, and FK relationships
4. Re-export after import produces structurally identical output
5. Edge cases (empty DB, invalid types, malformed JSON) are handled with appropriate error codes

The prior audit (`audit-backup.md`) finding that export/import/factory-reset were all stubs has been fully resolved.

**Remaining gaps** (not covered by this test, tracked in `audit-backup.md`):
- WebDAV upload scheduler (P1) -- retry logic, config persistence wiring
- `proxy_files.content_base64` size guard for large binary data
- `clearCache` table scope review (P2)
- Missing timestamps on `route_channels` and `route_group_sources` (schema design)
