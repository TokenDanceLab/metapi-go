# P13 Implementation Review: Frontend Embed + Migration Tool + CI/CD + Dockerfile

**Review date**: 2026-07-04
**Reviewed by**: automated spec-vs-impl audit
**Spec**: `docs/specs/p13-embed-ci.md`

---

## Summary

| Area | Status | Details |
|------|--------|---------|
| Frontend embed (go:embed) | PASS | Correctly wired with SPA fallback and cache headers |
| Migration tool (cmd/migrate) | PASS | All 18 tables, type coercion, JSON serialization, FK-safe order |
| CI workflow | BLOCKER | Two issues will prevent CI from passing |
| CD workflow | PASS | Valid syntax, matches spec |
| Dockerfile | PASS (minor deviation) | Multi-stage build valid; one intentional deviation |
| Documentation | INCOMPLETE | 5 required docs missing |
| docker-compose | PARTIAL | Dev compose present; production compose missing |

---

## 1. Frontend Embed -- PASS

### 1.1 go:embed directive

**File**: `web/embed.go`

```go
//go:embed dist
var Dist embed.FS
```

Directive is correctly placed on the `dist` directory (not `dist/*`), which embeds the entire directory tree including `index.html` and `assets/`. The exported `Dist` variable is consumed by `router.New()` in `cmd/server/main.go` line 71.

### 1.2 SPA fallback

**File**: `router/router.go` lines 103-134

The `setupSPAFallback` function implements:

- `/assets/*` served with `Cache-Control: public, max-age=31536000, immutable` -- matches spec section 1.2.
- `/api/` and `/v1/` paths excluded from fallback, returning JSON 404 -- matches spec.
- All other paths serve `index.html` with `Cache-Control: no-cache` -- matches spec.
- `/health` endpoint is registered **before** the SPA fallback (line 31), so it is never intercepted by NotFound -- correctly handles spec edge case E11.

### 1.3 Build verification

```
go build ./cmd/server  ->  EXIT 0
go vet ./...           ->  EXIT 0
```

`web/dist/` contains `index.html` and fingerprinted assets in `web/dist/assets/`. The embed compiles cleanly.

---

## 2. Migration Tool -- PASS

**File**: `cmd/migrate/main.go` (1191 lines)

### 2.1 CLI interface

All specified flags are present and match the spec (section 2.1):

| Flag | Spec | Implementation |
|------|------|---------------|
| `--from` | sqlite://path or plain path | implemented, with `file://` and `:memory:` extras |
| `--to` | postgres://... | implemented with validation |
| `--dry-run` | new Go feature | implemented (line 269-273) |
| `--progress` | new Go feature | implemented (line 327-336) |
| `--verify` | new Go feature | implemented (line 356-363) |
| `--overwrite` | default true | implemented (line 47, matches TS) |
| `--batch-size` | new Go feature | implemented (line 48), defaults to 1 (TS-compatible row-by-row) |

### 2.2 18-table coverage

All 18 tables from spec section 2.4 are present in `readAllTables()` (line 415-435) and `buildStatements()` (line 513-536). Each has a dedicated `build*()` function.

### 2.3 Per-column type coercion

Type coercion functions match the spec (section 2.6):

- `asString()` -- trims whitespace (line 52-58)
- `asBoolean()` -- handles "1"/"true"/"yes"/"on" and "0"/"false"/"no"/"off" (line 60-77)
- `asNumber()` -- returns `float64` with column-specific fallback (nil, 0, 1, or 10) (line 79-96)
- `asNullableString()` -- string or nil (line 98-103)

Column-specific fallback defaults verified against spec table (section 2.6):

