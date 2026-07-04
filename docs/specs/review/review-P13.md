# P13 Cross-Reference Review: Go Spec vs TypeScript Source

**Review date**: 2026-07-04
**Spec**: `D:/Code/TokenDance/metapi-go/docs/specs/p13-embed-ci.md`
**TS sources reviewed**:
- `D:/Code/TokenDance/metapi/Dockerfile.slim`
- `D:/Code/TokenDance/metapi/docker/docker-compose.yml`
- `D:/Code/TokenDance/metapi/.github/workflows/cd-slim.yml`
- `D:/Code/TokenDance/metapi/.github/workflows/backfill-pr-labels.yml`
- `D:/Code/TokenDance/metapi/.github/workflows/labeler.yml`
- `D:/Code/TokenDance/metapi/src/server/services/databaseMigrationService.ts`

---

## Accuracy Issues

### A1. Table count: spec claims 27, TS migrates 18

Spec line 64: "读取 SQLite 的所有 27 张表数据"

The TS `databaseMigrationService.ts` `toBackupSnapshot()` function reads exactly 18 distinct tables:
1. `sites`
2. `site_api_endpoints`
3. `site_announcements`
4. `site_disabled_models`
5. `accounts`
6. `account_tokens`
7. `checkin_logs`
8. `model_availability`
9. `token_model_availability`
10. `token_routes`
11. `route_channels`
12. `route_group_sources`
13. `proxy_logs`
14. `proxy_video_tasks`
15. `proxy_files`
16. `downstream_api_keys`
17. `events`
18. `settings`

There may be 27 tables in the full schema (e.g., migration tracking tables, drizzle meta tables), but the migration service only transfers 18. Either correct the count to 18, or document that the remaining 9 are system/meta tables not part of data migration.

### A2. Batch insert claim -- TS does row-by-row, not batch

Spec line 65: "批量插入 PG (事务包装, 每 1000 行一批)"

The TS `insertAllRows()` function (lines 738-743) inserts each row with an individual `INSERT` statement. There is no batching of any kind -- no chunking, no multi-row VALUES clause, no `COPY`. Every single row gets its own round-trip.

If the Go version introduces 1000-row batching, this should be called out explicitly as a new Go enhancement, not presented as if it reflects the existing TS behavior.

### A3. Progress bar and checksum verification -- not in TS

Spec line 68: "进度条 + 校验和验证"

The TS `databaseMigrationService.ts` has neither a progress bar nor a checksum verification step. The only "verification" is the row counts returned in the `DatabaseMigrationSummary`. These are new Go features and should be labelled as such.

### A4. Dry-run mode -- not in TS

Spec line 69: "Dry-run 模式"

The TS migration service has no dry-run mode. `migrateCurrentDatabase()` always executes the full migration (normalize, snapshot, ensure schema, clear, insert, sync sequences, commit). A dry-run flag would be a new Go addition.

### A5. CI: TS has no `ci.yml` at all

Spec lines 73-80 describe a `ci.yml` with lint, test (SQLite), test-pg (testcontainers), and build jobs. No such file exists in the TS `.github/workflows/` directory. The TS repo has:
- `cd-slim.yml` -- Docker build and push (CD, not CI)
- `backfill-pr-labels.yml` -- PR label backfill utility
- `labeler.yml` -- automatic PR labeling

The spec's `ci.yml` is entirely a new Go addition. This should be stated explicitly. The event triggers (`on: [push, pull_request]`), the `test-pg` job with testcontainers, and `golangci-lint` have no TS precedent.

### A6. CD trigger mismatch: tags vs main branch

Spec line 86: `cd.yml` triggers `on: push: tags: ['v*']`

The TS `cd-slim.yml` triggers on `push: branches: [main]` (plus `workflow_dispatch` and path filters on `src/**`, `drizzle/**`, etc.). It does NOT trigger on tags. The tag-based `v*` trigger is a divergence from the TS pattern. If intentional, it should be documented as a change.

### A7. GHCR image name suffix: `metapi` vs `metapi-go`

Spec line 91: `ghcr.io/tokendancelab/metapi-go`

TS `cd-slim.yml` line 17: `ghcr.io/tokendancelab/metapi` (no `-go` suffix).

