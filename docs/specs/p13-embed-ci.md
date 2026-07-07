# P13: Frontend Embed + SQLite-to-PG Migration Tool + CI/CD + Docs

**S.U.P.E.R**: E (environment-agnostic) · R (replaceable) | **Depends on**: P0-P12 (all) | **Size**: M

## Original TS Reference

- `<metapi-ts>\vite.config.ts` -- frontend build config
- `<metapi-ts>\Dockerfile.slim` -- existing Docker deployment
- `<metapi-ts>\docker\docker-compose.yml`
- `<metapi-ts>\src\server\services\databaseMigrationService.ts` -- cross-dialect migration
- `<metapi-ts>\.github\workflows\cd-slim.yml` -- CD workflow (TS has no CI)

## Go Module Structure

```
cmd/server/main.go        # + embed frontend static files
cmd/migrate/main.go       # standalone migration tool: SQLite -> PG
web/                      # (existing) React SPA source
web/dist/                 # (build artifact, gitignored) embed target
.github/workflows/
  ci.yml                  # Go lint + test + build (SQLite + PG matrix) [NEW - TS has no CI]
  cd.yml                  # Docker build + push to ghcr.io
Dockerfile                # multi-stage: frontend build + go build -> alpine
docker-compose.yml        # development/production compose
docs/
  deployment.md           # deployment guide
  architecture.md         # Go version architecture overview
  api.md                  # API reference (migrated from TS frontend)
  migration.md            # TS -> Go migration guide
```

## 1. Frontend Embed

### 1.1 Go embed with SPA fallback

```go
//go:embed web/dist
var webFS embed.FS

// chi router
r.NotFound(func(w http.ResponseWriter, r *http.Request) {
    // Exclude API paths from SPA fallback
    if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
        http.NotFound(w, r)
        return
    }
    // SPA fallback: serve index.html for all non-API, non-asset routes
    data, err := webFS.ReadFile("web/dist/index.html")
    if err != nil {
        http.NotFound(w, r)
        return
    }
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write(data)
})

// Static assets with long-lived cache (fingerprinted by Vite)
r.Handle("/assets/*", http.FileServer(http.FS(webFS)))
```

### 1.2 Asset caching headers

Since Vite fingerprints all output assets (JS, CSS, images) with content hashes, set aggressive cache headers for the `/assets/*` path:

```
Cache-Control: public, max-age=31536000, immutable
```

The `index.html` itself must not be cached (it references the fingerprinted assets):

```
Cache-Control: no-cache
```

### 1.3 Build prerequisite

If `web/dist/` does not exist at Go build time, `go build` will fail with an embed error. The build process must run `npm run build:web` (in the `web/` directory) before `go build`. The multi-stage Dockerfile handles this; local development should document the prerequisite.

### 1.4 TS divergence note

The TS version serves the frontend via Express `express.static()`. The Go version replaces this with `embed.FS`. Embedding eliminates the need for a separate static file volume or an external web server.

---

## 2. SQLite-to-PG Migration Tool

### 2.1 CLI interface

```bash
# Build standalone binary
go build -o metapi-migrate ./cmd/migrate

# Basic usage
metapi-migrate --from sqlite://data/hub.db --to 'postgres://<user>:<password>@<host>:5432/db?sslmode=require'

# With overwrite (default: true, matching TS behavior)
metapi-migrate --from sqlite://data/hub.db --to 'postgres://<user>:<password>@<host>:5432/db?sslmode=require' --overwrite

# Dry-run mode [NEW Go feature - not in TS]
metapi-migrate --from sqlite://data/hub.db --to 'postgres://<user>:<password>@<host>:5432/db?sslmode=require' --dry-run

# Show progress bar [NEW Go feature - not in TS]
metapi-migrate --from sqlite://data/hub.db --to 'postgres://<user>:<password>@<host>:5432/db?sslmode=require' --progress

# Checksum verification [NEW Go feature - not in TS]
metapi-migrate --from sqlite://data/hub.db --to 'postgres://<user>:<password>@<host>:5432/db?sslmode=require' --verify
```