| Column | Spec fallback | Implementation | Match |
|--------|---------------|----------------|-------|
| `sites.status` | `'active'` | `"active"` (line 557) | YES |
| `sites.global_weight` | `1` | `float64(1)` (line 560) | YES |
| `sites.use_system_proxy` | `false` | `false` (line 555) | YES |
| `site_api_endpoints.enabled` | `true` | `true` (line 587) | YES |
| `accounts.checkin_enabled` | `true` | `true` (line 670) | YES |
| `accounts.balance` | `0` | `float64(0)` (line 662) | YES |
| `accounts.unit_cost` | `null` | `nil` (line 665) | YES |
| `account_tokens.enabled` | `true` | `true` (line 696) | YES |
| `account_tokens.value_status` | `'ready'` | `"ready"` (line 694) | YES |
| `account_tokens.source` | `'manual'` | `"manual"` (line 695) | YES |
| `checkin_logs.status` | `'success'` | `"success"` (line 715) | YES |
| `model_availability.available` | `false` | `false` (line 735) | YES |
| `token_routes.route_mode` | `'pattern'` | `"pattern"` (line 776) | YES |
| `token_routes.enabled` | `true` | `true` (line 780) | YES |
| `route_channels.weight` | `10` | `float64(10)` (line 803) | YES |
| `route_channels.enabled` | `true` | `true` (line 804) | YES |
| `site_announcements.level` | `'info'` | `"info"` (line 631) | YES |
| `events.level` | `'info'` | `"info"` (line 973) | YES |
| `events.read` | `false` | `false` (line 974) | YES |
| `downstream_api_keys.enabled` | `true` | `true` (line 942) | YES |
| `downstream_api_keys.used_cost` | `0` | `float64(0)` (line 945) | YES |
| `downstream_api_keys.used_requests` | `0` | `float64(0)` (line 947) | YES |

### 2.4 JSON column serialization

13 columns across 5 tables defined in `jsonColumnSet` (line 109-122). Exact match with spec section 2.7 table.

`serializeColumnValue()` and `serializeJSONValue()` correctly handle nil, strings, and marshal objects/arrays to JSON string.

### 2.5 FK-safe clear order

`clearOrder` slice (line 159-178) exactly matches spec section 2.11. Children deleted before parents to avoid FK violations.

### 2.6 PG sequence synchronization

`sequenceTables` (line 182-201) contains 17 tables (settings excluded). Uses `setval(pg_get_serial_sequence(...))` pattern matching TS `syncPostgresSequences()` (spec section 2.12).

### 2.7 Settings key filtering

`runtimeDBSettingKeys` (line 151-155) filters `db_type`, `db_url`, `db_ssl` from settings migration (spec section 2.8). Settings INSERT omits `id` column (line 985: only `key`, `value`).

### 2.8 Transaction boundary

Entire clear+insert+sync-sequences block wrapped in a single transaction (lines 300-351). `defer tx.Rollback()` ensures rollback on any error before commit.

### 2.9 Password masking

`maskPassword()` (line 220-231) replaces password with `***` in the summary connection string.

---

## 3. CI/CD

### 3.1 CI workflow -- BLOCKER (2 issues)

**File**: `.github/workflows/ci.yml`

**Syntax**: Valid YAML. Four jobs (lint, test-sqlite, test-pg, build) match the spec section 3.1 exactly.

**Issue 1: CI build will fail -- `web/dist/` is gitignored**

`web/dist/` is listed in `.gitignore` (line 2) and no files under `web/dist/` are tracked by git. On a clean CI checkout, the `build` job runs:

```yaml
- run: go build ./cmd/server   # line 55
```

The `//go:embed dist` directive in `web/embed.go` requires the `web/dist/` directory to exist at compile time. Without it, Go compilation fails:

```
web/embed.go:10: pattern dist: no matching files found
```

**Fix**: Add a frontend build step before Go build, or keep a `.gitkeep` placeholder in `web/dist/` to make the directory exist (the `embed.FS` will still work; the SPA will just serve empty at runtime until the real frontend is built).

**Issue 2: go.mod declares Go 1.26.3, CI uses Go 1.24**

`go.mod` line 3: `go 1.26.3`
CI line 12: `go-version: '1.24'`

The Go toolchain refuses to build a module with `go 1.26.3` using an older Go release. Go 1.26.3 is a pre-release/toolchain version and should be `go 1.24` to match CI.

**Fix**: Change `go.mod` to `go 1.24` (or update CI to use a version >= the go directive).

### 3.2 CD workflow -- PASS

**File**: `.github/workflows/cd.yml`

Valid YAML syntax. Matches spec section 3.2:
- Triggers: `push: branches: [main]`, `tags: ['v*']`, `workflow_dispatch`
- Docker buildx, login, metadata-action, build-push-action all present
- Image name: `ghcr.io/tokendancelab/metapi-go` (matches spec divergence table)
- Tag strategy: `latest`, `vX.Y.Z`, `vX.Y`, `sha-short`
- GHA caching: `cache-from: type=gha`, `cache-to: type=gha,mode=max`
- Platform: `linux/amd64`