The `-go` suffix is intentional (Go rewrite) but this is a registry namespace decision, not a spec accuracy problem. Still worth noting the divergence.

### A8. CD: no path-filter, no workflow_dispatch, no cache-from/cache-to

The TS `cd-slim.yml` includes:
- Path filters (`paths:` on `src/**`, `drizzle/**`, `package.json`, etc.) -- spec cd.yml has none
- `workflow_dispatch:` manual trigger -- spec cd.yml has none
- `cache-from: type=gha` / `cache-to: type=gha,mode=max` -- spec cd.yml has none
- Tag strategy: TS uses `latest` + `YYYYMMDD` + `sha-short`; spec uses `latest` + `vX.Y.Z`

These omissions may be intentional simplifications for the Go rewrite, but should be acknowledged.

### A9. HEALTHCHECK and VOLUME -- not in TS Dockerfile

Spec Dockerfile lines 119-120: `HEALTHCHECK` and `VOLUME ["/app/data"]`

The TS `Dockerfile.slim` has neither. These are new additions for the Go version. The spec presents them without noting the delta.

---

## Missing Details

### M1. MySQL dialect support omitted

The TS migration service supports three dialects: `sqlite`, `mysql`, `postgres` (line 90: `const DIALECTS: MigrationDialect[] = ['sqlite', 'mysql', 'postgres']`).

The spec's migration tool only describes SQLite-to-PG (`--from sqlite://... --to postgres://...`). If MySQL is intentionally dropped from the Go version, this should be stated explicitly. If kept, the migration tool must handle MySQL identifier quoting (backticks, line 236-238 of TS) and MySQL-specific connection string validation (line 171-173).

### M2. `ensureRuntimeDatabaseSchema` step missing from migration flow

The TS `migrateCurrentDatabase()` calls `ensureRuntimeDatabaseSchema(client)` (line 791) BEFORE data insertion. This creates all tables in the target database if they don't exist. The spec mentions neither this step nor the runtime schema bootstrap concept.

### M3. `clearTargetData` step in overwrite mode

TS lines 291-315: When `overwrite` is true, all existing data is deleted from the target in reverse dependency order (FK-safe: route_channels first, settings last). The spec says "PG 已有数据 → 默认 skip, 需要 `--overwrite` 标志" but doesn't describe the deletion order or the 18-table clear loop.

### M4. JSON column serialization logic

The TS has specialized handling for columns with `logicalType === 'json'` via `serializeColumnValue()` and `serializeJsonColumnValue()` (lines 136-152). JSON columns like `custom_headers`, `extra_config`, `model_mapping`, `decision_snapshot`, `billing_details`, `status_snapshot`, `upstream_response_meta`, `supported_models`, `allowed_route_ids`, `site_weight_multipliers`, `excluded_site_ids`, `excluded_credential_refs` are all serialized specially. The spec mentions only "类型转换 (INTEGER 0/1 → BOOLEAN, TEXT datetime → TEXT)" -- no mention of JSON column handling.

### M5. Runtime database setting key filtering

TS lines 90 and 709: Settings rows with keys `db_type`, `db_url`, `db_ssl` (`RUNTIME_DATABASE_SETTING_KEYS`) are explicitly skipped during migration. These settings control the target database connection itself and would be wrong if copied. The spec does not mention this filtering.

### M6. SQLite dialect URL normalization

TS `normalizeSqliteTarget()` (lines 176-197) handles three SQLite URL formats:
- `:memory:` (passthrough)
- `file:///path/to/db` → extracts pathname from URL
- `sqlite://path/to/db` → strips prefix
- Plain path → uses as-is
- Network URL guard: rejects `http://...`, `postgres://...` etc. when dialect is `sqlite`

The spec's CLI example `--from sqlite://data/hub.db` only shows one format. The normalization logic is not described.

### M7. Connection string validation for non-SQLite dialects

TS `assertDialectUrl()` (lines 158-174) validates:
- PostgreSQL URLs must start with `postgres:` or `postgresql:`
- MySQL URLs must start with `mysql:`
- Invalid URLs throw descriptive errors

The spec does not document input validation.

### M8. Per-column type coercion with default values