### 2.2 Dialect support

The TS migration service supports three dialects: `sqlite`, `mysql`, `postgres`. For the Go version, **only SQLite-to-PostgreSQL is implemented**. MySQL support is dropped intentionally -- the Go project targets SQLite (local dev) and PostgreSQL (production).

### 2.3 Migration flow (step by step)

This is the exact migration sequence, matching the TS `migrateCurrentDatabase()`:

```
1. normalizeMigrationInput()     -- validate and normalize CLI args
2. toBackupSnapshot()            -- read all 18 tables from source SQLite into memory
3. createClient()                -- open target PG connection
4. ensureRuntimeDatabaseSchema() -- CREATE TABLE IF NOT EXISTS in target (if missing)
5. ensureTargetState()           -- check if target has data; reject unless --overwrite
6. BEGIN TRANSACTION
7.   clearTargetData()           -- DELETE FROM all 18 tables in FK-safe order (if overwrite)
8.   insertAllRows()             -- INSERT every row, one statement per row
9.   syncPostgresSequences()     -- SETVAL for 17 auto-increment sequences
10.  COMMIT
11. ON ERROR: ROLLBACK
12. close()                      -- return DatabaseMigrationSummary with per-table row counts
```

### 2.4 Tables migrated (18 tables)

The migration tool transfers exactly these 18 tables:

| # | Table | Notes |
|---|-------|-------|
| 1 | `sites` | |
| 2 | `site_api_endpoints` | |
| 3 | `site_announcements` | |
| 4 | `site_disabled_models` | |
| 5 | `accounts` | JSON column: `extra_config` |
| 6 | `account_tokens` | |
| 7 | `checkin_logs` | |
| 8 | `model_availability` | |
| 9 | `token_model_availability` | |
| 10 | `token_routes` | JSON columns: `model_mapping`, `decision_snapshot` |
| 11 | `route_channels` | |
| 12 | `route_group_sources` | |
| 13 | `proxy_logs` | JSON column: `billing_details` |
| 14 | `proxy_video_tasks` | JSON columns: `status_snapshot`, `upstream_response_meta` |
| 15 | `proxy_files` | Large column: `content_base64` (base64-encoded binary) |
| 16 | `downstream_api_keys` | JSON columns: `supported_models`, `allowed_route_ids`, `site_weight_multipliers`, `excluded_site_ids`, `excluded_credential_refs` |
| 17 | `events` | |
| 18 | `settings` | Filtered: `db_type`, `db_url`, `db_ssl` are skipped |

**TS note**: The TS service reads from a live Drizzle ORM database (which could be SQLite, MySQL, or PG) rather than from a SQLite file directly. The TS `toBackupSnapshot()` calls `db.select().from(schema.X).all()` for each table. The Go version reads from a SQLite file path instead.

### 2.5 Row insertion strategy

**TS behavior (row-by-row)**: The TS `insertAllRows()` iterates over all `InsertStatement` objects and executes each as a separate `INSERT` statement. There is no batching, chunking, or multi-row VALUES clause. Every row gets its own database round-trip.

**Go enhancement opportunity**: The Go version may add configurable batch insertion (e.g., 1000 rows per multi-row INSERT) for performance. This should be a CLI flag (`--batch-size N`) and must still use a single transaction wrapping the entire operation.

### 2.6 Per-column type coercion

The TS migration performs meticulous per-column type coercion with column-specific fallback values. The Go implementation must replicate this exactly:

**Type coercion functions:**

```
asString(value)        -> string, trimming whitespace
asBoolean(value, fb)   -> bool, with fallback (handles "1"/"true"/"yes"/"on", "0"/"false"/"no"/"off")
asNumber(value, fb)    -> int64|null, with fallback (null, 0, 1, or 10 depending on column)
asNullableString(val)  -> string|null
```

**Column-specific fallback defaults (non-exhaustive key examples):**