---

## 4. Dockerfile -- PASS (minor deviation)

**File**: `Dockerfile`

### 4.1 Multi-stage structure

Three stages match spec section 4.1:
1. **Stage 1**: `FROM node:25-alpine AS web` -- builds frontend
2. **Stage 2**: `FROM golang:1.24-alpine AS go` -- builds Go binaries with `CGO_ENABLED=0 -ldflags="-s -w"`
3. **Stage 3**: `FROM alpine:3.21` -- runtime with ca-certificates, tzdata, curl

### 4.2 Key decisions (spec section 4.2)

| Decision | Spec | Implementation | Match |
|----------|------|---------------|-------|
| Base image | alpine:3.21 | alpine:3.21 | YES |
| CGO_ENABLED=0 | Yes | Yes | YES |
| ldflags="-s -w" | Yes | Yes | YES |
| mkdir -p /app/data | Yes | Yes | YES |
| HEALTHCHECK | Yes | Yes | YES |
| VOLUME /app/data | Yes | Yes | YES |
| EXPOSE 4000 | 4000 | 4000 | YES |
| CMD | metapi | metapi | YES |

### 4.3 Minor deviation: `npx vite build` vs `npm run build:web`

**Spec** (section 4.1): `RUN npm run build:web`

**Implementation** (line 7): `RUN npx vite build`

`npm run build:web` expands to `npm run desktop:icons && vite build` (per `web/package.json` line 33). The Dockerfile skips desktop icon generation (`desktop:icons` runs a Node script to generate Electron icons). This is a reasonable and likely intentional deviation -- desktop icons are irrelevant inside a Docker container. However, the `COPY web/package.json web/package-lock.json` and `RUN npm ci` already install all dependencies needed for `npm run build:web`, so switching to `npm run build:web` would work without adding build tools.

**Verdict**: Low risk, intentional optimization. Does not break the build. Recommend documenting the reason in a comment.

### 4.4 Edge case note: no npm prune or native modules

The Dockerfile correctly avoids `npm prune` and native build tools (`python3 make g++`) since the frontend build uses only Vite and esbuild (prebuilt binaries). This matches spec section 4.2.

---

## 5. docker-compose -- PARTIAL

**File**: `docker-compose.yml`

Only a development compose is present:
```yaml
services:
  metapi:
    build: .
    environment:
      AUTH_TOKEN: change-me
      PROXY_TOKEN: sk-change-me
```

### Missing: production compose

The spec section 5.1 defines a production compose with:
- `image: ghcr.io/tokendancelab/metapi-go:latest` (instead of `build: .`)
- `ports: "127.0.0.1:4000:4000"` (bind to localhost)
- Required env validation (`${AUTH_TOKEN:?...}`)
- Optional env defaults (`CHECKIN_CRON`, `BALANCE_REFRESH_CRON`, `PORT`, `TZ`, `DATABASE_URL`)
- `restart: unless-stopped`

These are not present. A separate production compose or a compose with both profiles should be added.

---

## 6. Documentation -- INCOMPLETE

### Missing required documents

| Document | Spec section | Status |
|----------|-------------|--------|
| `README.md` | 6.2 | MISSING |
| `docs/deployment.md` | 6.2 | MISSING |
| `docs/architecture.md` | 6.2 | MISSING |
| `docs/api.md` | 6.2 | MISSING |
| `docs/migration.md` | 6.2 | MISSING |
| `docs/specs/*.md` (P0-P13) | 6.2 | All 14 specs present |

---

## 7. Cross-Cutting Observations

### 7.1 Build tooling

**Makefile** (`Makefile`) is complete with targets for build, test, lint, run, docker-build, web-build, migrate-build, and clean. The `web-build` target correctly runs `cd web && npm ci && npx vite build` before Go compilation.

### 7.2 Dependency health