The TS `buildStatements()` function (lines 317-719) does meticulous per-column coercions:
- Numeric columns: `asNumber(row.x, defaultFallback)` with column-specific fallbacks (0, null, 1, 10)
- Boolean columns: `asBoolean(row.x, fallback)` with column-specific defaults
- String columns: `asNullableString(row.x)` with null handling
- Status defaults: `row.status ?? 'active'`, `row.level ?? 'info'`

The spec's summary "处理类型转换" is far too vague. The Go implementation needs equivalent per-column type coercion with the same fallback values.

### M9. Direct-row migration vs live-db snapshot

The TS reads from the **currently running live database** (via Drizzle ORM `db.select().from(schema.X).all()`), not from a SQLite file. The spec assumes the source is always SQLite. If the Go version only supports SQLite-to-PG, that's a scope reduction that should be called out.

### M10. Transaction boundary

TS wraps the entire migration (clear + insert + sync sequences) in a single transaction (`client.begin()` at line 794). On any error, `client.rollback()` is called (line 803). The spec says "事务包装" but doesn't specify that it covers the entire clear+insert+sync block, nor that errors trigger rollback.

### M11. Dockerfile: no `mkdir -p /app/data`

The TS Dockerfile.slim explicitly creates the data directory (`RUN mkdir -p /app/data`, line 28). The spec Dockerfile has `VOLUME ["/app/data"]` but no `RUN mkdir -p` step. VOLUME creates the mount point but if the directory doesn't exist at build time, the behavior depends on the runtime. For safety, the spec should include the mkdir step.

### M12. Dockerfile: no npm prune or node_modules hygiene

The TS Dockerfile has `npm prune --omit=dev` (line 16) to strip devDependencies from the runtime image. The Go Dockerfile doesn't need this (Go produces a static binary), but the spec doesn't explain why this step is gone.

### M13. docker-compose.yml missing auth/env configuration

The TS `docker-compose.yml` specifies critical environment variables:
- `AUTH_TOKEN` (required, validated with `${AUTH_TOKEN:?}`)
- `PROXY_TOKEN` (required, validated with `${PROXY_TOKEN:?}`)
- `CHECKIN_CRON`, `BALANCE_REFRESH_CRON`, `PORT`, `DATA_DIR`, `TZ`

The spec template for `docker-compose.yml` is skeletal (just a single line "开发/生产 compose" in the module structure diagram). It should reference the full env var set from the TS compose file.

---

## Edge Cases Not Covered

### E1. Empty source database -- all tables have 0 rows

TS handles this implicitly: `buildStatements()` produces an empty array, `insertAllRows()` iterates zero times, migration succeeds with row counts of 0.

Spec line 156 mentions this for "SQLite 表为空" generically but doesn't confirm per-table empty behavior (some tables may have rows while others are empty -- that's the common real-world case, not "all tables empty").

### E2. Partial FK data (orphaned references)

For example: an `account_tokens` row referencing a non-existent `account_id`, or a `route_channels` row with a deleted `route_id`. The TS migration copies data as-is -- it does not validate FK integrity. If the Go version introduces FK constraints that the SQLite source didn't enforce, this could cause insertion failures.

### E3. Table clear order and FK cycles

TS `clearTargetData()` has a specific deletion order (lines 292-311). The order is carefully chosen to avoid FK violations: children before parents. If the Go version changes or reorders the table list, it could hit FK errors on DELETE.

### E4. Settings table -- idempotency across migrations

The TS settings migration filters out `RUNTIME_DATABASE_SETTING_KEYS`. But it also does NOT include an `id` column in the settings INSERT (line 714: `columns: ['key', 'value']`). If the settings table has an auto-increment `id`, repeated migrations with `--overwrite` could accumulate duplicate keys (since DELETE + INSERT with new IDs). The spec doesn't address this.

### E5. Large binary content (proxy_files.content_base64)

The `proxy_files` table has a `content_base64` column that can hold large base64-encoded file content. The spec makes no mention of memory limits, streaming, or chunked handling for large rows. The TS reads everything into memory via `toBackupSnapshot()`.

### E6. Concurrent access during migration

The TS migration reads a live snapshot under the assumption that the source DB is quiescent. No locking or read-consistency mechanism is described. Same for the Go version -- the spec says "停服 → dump → 切换 binary → 验证" in the migration docs (line 132), but this "stop service" step is documented only in `docs/migration.md`, not in the migration tool spec itself.

