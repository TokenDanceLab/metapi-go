# Deployment Guide

## Prerequisites

- Docker (for containerized deployment)
- Go 1.26.4+ (for bare-metal deployment)
- Node.js 25+ (for frontend build)
- PostgreSQL 16+ (optional, for production database)
- A reverse proxy (nginx or Caddy) with TLS

## Environment Variables

### Required

| Variable | Description |
|----------|-------------|
| `AUTH_TOKEN` | Admin API bearer token. Server exits if missing. |
| `PROXY_TOKEN` | Proxy endpoint API key. Server exits if missing. |

### Optional

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `4000` | HTTP listen port |
| `DATA_DIR` | `/app/data` | SQLite database directory |
| `CHECKIN_CRON` | `0 8 * * *` | Daily checkin cron expression |
| `BALANCE_REFRESH_CRON` | `0 * * * *` | Hourly balance refresh cron |
| `TZ` | `Asia/Shanghai` | Timezone for cron scheduling |
| `DB_TYPE` | `sqlite` | Database type: `sqlite` or `postgres`; inferred as `postgres` when a PostgreSQL URL is provided |
| `DATABASE_URL` | _(empty)_ | PostgreSQL connection string; alias of `DB_URL`; when set to `postgres://` or `postgresql://`, uses PG instead of SQLite |
| `DB_URL` | _(empty)_ | Database URL or SQLite file path; takes precedence over `DATABASE_URL` |
| `DB_SSLMODE` | _(empty)_ | PostgreSQL TLS mode. Supports `disable`, `allow`, `prefer`, `require`, `verify-ca`, and `verify-full`; non-empty values override `sslmode` in the connection string |
| `DB_MAX_OPEN_CONNS` | `20` | PostgreSQL application pool ceiling; must not exceed the database role connection limit |
| `DB_MAX_IDLE_CONNS` | `5` | PostgreSQL idle pool ceiling; must not exceed `DB_MAX_OPEN_CONNS` |
| `DB_CONN_MAX_LIFETIME_SEC` | `1800` | Maximum PostgreSQL connection lifetime; `0` disables rotation |
| `DB_CONN_MAX_IDLE_TIME_SEC` | `300` | Maximum PostgreSQL idle time; `0` disables idle rotation |
| `PROXY_MAX_BUFFERED_RESPONSE_BYTES` | `20971520` | Maximum buffered non-streaming upstream response size; responses above the limit return 502 |
| `METAPI_ENABLE_PROXY_STUB` | _(empty)_ | Test/demo-only local proxy stub. Leave empty in production; unconfigured upstream forwarding returns 503 |
| `TRUSTED_PROXY_CIDRS` | _(empty)_ | Comma-separated reverse-proxy CIDRs allowed to supply `X-Forwarded-For` / `X-Real-IP`; forwarded headers are ignored when empty |
| `ADMIN_CORS_ALLOWED_ORIGINS` | _(empty)_ | Comma-separated exact `http(s)` browser origins allowed to call `/api/*`; empty keeps admin API same-origin only, and `*` is rejected |
| `REDIS_URL` / `METAPI_REDIS_URL` | _(empty)_ | Optional Redis for multi-instance shared downstream-key **RPM/TPM admission** only (`internal/sharedcount`; fail-open). Empty = process-local counters; no Redis process required. Does **not** enable sticky session multi-instance sharing |

## Docker Compose (Production)

1. Create a `.env` file:

```bash
AUTH_TOKEN=<your-admin-token>
PROXY_TOKEN=<your-proxy-token>
```

2. Start the service:

```bash
docker compose -f docker-compose.prod.yml up -d
```

3. Check health:

```bash
curl http://localhost:4000/health
# {"status":"ok"}
```

## Nginx Reverse Proxy

```nginx
server {
    listen 443 ssl http2;
    server_name metapi.example.com;

    ssl_certificate     /etc/letsencrypt/live/metapi.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/metapi.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:4000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE/streaming support
        proxy_buffering off;
        proxy_read_timeout 600s;
    }
}
```

If this service is reachable only through that proxy and admin IP allowlists depend on the client IP, set `TRUSTED_PROXY_CIDRS` to the proxy source CIDR. Leave it empty when clients can reach MetAPI directly.

## TLS with Let's Encrypt

```bash
# Install certbot
sudo apt install certbot python3-certbot-nginx

# Obtain certificate
sudo certbot --nginx -d metapi.example.com

# Auto-renewal (certbot timer is enabled by default)
sudo certbot renew --dry-run
```

## Bare-Metal Deployment

```bash
# 1. Build frontend
cd web && npm ci --ignore-scripts && npm run build:web && cd ..

# 2. Build server
go build -ldflags="-s -w" -o metapi ./cmd/server

# 3. Configure environment
export AUTH_TOKEN=<your-admin-token>
export PROXY_TOKEN=<your-proxy-token>
export DATABASE_URL='postgres://<user>:<password>@<host>:5432/<db>?sslmode=require'  # optional

# 4. Start server
./metapi
```

