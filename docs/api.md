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

Get current database configuration (dialect, connection info).

### PUT /api/settings/database/runtime

Update database configuration.

### POST /api/settings/database/test-connection

Test a database connection string.

### POST /api/settings/database/migrate

Trigger cross-dialect data migration.

---

## Settings - Backup

### GET /api/settings/backup/export

Export all settings and data as JSON.

### POST /api/settings/backup/import

Import settings and data from JSON.

### GET /api/settings/backup/webdav

Get WebDAV backup configuration.

### PUT /api/settings/backup/webdav

Update WebDAV backup configuration.

### POST /api/settings/backup/webdav/export

Trigger WebDAV backup export.

### POST /api/settings/backup/webdav/import

Trigger WebDAV backup import.

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

Health check (no auth required). Returns `{"status":"ok"}`.

### GET /api/desktop/health

Desktop health check. Returns `{"status":"ok"}`.
