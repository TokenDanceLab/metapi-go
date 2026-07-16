# Migration Guide: TypeScript to Go

This guide walks through migrating from the TypeScript MetAPI server to the Go rewrite.

## Overview

The migration involves:
1. Stopping the old TS server
2. Transferring SQLite data to PostgreSQL (optional, if switching to PG)
3. Starting the Go binary
4. Verifying functionality

## Prerequisites

- Go 1.26.4+ installed
- Built `metapi-migrate` binary: `make migrate-build`
- PostgreSQL 16+ running (if migrating to PG)
- Backed up your existing SQLite database

## Step 1: Stop the TS Server

```bash
# Stop the old server (method depends on your deployment)
docker compose down        # if using Docker
# or
systemctl stop metapi      # if using systemd
# or
kill $(pgrep -f "node.*metapi")
```

The server must be stopped to ensure a consistent database snapshot.

## Step 2: Back Up Your Data

```bash
# Backup SQLite database
cp data/hub.db "backups/hub-pre-migration-$(date +%Y%m%d-%H%M%S).db"

# Backup other important files
cp .env "backups/env-pre-migration-$(date +%Y%m%d-%H%M%S)"
```

## Step 3: Migrate Data (SQLite to PostgreSQL)

If you are staying with SQLite, skip to Step 4.

```bash
# Dry-run first to see what will be transferred
./metapi-migrate \
  --from sqlite://data/hub.db \
  --to 'postgres://<user>:<password>@<host>:5432/metapi?sslmode=require' \
  --dry-run

# Run actual migration with progress and verification
./metapi-migrate \
  --from sqlite://data/hub.db \
  --to 'postgres://<user>:<password>@<host>:5432/metapi?sslmode=require' \
  --overwrite \
  --progress \
  --verify
```

Expected output:
```
Reading source SQLite database...
  sites:                     5 rows
  site_api_endpoints:       12 rows
  ...
  settings:                  8 rows

Clearing target data (FK-safe order)...

Inserting 1234 rows...
  100/1234 rows inserted (2.3s elapsed)
  200/1234 rows inserted (4.1s elapsed)
  ...
  Done: 1234 rows in 12.5s

Syncing PostgreSQL sequences...

Verifying checksums...
  All checksums match.

Migration Summary:
  dialect:    postgres
  connection: postgres://<user>:***@<host>:5432/metapi
  overwrite:  true
  version:    live-db-snapshot
  timestamp:  1720000000000
  rows:
    sites:                  5
    ...
```

### Migration flags

| Flag | Description |
|------|-------------|
| `--from` | Source SQLite path (`sqlite://path` or plain path) |
| `--to` | Target PostgreSQL connection string |
| `--overwrite` | Clear target data before inserting (default: true) |
| `--dry-run` | Print migration plan without writing data |
| `--progress` | Show per-table progress during transfer |
| `--verify` | Compute row-count and checksum verification |
| `--batch-size N` | Rows per multi-row INSERT (default: 1, row-by-row) |

### What gets migrated

18 tables are transferred with full type coercion:

- sites, site_api_endpoints, site_announcements, site_disabled_models
- accounts, account_tokens
- checkin_logs, model_availability, token_model_availability
- token_routes, route_channels, route_group_sources
- proxy_logs, proxy_video_tasks, proxy_files
- downstream_api_keys, events, settings

System settings `db_type`, `db_url`, `db_ssl` are automatically filtered (they control the database connection and should not be copied).

## Step 4: Start the Go Server

### With SQLite (same data)

```bash
export AUTH_TOKEN=<your-token>
export PROXY_TOKEN=<your-proxy-token>
export DATA_DIR=./data
./metapi
```

### With PostgreSQL

```bash
export AUTH_TOKEN=<your-token>
export PROXY_TOKEN=<your-proxy-token>
export DATABASE_URL='postgres://<user>:<password>@<host>:5432/metapi?sslmode=require'
./metapi
```

### With Docker (production)

```bash
# Edit .env with your tokens
docker compose -f docker-compose.prod.yml up -d
```

The Go server auto-runs DDL migrations at startup.

## Step 5: Verify

### Health and readiness checks

```bash
curl http://localhost:4000/health
# {"status":"ok"}

curl http://localhost:4000/ready
# {"status":"ok","database":"ok"}
```

### Frontend

Open `http://localhost:4000` in a browser. The React SPA should load.

### API check

```bash
curl -H "Authorization: Bearer $AUTH_TOKEN" http://localhost:4000/api/stats/dashboard
```

### Data integrity

- Verify site and account counts match the old server
- Confirm route configurations are intact
- Check that downstream API keys work
- Test a proxy request through the new server

## Rollback Plan

If the Go server has issues:

```bash
# Stop Go server
kill $(pgrep metapi)

# Restore old TS server
cd /path/to/metapi-ts
docker compose up -d

# If you migrated to PG and want to revert to SQLite:
# The SQLite file was not modified (the migration tool reads only).
# Simply restart the TS server with the original SQLite database.
```