For container releases, the Dockerfile builds the React frontend inside a Node stage and copies `web/dist` into the Go build stage before `go:embed` runs. A clean checkout should pass `docker build -t metapi-go:ci .` without a pre-existing local `web/dist`.

## Database

### SQLite (default)

Data is stored in `$DATA_DIR/hub.db`. The directory is created automatically on first start. Auto-migration runs at startup. SQLite is intended for a single MetAPI process.

### PostgreSQL (production)

Set `DATABASE_URL` to switch to PostgreSQL:

```
DATABASE_URL='postgres://<user>:<password>@<host>:5432/metapi?sslmode=require'
```

For certificate validation, set `DB_SSLMODE=verify-full` and configure the PostgreSQL client certificate roots in the runtime environment.

Schema migrations run automatically at startup. Use `metapi-migrate` to transfer data from an existing SQLite database.

Use PostgreSQL for multi-instance deployments. Side-effecting schedulers use PostgreSQL advisory locks, so only one replica runs each job batch at a time. `admin-snapshot` remains process-local cache warming; `usage-aggregation` uses its own checkpoint lease.

Optional Redis (`REDIS_URL` / `METAPI_REDIS_URL`) is used only for multi-instance shared downstream-key **RPM/TPM admission** via `auth.ConfigureSharedAdmissionFromRedisURL` and `internal/sharedcount`. Admission is fail-open: if Redis is unreachable, counters fall back to process-local windows. Leave empty for single-node deployments — no Redis process is required. Sticky session bindings remain process-local; Redis does **not** share sticky maps across instances (STICKY-B is residual, not product). Details: [`docs/analysis/redis-shared-state.md`](analysis/redis-shared-state.md).

### Proxy Forwarding

Proxy routes require routing and upstream forwarding dependencies at runtime. If they are not wired, requests return HTTP 503 instead of a synthetic success response. `METAPI_ENABLE_PROXY_STUB=1` is limited to tests and demos.

## Backup Strategy

### SQLite

```bash
# Simple file copy (while server is running -- WAL mode safe)
cp data/hub.db "backups/hub-$(date +%Y%m%d-%H%M%S).db"

# With sqlite3 backup command
sqlite3 data/hub.db ".backup 'backups/hub-$(date +%Y%m%d-%H%M%S).db'"
```

### PostgreSQL

```bash
pg_dump -Fc 'postgres://<user>:<password>@<host>:5432/metapi?sslmode=require' > "backups/metapi-$(date +%Y%m%d-%H%M%S).dump"
```

## Monitoring

- Liveness check: `GET /health` returns `{"status":"ok"}` when the HTTP process is alive
- Readiness check: `GET /ready` returns `{"status":"ok","database":"ok"}` or HTTP 503 when the database is unavailable or the process is draining for shutdown
- Docker healthcheck runs `metapi healthcheck`, which polls `/ready` every 30 seconds by default
- Override the healthcheck target with `METAPI_HEALTHCHECK_URL` or `METAPI_HEALTHCHECK_PATH`
- Startup exits before binding the HTTP port when database bootstrap, runtime settings load, or runtime schema migration fails.
- Admin events are logged in the database `events` table
- Proxy request logs are stored in `proxy_logs`

## Browser CORS

- `/api/*` admin routes do not allow cross-origin browser access unless `ADMIN_CORS_ALLOWED_ORIGINS` is set.
- `/v1/*`, non-`/v1` proxy aliases, `/health`, `/ready`, and `/metrics` retain wildcard CORS for operational and downstream-client compatibility.
- Keep `ADMIN_CORS_ALLOWED_ORIGINS` to exact trusted origins, for example `https://admin.example.com`; wildcard origins, paths, query strings, and fragments are rejected at startup.

## Request Limits

- HTTP request bodies are capped at 20 MiB.
- Admin JSON decoders enforce the same cap, reject duplicate object keys, and reject trailing JSON values.
- WebDAV backup import downloads are capped at 64 MiB.

## Trusted Proxies

- `X-Forwarded-For` and `X-Real-IP` are ignored by default.
- Set `TRUSTED_PROXY_CIDRS` only to reverse-proxy source ranges you control.
- Admin IP allowlists and rate limits use the direct peer IP unless the peer matches `TRUSTED_PROXY_CIDRS`.

## Upgrading

```bash
# Pull new image
docker pull ghcr.io/tokendancelab/metapi-go:latest

# Restart with new image
docker compose -f docker-compose.prod.yml up -d
```

Docker Compose will automatically recreate the container with the new image. Auto-migration runs at startup to apply any new schema changes.
