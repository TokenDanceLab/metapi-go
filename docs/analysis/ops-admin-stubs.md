# Ops admin stubs residual (#158)

**Date**: 2026-07-17  
**Issue**: [#158](https://github.com/TokenDanceLab/metapi-go/issues/158)  
**Lane**: `lane:ops-admin`

## Scope

Replace remaining admin ops stubs that previously returned fake success:

| Endpoint | Previous stub | New behavior |
|----------|---------------|--------------|
| `POST /api/settings/notify/test` | Always `success:true` with `0/0` | Real `notify.SendNotification` with `bypassThrottle/requireChannel/throwOnFailure`; clear `400` when no channel configured or all sends fail |
| `PUT /api/monitor/config` | Fake success, never persisted | Validates + persists `monitor_ldoh_cookie` setting; clear cookie supported |
| `ALL /monitor-proxy/ldoh*` | Fake/partial 400 stub | Requires monitor session cookie + configured LDOH cookie; reverse-proxies `https://ldoh.105117.xyz` with HTML/JS/CSS/JSON rewrite |
| `GET /api/tasks` / `GET /api/tasks/:id` | Always empty / not found | In-memory background task registry with camelCase schema; empty list is honest success |
| `POST /api/site-announcements/sync` | Fake `taskId:"stub"` | Queues real background task (`site-announcements-sync`) that pulls announcements via platform adapters |

## Residuals / honest limits

1. **Notify test** depends on operator-configured channels (webhook/bark/serverchan/telegram/smtp). With none configured, response is `400` + `no notification channels configured` (not silent success).
2. **LDOH reverse proxy** needs a valid operator-provided `ld_auth_session` cookie and a prior `POST /api/monitor/session`. Upstream network failures return `502`, never fake `success:true`.
3. **Background task registry** is process-local (in-memory), matching the TS service model. Multi-instance deployments do not share task state.
4. **Announcement sync** is best-effort per site: unsupported platforms increment `unsupported`; adapter/network failures are recorded in `failedSites` inside the task result rather than inventing a queued success without work.

## Tests

`handler/admin/ops_admin_stubs_test.go` covers:

- notify test 400 when unconfigured
- monitor config persist/mask + invalid cookie 400
- LDOH proxy 401 (no session) / 400 (no cookie) without fake success
- tasks empty schema + camelCase list/get after `StartBackgroundTask`
- announcements sync returns real `taskId` and completes a registry task
