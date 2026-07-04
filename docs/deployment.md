# Deployment Guide

## Prerequisites

- Docker (for containerized deployment)
- Go 1.24+ (for bare-metal deployment)
- Node.js 20+ (for frontend build)
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
| `DATABASE_URL` | _(empty)_ | PostgreSQL connection string; when set, uses PG instead of SQLite |

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
cd web && npm ci && npx vite build && cd ..

# 2. Build server
go build -ldflags="-s -w" -o metapi ./cmd/server

# 3. Configure environment
export AUTH_TOKEN=<your-admin-token>
export PROXY_TOKEN=<your-proxy-token>
export DATABASE_URL=postgres://user:pass@host:5432/db  # optional

# 4. Start server
./metapi
```

## Database

### SQLite (default)

Data is stored in `$DATA_DIR/hub.db`. The directory is created automatically on first start. Auto-migration runs at startup.

### PostgreSQL (production)

Set `DATABASE_URL` to switch to PostgreSQL:

```
DATABASE_URL=postgres://user:password@host:5432/metapi?sslmode=require
```

Schema migrations run automatically at startup. Use `metapi-migrate` to transfer data from an existing SQLite database.

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
pg_dump -Fc postgres://user:pass@host:5432/metapi > "backups/metapi-$(date +%Y%m%d-%H%M%S).dump"
```

## Monitoring

- Health check: `GET /health` returns `{"status":"ok"}`
- Docker healthcheck polls `/health` every 30 seconds
- Admin events are logged in the database `events` table
- Proxy request logs are stored in `proxy_logs`

## Upgrading

```bash
# Pull new image
docker pull ghcr.io/tokendancelab/metapi-go:latest

# Restart with new image
docker compose -f docker-compose.prod.yml up -d
```

Docker Compose will automatically recreate the container with the new image. Auto-migration runs at startup to apply any new schema changes.