### E7. Docker: migration on startup

The TS Dockerfile runs `node dist/server/db/migrate.js` before starting the server (line 34: `CMD ["sh", "-c", "node dist/server/db/migrate.js && node dist/server/index.js"]`). The spec Dockerfile has no equivalent -- `CMD ["metapi"]` assumes the binary handles its own migration internally, or that migration is a separate step. The acceptance criteria say "data 目录为空 → 自动创建 + migration" (line 157) but the Dockerfile doesn't show how this auto-migration is triggered.

### E8. Docker image size claim (<25MB)

The spec claims the Docker image will be under 25MB including the frontend. A Go binary with embedded React SPA (web/dist with all assets) plus an Alpine base (~7MB) could plausibly fit, but no baseline is given. The TS `node:25-alpine` base alone is ~50MB. The claim should be marked as aspirational until verified.

### E9. Multi-arch: spec says amd64 only; TS Dockerfile says amd64 + arm64

Spec line 90: "Build multi-arch (amd64) Docker image"

TS `Dockerfile.slim` line 3: "Multi-arch: linux/amd64, linux/arm64"

The spec's parenthetical "(amd64)" suggests single-arch. The TS comment claims dual-arch (though no `--platform` argument is present in the workflow). The Go spec should clarify whether arm64 is in scope.

### E10. npm install dependencies for web build

The spec Dockerfile uses `npm ci` in the web build stage. The TS Dockerfile.slim requires `apk add --no-cache python3 make g++` (line 8) and `npm rebuild esbuild sharp better-sqlite3` (line 12). For a Go version that only builds the React frontend (not the server), `better-sqlite3` and `sharp` native rebuilds are unnecessary. The spec correctly omits these -- but the reviewer should verify that the `web/` subdirectory's `package.json` doesn't depend on native modules that would fail without build tools.

---

## Incorrect Details

### I1. Table count: "27" should be "18" (or explicitly list which 27)

As documented in A1. The migration service transfers 18 tables, not 27.

### I2. CI trigger model: spec implies parity with TS CI, but TS has no CI workflow

The spec's CI section (lines 72-81) is presented as if it's the Go equivalent of existing TS CI infrastructure. In reality, the TS repo has **zero** CI workflows. The two non-labeler workflows are a CD pipeline (`cd-slim.yml`) and a label backfill utility. The spec should clearly state that CI (lint, test, build) is entirely new for the Go version.

### I3. CD trigger: spec says tags, TS says main branch

Spec line 86: `tags: ['v*']` -- TS triggers on `branches: [main]`. If the Go project wants tag-based releases, that's a deliberate change. But the spec doesn't flag it as a change.

### I4. CD image: `metapi-go` vs `metapi`

Spec line 91: `ghcr.io/tokendancelab/metapi-go` -- TS uses `ghcr.io/tokendancelab/metapi`. The `-go` suffix is new. Should be acknowledged as intentional divergence.

---

## Summary

| Category | Count |
|----------|-------|
| Accuracy Issues | 9 |
| Missing Details | 13 |
| Edge Cases Not Covered | 10 |
| Incorrect Details | 4 |

**Verdict: NEEDS_REVISION**

The spec captures the high-level intent correctly but contains multiple factual errors about the TS baseline. The table count (27 vs 18), the CI/CD trigger model, and several "missing but assumed present" features (batching, progress bar, dry-run, checksum) inflate the TS capabilities and set unrealistic expectations for parity. The migration tool section in particular needs a detailed line-by-line reconciliation against `databaseMigrationService.ts` to ensure all per-column coercions, JSON serialization, runtime key filtering, and FK-safe deletion order are carried forward.

Key fixes required before approval:
1. Correct table count from 27 to 18 (or list all 27 tables explicitly if including system tables)
2. Mark batch insert, progress bar, checksum, and dry-run as **new Go features**
3. Clarify that `ci.yml` is entirely new (no TS precedent)
4. Decide and document whether CD triggers on tags or main branch
5. Add sections on JSON column serialization, runtime key filtering, and per-column type coercion defaults
6. Document clearTargetData deletion order for FK safety
7. Add MySQL dialect support decision (kept or dropped)
8. Clarify Docker auto-migration mechanism on startup