`go.sum` exists (93 lines, 7889 bytes). All required dependencies are in `go.mod`:
- `github.com/go-chi/chi/v5` -- router
- `github.com/jackc/pgx/v5` -- PostgreSQL driver (migration tool)
- `modernc.org/sqlite` -- pure-Go SQLite driver (migration tool)
- `github.com/jmoiron/sqlx` -- database helpers
- `github.com/joho/godotenv` -- .env loading
- `robfig/cron/v3` -- cron scheduler (indirect, used by app pkg)

### 7.3 Build artifacts present

Windows `.exe` binaries in the repo root (`metapi.exe`, `metapi-migrate.exe`, `migrate.exe`, `server.exe`) are build artifacts and should ideally be in `.gitignore`. Not blocking.

### 7.4 node:25-alpine availability

The Dockerfile uses `node:25-alpine`. Node 25 is not yet an LTS release (current LTS is 24.x). While the image is available on Docker Hub, this may cause unexpected churn. The spec explicitly specifies `node:25-alpine`, so this matches the spec.

---

## 8. Acceptance Criteria Checklist

| # | Criterion | Status | Notes |
|---|-----------|--------|-------|
| 1 | `go build ./cmd/server` embeds frontend | PASS | Verified locally |
| 2 | `./metapi` starts, browser loads React SPA | UNVERIFIED | Requires running server |
| 3 | `/assets/*` served with immutable cache | PASS | Code correctly sets Cache-Control header |
| 4 | `index.html` served with no-cache | PASS | Code correctly sets Cache-Control header |
| 5 | `metapi-migrate` transfers all 18 tables | PASS | All tables mapped with coercion |
| 6 | `metapi-migrate --dry-run` prints plan | PASS | Implemented line 269-273 |
| 7 | `metapi-migrate --overwrite` FK-safe clear | PASS | clearOrder matches spec |
| 8 | `metapi-migrate` skips db_type/db_url/db_ssl | PASS | runtimeDBSettingKeys filter |
| 9 | Per-column type coercion with fallbacks | PASS | Verified 21 column-specific defaults |
| 10 | JSON columns serialized (13 cols, 5 tables) | PASS | jsonColumnSet + serializeColumnValue |
| 11 | PG sequence sync after insert | PASS | 17 tables, settings excluded |
| 12 | Rollback entire transaction on error | PASS | Single tx with defer Rollback |
| 13 | CI: golangci-lint passes | BLOCKED | By go.mod version + web/dist issues |
| 14 | CI: go test ./... (SQLite) | BLOCKED | By go.mod version issue |
| 15 | CI: go test ./... -tags=integration (PG) | BLOCKED | By go.mod version issue |
| 16 | CI: go build both binaries | BLOCKED | web/dist gitignored |
| 17 | CD: Docker image builds and pushes | LIKELY PASS | Syntax valid |
| 18 | Docker image <25MB | UNVERIFIED | Requires build |
| 19 | Docker healthcheck /health returns 200 | PASS | CMD curl -f localhost:4000/health |
| 20 | Docker mkdir -p /app/data | PASS | Line 22 |
| 21 | Auto-migration at startup | CODE PRESENT | store.EnsureRuntimeDatabase + store.Migrate in main.go |
| 22 | Documentation complete | INCOMPLETE | 5 docs missing |

---

## 9. Required Fixes (ranked)

### BLOCKER -- must fix before CI will pass

1. **`web/dist/` gitignored breaks CI build**: Either add `.gitkeep` to `web/dist/` and track it (so the directory exists on clone), or add a frontend build step before `go build ./cmd/server` in CI. Simplest fix: add `web/dist/.gitkeep` and commit it.

2. **`go.mod` version incompatible with CI**: Change `go 1.26.3` to `go 1.24` in `go.mod` line 3. The current value is a local toolchain version leak.

### HIGH -- documented requirements missing

3. **Create README.md** with badges, quick start, feature list, doc links (spec section 6.2).

4. **Create production docker-compose.yml** (or add profiles to the existing one) with image-based deployment, required env validation, and restart policy (spec section 5.1).

### MEDIUM -- required documentation

5. Create `docs/deployment.md`
6. Create `docs/architecture.md`
7. Create `docs/api.md`
8. Create `docs/migration.md`

### LOW -- minor deviations

9. Dockerfile uses `npx vite build` instead of `npm run build:web` -- low risk, consider adding a comment explaining the intentional omission of `desktop:icons`.
