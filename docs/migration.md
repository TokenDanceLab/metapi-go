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

## Troubleshooting

### "web/dist: no matching files found" at build time

Build the frontend first: `make web-build`

### "target database already contains data"

Use `--overwrite` to clear and replace existing data.

### Foreign key violations during migration

Ensure you are using `--overwrite` (default). The migration tool deletes data in FK-safe order before inserting.

### Server exits with "AUTH_TOKEN is required"

Set the `AUTH_TOKEN` environment variable. Both `AUTH_TOKEN` and `PROXY_TOKEN` are required.