| Column | Coercion | Fallback |
|--------|----------|----------|
| `sites.id`, most `id` columns | `asNumber` | `0` |
| `sites.status` | `asNullableString` then `??` | `'active'` |
| `sites.global_weight` | `asNumber` | `1` |
| `sites.use_system_proxy` | `asBoolean` | `false` |
| `site_api_endpoints.enabled` | `asBoolean` | `true` |
| `accounts.checkin_enabled` | `asBoolean` | `true` |
| `accounts.balance` | `asNumber` | `0` |
| `accounts.unit_cost` | `asNumber` | `null` |
| `account_tokens.enabled` | `asBoolean` | `true` |
| `account_tokens.value_status` | `asNullableString` then `??` | `'ready'` |
| `account_tokens.source` | `asNullableString` then `??` | `'manual'` |
| `checkin_logs.status` | `asNullableString` then `??` | `'success'` |
| `model_availability.available` | `asBoolean` | `false` |
| `token_routes.route_mode` | `asNullableString` then `??` | `'pattern'` |
| `token_routes.enabled` | `asBoolean` | `true` |
| `route_channels.weight` | `asNumber` | `10` |
| `route_channels.enabled` | `asBoolean` | `true` |
| `site_announcements.level` | `asNullableString` then `??` | `'info'` |
| `events.level` | `asNullableString` then `??` | `'info'` |
| `events.read` | `asBoolean` | `false` |
| `downstream_api_keys.enabled` | `asBoolean` | `true` |
| `downstream_api_keys.used_cost` | `asNumber` | `0` |
| `downstream_api_keys.used_requests` | `asNumber` | `0` |
| `site_disabled_models` — all columns | `asNumber`/`asNullableString` | standard |

The full mapping of every column for all 18 tables is in `databaseMigrationService.ts` lines 317-719 (`buildStatements()`).

### 2.7 JSON column serialization

13 columns across 5 tables are typed as `json` in the schema contract. The TS `serializeColumnValue()` checks `getColumnLogicalType(table, column)` from `schemaContract.json`. If the logical type is `'json'`, the value is serialized via `JSON.stringify()` (objects/arrays) or passed through as-is (strings). Null/undefined becomes SQL NULL.

JSON columns by table:

| Table | JSON Columns |
|-------|-------------|
| `sites` | `custom_headers` |
| `accounts` | `extra_config` |
| `token_routes` | `model_mapping`, `decision_snapshot` |
| `proxy_logs` | `billing_details` |
| `proxy_video_tasks` | `status_snapshot`, `upstream_response_meta` |
| `downstream_api_keys` | `supported_models`, `allowed_route_ids`, `site_weight_multipliers`, `excluded_site_ids`, `excluded_credential_refs` |

The Go version must maintain a column-type map (equivalent to `schemaContract.json`) and apply JSON serialization where needed.

### 2.8 Runtime database setting key filtering

Three settings keys control the destination database connection itself and must NOT be migrated:

- `db_type`
- `db_url`
- `db_ssl`

The TS filters these in `buildStatements()` via `RUNTIME_DATABASE_SETTING_KEYS.has(row.key)`. The settings INSERT also omits the `id` column (only `key` and `value` are inserted) -- this avoids auto-increment ID conflicts on repeated overwrite migrations.

### 2.9 SQLite URL normalization

The TS `normalizeSqliteTarget()` handles four SQLite path formats:

