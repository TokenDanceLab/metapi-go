# Admin API Reference

Base URL: `http://localhost:4000/api`

All admin endpoints require authentication via Bearer token:

```
Authorization: Bearer <AUTH_TOKEN>
```

## Response Format

### Success

```json
{
  "success": true,
  "data": { ... }
}
```

### Error

```json
{
  "success": false,
  "message": "Error description"
}
```

HTTP status codes: 200 (OK), 201 (Created), 202 (Accepted), 400 (Bad Request), 401 (Unauthorized), 404 (Not Found), 500 (Internal Server Error).

## Request Body Rules

Request bodies are capped at 20 MiB by the HTTP router. Admin JSON handlers also apply the same cap when decoding, so direct route handlers still read bounded input.

Admin JSON requests must contain one JSON value. Duplicate object keys and trailing JSON values are rejected with `400`.

---

## Stats & Dashboard

### GET /api/stats/dashboard

Returns the admin dashboard snapshot.

**Response**: Site/account/token counts, total cost, usage summaries.

### GET /api/stats/proxy-logs

Query proxy request logs.

**Query params**: `page`, `limit`, `status`, `model`, `client`, `from`, `to`

### GET /api/stats/proxy-logs/:id

Get a single proxy log entry by ID.

### GET /api/stats/site-distribution

Token distribution across sites.

### GET /api/stats/site-trend

Usage trend data by site.

### GET /api/stats/model-by-site

Model usage breakdown by site.

---

## Models & Routes

### GET /api/routes/lite

Lightweight route list (id, modelPattern, displayName, displayIcon, routeMode, routingStrategy, enabled).

### GET /api/routes/summary

Route list with channel counts and site names.

### GET /api/routes

Full route list with channels, accounts, and site information.

### GET /api/routes/:id/channels

Channels for a specific route.

### POST /api/routes

Create a new route.

### PUT /api/routes/:id

Update an existing route.

### DELETE /api/routes/:id

Delete a route and its channels.

### POST /api/routes/batch

Batch enable/disable/delete routes. Body: `{ "ids": [1, 2, 3], "action": "enable" }`.

### POST /api/routes/rebuild

Trigger route rebuild. Body: `{ "refreshModels": true }`.

### POST /api/routes/:id/cooldown/clear

Clear cooldown state for all channels on a route.

### POST /api/routes/:id/channels/batch

Batch add/update channels on a route.

### POST /api/routes/:id/channels

Add a single channel to a route.

### PUT /api/channels/batch

Batch update channel properties (weight, enabled, priority).

### PUT /api/channels/:channelId

Update a single channel.

### DELETE /api/channels/:channelId

Delete a channel.

### POST /api/admin/test-channel