The SQLite database is never modified by the migration tool (it only reads). You can always fall back to the TS server with the original SQLite file.

## Key Differences

| Feature | TypeScript | Go |
|---------|-----------|-----|
| Startup time | ~5-10s (Node.js JIT) | ~100ms |
| Memory usage | ~150MB+ (Node.js baseline) | ~20-30MB |
| MySQL support | Yes | No (SQLite + PG only) |
| Frontend serving | Separate volume / Express static | Embedded in binary |
| Migration tool | Integrated in server | Standalone binary |
| Container image | ~80MB+ | <25MB |
| CI/CD | Manual / CD-only | Full CI (lint + test + build) |

## Additive enterprise upgrades

**Issue:** [SC1 #21](https://github.com/TokenDanceLab/metapi-go/issues/21)  
**Depends on:** [SC0 #20](https://github.com/TokenDanceLab/metapi-go/issues/20) (`docs/analysis/schema-parity.md`)  
**Implements product columns:** [SC2 #22](https://github.com/TokenDanceLab/metapi-go/issues/22)

This section is the operator-facing contract for **forward-only schema upgrades**
on existing SQLite and PostgreSQL installs. It complements the one-shot
TS→Go / SQLite→PG data transfer tool above.

### Why CREATE TABLE IF NOT EXISTS is not enough

Startup already runs `store.AutoMigrate`:

1. **Base bootstrap** — `CREATE TABLE IF NOT EXISTS` for all 27 product tables +
   `CREATE INDEX IF NOT EXISTS` for non-unique indexes.
2. **Additive upgrades** — ordered steps recorded in `schema_migrations`.

`CREATE TABLE IF NOT EXISTS` only creates **missing tables**. It never adds
columns to a table that already exists. Enterprise upgrades such as:

| Column | Table | Default / meaning |
|:-------|:------|:------------------|
| `proxy_url` | `downstream_api_keys` | `NULL` → fall back to site / system proxy |
| `max_concurrency` | `sites` | `NULL` or `0` → unlimited (current behavior) |
| `context_length` | `token_routes` (or catalog) | `NULL` → unknown / no enforcement |

must use **`ALTER TABLE … ADD COLUMN`** (or new tables) on live databases. SC1
ships the dual-dialect machinery; SC2 registers the concrete steps.

### Versioning model

Bookkeeping table (created automatically on every startup):

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version     TEXT PRIMARY KEY,
  applied_at  TEXT NOT NULL,   -- ISO-8601 UTC, app-filled
  description TEXT
);
```

| Rule | Detail |
|:-----|:-------|
| Version ID | Stable **string** primary key (e.g. `sc2_001_downstream_proxy_url`), not a dense integer sequence |
| Ordering | Registry order in `store/additive.go` (`enterpriseAdditiveSteps`); append-only |
| Applied set | `SELECT version FROM schema_migrations`; skip if already present |
| Fresh install | Base DDL creates full current tables; additive steps that only `EnsureColumn` become no-ops if the column is already in CREATE TABLE, then still get a bookkeeping row when registered |
| Old install | Missing columns are added; defaults preserve pre-upgrade behavior |
| Concurrency | `INSERT OR IGNORE` (SQLite) / `ON CONFLICT DO NOTHING` (PG) when marking applied |

**Do not** renumber, rename, or delete a shipped `version` string. New work is
always a new ID appended to the registry.

### How new columns get defaults

Additive columns must leave **old rows and old clients** behaving as today:

1. Prefer **nullable** columns with application-level fallback  
   (`NULL` = “feature off / use previous resolver path”).
2. Or **`DEFAULT`** values that encode the historical unlimited / empty behavior  
   (e.g. `max_concurrency DEFAULT 0` meaning unlimited).
3. Never add `NOT NULL` without a default on an existing table (blocks upgrade).
4. Never change type, rename, or drop columns in an additive step.
5. Dual-dialect type fragments:
   - booleans: SQLite `INTEGER` + `DEFAULT 0/1`; PG `BOOLEAN` + `DEFAULT FALSE/TRUE`
   - floats: SQLite `REAL`; PG **`DOUBLE PRECISION`** (never bare PG `REAL`)
   - JSON: always `TEXT` (app marshal), not PG `JSONB`
   - datetimes: `TEXT` ISO-8601 filled by the app

Primitive used by SC2 steps:

```go
// store.EnsureColumn — inspects then ALTER TABLE ADD COLUMN if missing
EnsureColumn(db, "downstream_api_keys", "proxy_url", "TEXT", "TEXT", "")
EnsureColumn(db, "sites", "max_concurrency", "INTEGER", "INTEGER", "DEFAULT 0")
```

`columnExists` uses `PRAGMA table_info` on SQLite and
`information_schema.columns` on PostgreSQL.

### Dual-dialect notes

| Concern | SQLite | PostgreSQL |
|:--------|:-------|:-----------|
| Base bootstrap | `INTEGER PRIMARY KEY AUTOINCREMENT`, bool as INTEGER | `SERIAL PRIMARY KEY`, native BOOLEAN, DOUBLE PRECISION |
| Additive DDL | `ALTER TABLE … ADD COLUMN` | Same (PG also supports `IF NOT EXISTS` on newer versions; we use inspect-then-add for both) |
| Index create | `CREATE INDEX IF NOT EXISTS` | Same |
| Bookkeeping insert | `INSERT OR IGNORE` | `ON CONFLICT (version) DO NOTHING` |
| Placeholders | `?` | `?` rebound to `$N` via `store.DB` |
| Startup path | `EnsureRuntimeDatabase` → `AutoMigrate` → `ApplyAdditiveMigrations` | Same |

The standalone `metapi-migrate` binary (**SQLite → PostgreSQL data copy**) is
orthogonal: it transfers rows after the target schema exists. Additive upgrades
run inside the **server** process on whatever dialect is configured. After a
SQLite→PG transfer, start the Go server once so base + additive migrations run
on the PG target (or rely on the migrator’s minimal `ensureTargetSchema` plus a
server boot).

### Safe upgrade path for existing deployments

1. **Back up** the database (SQLite file copy or `pg_dump`).
2. Deploy the new binary (or image). No separate migrate CLI is required for
   additive column upgrades.
3. On first start, logs should include:
   - `store: running auto-migration`
   - `store: applying additive migration` (only for pending versions)
   - `store: auto-migration complete`
4. Verify with health/ready endpoints and a spot-check of row counts.
5. Optional: inspect bookkeeping  
   `SELECT version, applied_at, description FROM schema_migrations ORDER BY applied_at;`

Failed steps **do not** write a `schema_migrations` row. The next process start
retries the same version. Steps should keep DDL idempotent (`EnsureColumn`) so a
crash between `ALTER TABLE` and the bookkeeping insert recovers cleanly.

### Rollback philosophy

Additive migrations are **forward-only**:

| Policy | Rationale |
|:-------|:----------|
| No automatic `DROP COLUMN` / down migrations | Dual-dialect drop semantics differ; dropping data is operator-dangerous |
| Binary rollback | Older binaries ignore unknown columns; keep defaults so old code paths remain valid |
| True rollback of data | Restore from pre-upgrade backup (SQLite file or `pg_dump`) |
| Failed deploy | Fix forward (new additive step or hotfix); do not rewrite history in `schema_migrations` |
| Column retirement (rare) | Stop writing the column in app code first; physical drop is a later, explicit ops task outside SC1 |

**Compatibility invariant:** every additive column’s default / NULL semantics
must preserve pre-upgrade behavior until a feature flag or UI explicitly opts in.

### Code map (SC1)

| Path | Role |
|:-----|:-----|
| `store/migrate.go` | Base 27-table DDL; `AutoMigrate` calls `ApplyAdditiveMigrations` |
| `store/additive.go` | `schema_migrations`, `ApplyAdditiveMigrations`, `EnsureColumn`, `columnExists` |
| `store/additive_test.go` | SQLite unit tests + optional PG (`PG_TEST_DSN`) dual-dialect smoke |
| `docs/analysis/schema-parity.md` §5 | Product column proposals for SC2 |
| `cmd/migrate/` | **Not** the additive engine — SQLite→PG **data** transfer only |

### SC2 registration checklist

When implementing enterprise columns:

1. Add / update Go structs in `store/schema.go` and base `CREATE TABLE` builders
   so **new** installs get the column from bootstrap.
2. Append an `AdditiveStep` with a new `version` that calls `EnsureColumn` (and
   optional `EnsureIndex`) so **old** installs converge.
3. Keep defaults compatible; wire feature code to treat NULL/0 as “legacy”.
4. Extend dual-dialect tests; run `go test ./store/ -count=1` (and PG if available).

## Troubleshooting

### "web/dist: no matching files found" at build time

Build the frontend first: `make web-build`

### "target database already contains data"

Use `--overwrite` to clear and replace existing data.

### Foreign key violations during migration

Ensure you are using `--overwrite` (default). The migration tool deletes data in FK-safe order before inserting.

### Server exits with "AUTH_TOKEN is required"

Set the `AUTH_TOKEN` environment variable. Both `AUTH_TOKEN` and `PROXY_TOKEN` are required.

### Additive migration fails on startup

1. Read the log line `store: additive migration <version>: …` for the failing step.
2. Confirm disk / DB permissions and that the dialect matches `DB_TYPE`.
3. After fixing, restart — pending versions retry automatically.
4. Do not manually delete rows from `schema_migrations` unless recovering from a
   partial operator experiment; prefer re-running with idempotent steps.