| Input | Normalized Output |
|-------|-------------------|
| `:memory:` | `:memory:` (passthrough) |
| `file:///path/to/db` | `/path/to/db` (extracts pathname from file:// URL) |
| `sqlite://path/to/db` | `path/to/db` (strips `sqlite://` prefix) |
| `/path/to/db` (plain path) | `/path/to/db` (as-is) |
| `http://...`, `postgres://...` etc. | ERROR: "SQLite connection cannot be a network URL" |

The Go tool must support at minimum the `sqlite://` prefix and plain file paths. `file://` and `:memory:` support are optional for the Go version.

### 2.10 Connection string validation for PostgreSQL

The TS `assertDialectUrl()` validates:
- PostgreSQL URLs must start with `postgres:` or `postgresql:`
- Invalid URLs throw descriptive errors

The Go tool should validate the `--to` argument similarly.

### 2.11 FK-safe clear order (overwrite mode)

When `--overwrite` is set, the TS `clearTargetData()` deletes from all 18 tables in this exact order (children before parents, to avoid FK violations):

```
route_channels          (FK: route_id -> token_routes, account_id -> accounts, token_id -> account_tokens)
route_group_sources     (FK: group_route_id -> token_routes, source_route_id -> token_routes)
token_model_availability (FK: token_id -> account_tokens)
model_availability      (FK: account_id -> accounts)
checkin_logs            (FK: account_id -> accounts)
proxy_logs              (FK: route_id, channel_id, account_id)
proxy_video_tasks       (FK: channel_id, account_id)
proxy_files             (no FKs)
account_tokens          (FK: account_id -> accounts)
accounts                (FK: site_id -> sites)
site_announcements      (FK: site_id -> sites)
site_disabled_models    (FK: site_id -> sites)
site_api_endpoints      (FK: site_id -> sites)
token_routes            (no FKs)
sites                   (no FKs)
downstream_api_keys     (no FKs)
events                  (no FKs)
settings                (no FKs)
```

### 2.12 PostgreSQL sequence synchronization

After data insertion, the TS calls `syncPostgresSequences()` which runs `SELECT setval(pg_get_serial_sequence('<table>', 'id'), COALESCE((SELECT MAX(id) FROM "<table>"), 1), TRUE)` for all 17 tables that have auto-increment `id` columns (settings is excluded -- settings has no serial id in the migration path).

### 2.13 Transaction boundary

The TS wraps the entire clear + insert + sync-sequences block in a single database transaction. On any error, the entire transaction is rolled back. The Go version must replicate this.

### 2.14 New Go features (not in TS)

The following are **new additions** for the Go version with no TS precedent:

- **Dry-run mode** (`--dry-run`): Validate inputs, connect to both databases, print migration plan (row counts per table), but do not write anything.
- **Progress bar** (`--progress`): Show per-table progress during data transfer (table N/18, rows inserted, elapsed time).
- **Checksum verification** (`--verify`): After migration, compute row-count + hash comparison per table between source and target.
- **Batch insert** (`--batch-size N`): Optionally use multi-row INSERT for performance (TS does row-by-row only).

### 2.15 DatabaseMigrationSummary output

After a successful migration, the tool prints a summary with per-table row counts:

```
dialect: postgres
connection: postgres://<user>:***@<host>:5432/db  (password masked)
overwrite: true
version: live-db-snapshot
timestamp: 1720000000000
rows:
  sites: 5
  site_api_endpoints: 12
  ...
  settings: 8
```

---

## 3. CI/CD

### 3.1 CI workflow (`ci.yml`) [NEW -- TS has no CI]

The TS repository has **zero CI workflows** (no lint, no test, no build checks). The CI pipeline is entirely new for the Go version.

```yaml
name: CI

on: [push, pull_request]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - uses: golangci/golangci-lint-action@v6

  test-sqlite:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - run: go test ./... -count=1

  test-pg:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_PASSWORD: test
          POSTGRES_DB: metapi_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - run: |
          dsn="${PG_SCHEME}://${PG_USER}:${PG_PASSWORD}@localhost:5432/${PG_DATABASE}?sslmode=disable"
          DATABASE_URL="$dsn" go test ./... -count=1 -tags=integration
        env:
          PG_SCHEME: postgres
          PG_USER: postgres
          PG_PASSWORD: test
          PG_DATABASE: metapi_test

  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - run: go build ./cmd/server
      - run: go build ./cmd/migrate
```

### 3.2 CD workflow (`cd.yml`)

The TS `cd-slim.yml` triggers on `push: branches: [main]` with path filters and `workflow_dispatch`. The Go CD can choose either trigger model. Below is the **recommended** Go version, which includes the best of TS (path filters, workflow_dispatch, GHA caching) plus tag-based versioned releases.

```yaml
name: CD

on:
  push:
    branches: [main]
    paths:
      - "**.go"
      - "go.mod"
      - "go.sum"
      - "cmd/**"
      - "internal/**"
      - "web/**"
      - "Dockerfile"
      - ".github/workflows/cd.yml"
    tags: ['v*']
  workflow_dispatch:

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ghcr.io/tokendancelab/metapi-go

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - uses: actions/checkout@v4

      - uses: docker/setup-buildx-action@v3

      - uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - uses: docker/metadata-action@v5
        id: meta
        with:
          images: ${{ env.IMAGE_NAME }}
          tags: |
            type=raw,value=latest,enable=${{ github.ref == 'refs/heads/main' }}
            type=semver,pattern={{version}},enable=${{ startsWith(github.ref, 'refs/tags/v') }}
            type=semver,pattern={{major}}.{{minor}},enable=${{ startsWith(github.ref, 'refs/tags/v') }}
            type=sha,prefix=,format=short

      - uses: docker/build-push-action@v6
        with:
          context: .
          file: ./Dockerfile
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
          platforms: linux/amd64
```

### 3.3 Divergence notes from TS

| Aspect | TS | Go |
|--------|----|----|
| CI exists | No | Yes (new) |
| CD trigger | `branches: [main]` only | `main` + `tags: ['v*']` + `workflow_dispatch` |
| Image name | `ghcr.io/tokendancelab/metapi` | `ghcr.io/tokendancelab/metapi-go` |
| Tag strategy | `latest` + `YYYYMMDD` + `sha-short` | `latest` + `vX.Y.Z` + `vX.Y` + `sha-short` |
| Path filters | `src/**`, `drizzle/**`, `package.json`, etc. | `**.go`, `cmd/**`, `internal/**`, `web/**`, `Dockerfile` |
| Multi-arch | Comment claims `amd64, arm64` (no `--platform` arg) | `linux/amd64` only initially; arm64 can be added later |
| Cache | `cache-from: gha` + `cache-to: gha,mode=max` | Same (retained from TS) |

The `metapi-go` image name is intentional to avoid collision with the existing TS `metapi` image in the same GHCR namespace.

---

## 4. Dockerfile

### 4.1 Multi-stage build

```dockerfile
# Stage 1: Frontend build
FROM node:25-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build:web

# Stage 2: Go build
FROM golang:1.24-alpine AS go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi ./cmd/server
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi-migrate ./cmd/migrate

# Stage 3: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl
RUN mkdir -p /app/data
COPY --from=go /app/metapi /usr/local/bin/metapi
COPY --from=go /app/metapi-migrate /usr/local/bin/metapi-migrate
EXPOSE 4000
ENV DATA_DIR=/app/data
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:4000/health || exit 1
CMD ["metapi"]
```

### 4.2 Key design decisions

| Decision | Rationale |
|----------|-----------|
| `alpine:3.21` not `scratch` | Need `ca-certificates` (TLS), `tzdata` (timezone), `curl` (healthcheck). Alpine adds ~7MB over scratch. |
| `CGO_ENABLED=0` | Pure Go, no C dependencies. Produces static binary. |
| `-ldflags="-s -w"` | Strip debug info, reducing binary by ~30%. |
| `mkdir -p /app/data` | Creates the data directory at build time so VOLUME mounts correctly even when the host directory is empty. |
| `HEALTHCHECK` | **New Go addition** -- not present in TS Dockerfile.slim. Uses `/health` endpoint that the Go server must expose. |
| `VOLUME ["/app/data"]` | **New Go addition** -- not present in TS Dockerfile.slim. Ensures SQLite database persists across container restarts. |
| No `npm prune` or native modules | The TS Dockerfile needs `python3 make g++` for `better-sqlite3`/`sharp` native rebuilds and `npm prune --omit=dev`. The Go version only uses the Node stage for `npm run build:web` (Vite) -- no native modules are compiled. |
| Auto-migration on startup | The TS CMD runs `node dist/server/db/migrate.js && node dist/server/index.js`. The Go version must embed auto-migration into the `metapi` binary itself (e.g., run DDL migrations at startup before serving). |

### 4.3 Image size target

**Target: <25MB** (binary + embedded frontend + Alpine base).

Breakdown:
- Alpine 3.21 base: ~7MB
- Go binary (stripped): ~10-12MB
- Embedded web/dist (gzip compressed by Go embed): ~3-5MB
- **Total est.: ~20-24MB** -- aspirational, to be verified after build.

The TS Docker image (`node:25-alpine` base) is ~50MB+ for the base layer alone.

### 4.4 Auto-migration mechanism

The Go binary must run DDL migrations at startup before accepting connections. Options:

**Option A (embedded migrator)**: The `cmd/server` binary imports the migration package and runs `migrate.Up()` during initialization. This is the recommended approach -- it matches the TS pattern and requires no external orchestration.

**Option B (init container)**: The Docker Compose or Kubernetes config runs `metapi-migrate` as an init container before starting the server. More complex but separates concerns.

The `metapi-migrate` standalone binary is for **data migration** (SQLite -> PG), not for DDL migration. DDL migration must be built into `metapi` itself.

---

## 5. Docker Compose

### 5.1 Production compose

Based on the TS `docker-compose.yml`:

```yaml
services:
  metapi:
    image: ghcr.io/tokendancelab/metapi-go:latest
    ports:
      - "127.0.0.1:4000:4000"
    volumes:
      - ./data:/app/data
    environment:
      # Required (validated at startup -- missing = exit with error)
      AUTH_TOKEN: ${AUTH_TOKEN:?AUTH_TOKEN is required}
      PROXY_TOKEN: ${PROXY_TOKEN:?PROXY_TOKEN is required}
      # Optional with defaults
      CHECKIN_CRON: ${CHECKIN_CRON:-0 8 * * *}
      BALANCE_REFRESH_CRON: ${BALANCE_REFRESH_CRON:-0 * * * *}
      PORT: ${PORT:-4000}
      DATA_DIR: /app/data
      TZ: ${TZ:-Asia/Shanghai}
      # Database config (for PG mode)
      DATABASE_URL: ${DATABASE_URL:-}
    restart: unless-stopped
```

### 5.2 Development compose

```yaml
services:
  metapi:
    build: .
    ports:
      - "4000:4000"
    volumes:
      - ./data:/app/data
    environment:
      AUTH_TOKEN: dev-token
      PROXY_TOKEN: dev-proxy-token
      DATA_DIR: /app/data
```

### 5.3 Required environment variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AUTH_TOKEN` | Yes | -- | Admin API bearer token. Missing = fatal. |
| `PROXY_TOKEN` | Yes | -- | Proxy endpoint API key. Missing = fatal. |
| `CHECKIN_CRON` | No | `0 8 * * *` | Daily checkin cron expression |
| `BALANCE_REFRESH_CRON` | No | `0 * * * *` | Hourly balance refresh cron |
| `PORT` | No | `4000` | HTTP listen port |
| `DATA_DIR` | No | `/app/data` | SQLite database directory |
| `TZ` | No | `Asia/Shanghai` | Timezone for cron scheduling |
| `DATABASE_URL` | No | -- | PostgreSQL connection string (when set, use PG instead of SQLite) |

---

## 6. Documentation

### 6.1 Required documents

| Document | Content |
|----------|---------|
| `README.md` | Project overview, features, quick start (`go build` + `docker compose up`) |
| `docs/deployment.md` | Full deployment guide: env vars, docker-compose, nginx reverse proxy, TLS |
| `docs/architecture.md` | Go version architecture, differences from TS, S.U.P.E.R improvements |
| `docs/api.md` | Admin API reference (consistent with TS version) |
| `docs/migration.md` | TS -> Go migration guide: stop service -> dump SQLite -> switch binary -> verify |
| `docs/specs/*.md` | 14 spec files (P0-P13, created during planning) |

### 6.2 Docs coverage checklist

- [ ] `README.md`: badges (CI status, Go version, Docker image), quick start, feature list, links to docs
- [ ] `docs/deployment.md`: prerequisites, Docker Compose, bare-metal, nginx config, TLS (Let's Encrypt), env var reference, backup strategy
- [ ] `docs/architecture.md`: package diagram, data flow (SQLite/PG -> store -> handlers -> chi), key design decisions, TS vs Go comparison table
- [ ] `docs/api.md`: all admin endpoints with request/response examples, auth headers, error format
- [ ] `docs/migration.md`: step-by-step guide, `metapi-migrate` usage, rollback plan, verification checklist
- [ ] `docs/specs/*.md`: all 14 P0-P13 specs

---

## Acceptance Criteria

- [ ] `go build ./cmd/server` produces a single binary with embedded frontend
- [ ] `./metapi` starts -> browser at `:4000` -> React SPA loads
- [ ] `/assets/*` served with `Cache-Control: public, max-age=31536000, immutable`
- [ ] `index.html` served with `Cache-Control: no-cache`
- [ ] `./metapi-migrate` transfers all 18 tables from SQLite to PG with correct row counts
- [ ] `./metapi-migrate --dry-run` prints migration plan without writing data [NEW]
- [ ] `./metapi-migrate --overwrite` clears target data in FK-safe order before inserting
- [ ] `./metapi-migrate` skips `db_type`, `db_url`, `db_ssl` settings keys
- [ ] `./metapi-migrate` applies per-column type coercion with correct fallback defaults
- [ ] `./metapi-migrate` serializes JSON columns (13 columns across 5 tables)
- [ ] `./metapi-migrate` syncs PostgreSQL sequences after insertion
- [ ] `./metapi-migrate` rolls back entire transaction on error
- [ ] CI: `golangci-lint` passes
- [ ] CI: `go test ./...` passes (SQLite)
- [ ] CI: `go test ./... -tags=integration` passes (PostgreSQL via service container)
- [ ] CI: `go build ./cmd/server && go build ./cmd/migrate` succeeds
- [ ] CD: Docker image builds and pushes to `ghcr.io/tokendancelab/metapi-go`
- [ ] Docker image <25MB (binary + frontend + Alpine)
- [ ] Docker healthcheck returns 200 on `/health`
- [ ] Docker `mkdir -p /app/data` ensures volume mount directory exists
- [ ] Auto-migration runs at server startup (DDL, not data migration)
- [ ] Documentation complete: deployment + architecture + api + migration + README

---

## Test Plan

| Item | Verification |
|------|-------------|
| `go test ./...` | CI auto (SQLite :memory:) |
| `go test ./... -tags=integration` | CI auto (PostgreSQL service container) |
| `go build ./cmd/server` | CI build check |
| `go build ./cmd/migrate` | CI build check |
| SQLite -> PG data migration | Test: create SQLite with test data -> `metapi-migrate` -> verify PG row counts match and JSON columns are intact |
| Migration dry-run | Test: `metapi-migrate --dry-run` -> verify no data written, plan matches |
| Migration overwrite | Test: populate PG with stale data -> `metapi-migrate --overwrite` -> verify stale data replaced |
| Migration FK-safe clear | Test: populate PG with FK-constrained data -> `metapi-migrate --overwrite` -> verify no FK violations during DELETE |
| Migration settings filter | Test: create SQLite with `db_type`/`db_url`/`db_ssl` settings -> migrate -> verify these keys are NOT in PG |
| Migration rollback | Test: corrupt one row mid-migration -> verify entire transaction rolled back, PG unchanged |
| Docker healthcheck | `docker run` -> `curl :4000/health` -> 200 |
| Docker frontend | `docker run` -> `curl :4000/` -> returns `text/html` with React root div |
| Docker asset caching | `curl -I :4000/assets/xxx.js` -> `Cache-Control: public, max-age=31536000, immutable` |
| Docker data persistence | `docker run` with volume -> stop -> start -> data still present |

---

## Edge Cases

### E1. Empty source database (per-table)

Some tables may have 0 rows while others have data. The migration must handle per-table emptiness: `buildStatements()` produces no INSERTs for empty tables, `insertAllRows()` skips silently, and the summary shows 0 for those tables.

### E2. Orphaned foreign key references

The TS migration copies data as-is without validating FK integrity. If the SQLite source was not enforcing FKs (SQLite FK enforcement is opt-in), there may be orphaned rows (e.g., `account_tokens` referencing a deleted `account_id`). The Go version should either: (a) validate FKs before insertion and warn/reject, or (b) defer FK constraints during migration and re-enable after. Option (b) is preferred for robustness.

### E3. FK-safe deletion order during overwrite

The `clearTargetData()` order in section 2.11 is mandatory. Child tables (those with FKs) must be cleared before parent tables. Changing this order will cause FK violation errors on DELETE.

### E4. Settings table idempotency across repeated overwrites

The settings INSERT omits the `id` column. If the PG settings table has an auto-increment `id`, each `--overwrite` run will produce new IDs for the same keys. This is acceptable because settings are key-value and looked up by key, not by ID. However, if any code relies on settings.id stability, this could be an issue.

### E5. Large binary content in proxy_files.content_base64

The `proxy_files` table stores base64-encoded file content. A single row could be multi-megabyte. The TS reads everything into memory via `toBackupSnapshot()` and inserts row-by-row. The Go version should consider: (a) a memory limit flag (`--max-row-size`), (b) streaming large rows in chunks, or (c) stating the memory requirement upfront (SQLite DB size + overhead = RAM needed).

### E6. Concurrent access during migration

The TS migration reads from the live database under the assumption the source is quiescent. No locking mechanism exists. The Go migration reads from a SQLite file -- the source service should be **stopped** before migration to ensure a consistent snapshot. This is documented in `docs/migration.md` (stop service -> dump -> switch binary -> verify).

### E7. Docker auto-migration at startup

If `DATA_DIR` is empty (fresh deployment), the server must auto-create the SQLite database and run DDL migrations before listening. If DDL migration fails, the server must exit with a clear error (no silent fallback to an empty DB).

### E8. web/dist not present at build time

If the frontend has not been built before `go build`, the Go compiler will fail because `//go:embed web/dist` cannot find the directory. The build process (CI, Dockerfile, local dev) must ensure `web/dist/` exists first. Provide a clear error message or a Makefile target that builds both.

### E9. Multi-arch (amd64 vs arm64)

The Go CD initially targets `linux/amd64` only. The TS Dockerfile comment claims dual-arch but the workflow has no `--platform` argument. If arm64 support is needed, add `platforms: linux/amd64,linux/arm64` to the `docker/build-push-action` step. The Go cross-compilation is trivial (`GOARCH=arm64`).

### E10. Frontend build without native server dependencies

The TS Dockerfile requires `python3 make g++` and `npm rebuild esbuild sharp better-sqlite3` because the server uses native modules. The Go version only builds the React frontend in the Node stage. Verify that `web/package.json` has no native dependencies beyond `vite` and `esbuild` (which ship prebuilt binaries). If `sharp` or other native deps are in the web package.json, they must be removed or the build stage must add build tools.

### E11. SPA routing for deep links

The `NotFound` handler must return `index.html` for client-side routes (e.g., `/sites/123`, `/accounts`). The exclusion list (`/api/`, `/v1/`, `/assets/`) must be comprehensive. Additionally, the `/health` endpoint must NOT fall through to the SPA handler. Explicitly register `/health` as a route that returns JSON before the NotFound fallback.

### E12. Database migration summary with password masking

The migration summary prints the target connection string with the password replaced by `***`. This prevents accidental credential leaks in logs. The Go tool must implement equivalent masking.