Admin-only forced channel/site probe (competitive learn #119). Alias: `POST /api/debug/channel-probe`.

Body: `{ "channelId"?: number, "siteId"?: number, "model"?: string, "prompt"?: string, "mode"?: "chat"|"models", "timeoutMs"?: number }`.

Requires `channelId` or `siteId`. Forces one upstream request (no weighted selection). Returns `{ success, statusCode, latencyMs, truncatedBody, error, channelId, siteId, accountId, model, mode, bodyTruncated, ... }` with ~2 KiB redacted body summary. See `docs/analysis/admin-channel-test-harness.md`.

---

## Route Decision

### GET /api/routes/decision

Get route decision (which channel was selected for which model).

### POST /api/routes/decision/batch

Batch decision query for specific models.

### POST /api/routes/decision/by-route/batch

Batch decision query for specific routes.

### POST /api/routes/decision/route-wide/batch

Route-wide decision query.

### POST /api/routes/decision/refresh

Trigger decision snapshot refresh.

---

## Model Marketplace & Probing

### GET /api/models/marketplace

Available models by site.

### GET /api/models/price-compare

Cross-site effective model price comparison for operators.

Query: `model` (optional name/substring), `days` (default 30), `limit` (default 50), `topModels` (default 12 when model empty).

Returns `{ model, days, limit, sampleUsage, items: [{ siteId, siteName, platform, model, accountId, username, inputPerMillion, outputPerMillion, source, ratesSource, estimatedCostSample, observedSamples, configuredUnitCost, missingPrice }], meta }`.

`source` is one of `billing_details` | `observed` | `configured` | `fallback`. Fallback is always labeled; `missingPrice=true` when no catalog/observed/configured signal exists.

Alias: `GET /api/stats/model-prices` (same handler).

### GET /api/models/token-candidates

Available token candidates for route configuration.

### POST /api/models/check/:accountId

Check model availability for a specific account. Returns `{ "success": true, "models": [...] }`.

### POST /api/models/probe

Trigger a model probe. Body: `{ "models": ["gpt-4o"], "wait": false }`. Returns 202 with probe job.

---

## Proxy Debug

### GET /api/stats/proxy-debug/traces

List proxy debug traces.

### GET /api/stats/proxy-debug/traces/:id

Get a specific debug trace with related attempts.

---

## Sites

### GET /api/sites, POST /api/sites

List all sites. Create a new site.

### GET /api/sites/:id, PUT /api/sites/:id, DELETE /api/sites/:id

Get, update, delete a site.

---

## Accounts

### GET /api/accounts, POST /api/accounts

List all accounts. Create a new account.

### GET /api/accounts/:id, PUT /api/accounts/:id, DELETE /api/accounts/:id

Get, update, delete an account.

---

## Account Tokens

### GET /api/account-tokens, POST /api/account-tokens

List all account tokens. Create a new account token.

### GET /api/account-tokens/:id, PUT /api/account-tokens/:id, DELETE /api/account-tokens/:id

Get, update, delete an account token.

---

## Site Announcements

### GET /api/site-announcements

List site announcements.

### GET /api/site-announcements/:id

Get a single announcement.

### PUT /api/site-announcements/:id

Update announcement (e.g., mark as read/dismissed).

---

## Events

### GET /api/events

List system events (paginated). **Query params**: `page`, `limit`, `level`.

### GET /api/events/unread-count

Get count of unread events.

### POST /api/events/mark-all-read

Mark all events as read.

### PUT /api/events/:id/read

Mark a single event as read.

---

## Downstream API Keys

### GET /api/downstream-keys

List all downstream API keys.

### GET /api/downstream-keys/summary

Key list with usage summaries. **Query params**: `group`, `tags`, `tagMatch`.

### GET /api/downstream-keys/:id/overview

Usage overview for a specific key (24h, 7d, all-time).

### GET /api/downstream-keys/:id/trend

Usage trend data for a specific key.

### POST /api/downstream-keys

Create a new downstream API key.

**Body**:
```json
{
  "name": "My Key",
  "groupName": "production",
  "tags": "tag1,tag2",
  "supportedModels": ["gpt-4o", "claude-sonnet-4-20250514"],
  "allowedRouteIds": [1, 2],
  "maxCost": 100.0,
  "maxRequests": 10000,
  "expiresAt": "2026-12-31T23:59:59Z"
}
```

### PUT /api/downstream-keys/:id

Update a downstream API key.

### DELETE /api/downstream-keys/:id

Delete a downstream API key.

### POST /api/downstream-keys/:id/reset-usage

Reset usage counters (used_cost, used_requests) to zero.

---

## Settings

### GET /api/settings/runtime

Get all runtime settings. Returns 60+ fields. Sensitive values (proxyToken, tokens, passwords) are masked.

### PUT /api/settings/runtime

Update runtime settings. Partial update -- only send fields you want to change.

### GET /api/settings/brand-list

Get list of AI model brands.

### POST /api/settings/system-proxy/test

Test system proxy connectivity.

---

## Settings - Database

### GET /api/settings/database/runtime

Get database runtime state. `active` reports the database used by the current process, with PostgreSQL credentials masked. `saved` reports a restart-pending override from settings when one exists.

### PUT /api/settings/database/runtime

Save database configuration for the next restart. The response keeps `active` separate from `saved` so operators can see whether the process has switched yet.

### POST /api/settings/database/test-connection

Test a SQLite or PostgreSQL connection string. Returns `400` when the dialect is unsupported or the connection cannot be opened. Error messages mask credentials.

### POST /api/settings/database/migrate

Returns `501`. Runtime database migration is not wired into the admin API yet. Use `metapi-migrate` for SQLite to PostgreSQL migration.

---

## Settings - Backup

### GET /api/settings/backup/export

Export all settings and data as JSON.

### POST /api/settings/backup/import

Import settings and data from JSON. Runtime-local settings such as `auth_token`, database connection settings, and WebDAV sync state are skipped.

### GET /api/settings/backup/webdav

Get WebDAV backup configuration and last sync state. Passwords are never returned; use `hasPassword` and `passwordMasked` to show saved credential status.

The returned `state.lastSyncAt` is the last successful WebDAV import/export time. `state.lastAttemptAt` is the most recent attempt time, including failed attempts. `state.lastError` is set only when the latest attempt failed.

### PUT /api/settings/backup/webdav

Update WebDAV backup configuration. `fileUrl` must be an `http` or `https` URL without embedded userinfo. `exportType` supports `all`, `accounts`, or `preferences`.

### POST /api/settings/backup/webdav/export

Export a restorable backup payload to `fileUrl` with HTTP `PUT`. The payload uses the same `tables` structure as `GET /api/settings/backup/export`.

### POST /api/settings/backup/webdav/import

Download a backup payload from `fileUrl` with HTTP `GET` and import its `tables`. Runtime-local settings are skipped. The response includes imported row counts and updated sync state. The maximum downloaded backup size is 64 MiB.

---

## Settings - Notifications

### POST /api/settings/notify/test

Send a test notification.

---

## Settings - Maintenance

### POST /api/settings/maintenance/clear-cache

Clear model availability cache and rebuild routes. Returns deleted counts.

### POST /api/settings/maintenance/clear-usage

Clear all proxy usage data (proxy_logs, route_channel stats, account balanceUsed).

### POST /api/settings/maintenance/factory-reset

Reset all data to factory defaults.

---

## Checkin

### POST /api/checkin/run

Trigger manual checkin for all accounts.

### POST /api/checkin/reset-attempts

Reset checkin attempt tracking.

### PUT /api/checkin/schedule

Update checkin schedule (cron or interval mode).

---

## Update Center

### GET /api/update-center/status

Get update center status.

### POST /api/update-center/check

Trigger update center check.

---

## Monitor

### GET /api/monitor/status

System monitoring status.

---

## Search

### GET /api/search

Global search across sites, accounts, routes, and keys. **Query params**: `q`.

---

## Tasks

### GET /api/tasks

List background tasks.

### GET /api/tasks/:id

Get task status.

---

## Test

### POST /api/test/echo

Echo test endpoint.

---

## OAuth

### GET /api/oauth/providers

List OAuth providers.

### POST /api/oauth/authorize

Start OAuth authorization flow.

### GET /api/oauth/callback

OAuth callback endpoint.

---

## Auth Settings

### GET /api/auth/settings

Get authentication settings (admin IP allowlist, proxy token config).

### PUT /api/auth/settings

Update authentication settings.

---

## Health

### GET /health

Liveness check (no auth required). It does not touch dependencies and returns `{"status":"ok"}` when the HTTP process is alive.

### GET /ready

Readiness check (no auth required). It pings the active database and returns `200 {"status":"ok","database":"ok"}` when ready, `503 {"status":"degraded","database":"error"}` when the database is unavailable, or `503 {"status":"draining","database":"ok"}` while graceful shutdown is in progress.

### GET /api/desktop/health

Desktop health check. Returns `{"status":"ok"}`.

## Browser CORS

Admin routes under `/api/*` are same-origin by default. Set `ADMIN_CORS_ALLOWED_ORIGINS` to a comma-separated list of exact trusted `http(s)` browser origins only when the admin UI is hosted separately. Wildcards, paths, query strings, and fragments are rejected. Proxy routes and health/metrics endpoints retain wildcard CORS.

## Trusted Client IPs

Forwarded client IP headers are ignored by default. Set `TRUSTED_PROXY_CIDRS` only for reverse-proxy source ranges you control; admin IP allowlists and rate limits otherwise use the direct peer IP.


### GET /api/stats/usage-heatmap

Admin usage density cells (, ). Hard limit 2000 cells. See .

### GET /api/stats/slow-requests

Admin slow-request ranking from  (, , ).
