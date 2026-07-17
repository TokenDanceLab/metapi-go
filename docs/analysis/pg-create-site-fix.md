# PostgreSQL CreateSite LastInsertId flake (#204)

Last updated: 2026-07-17

## Symptom

CI `test-pg` / release-gate intermittently (often always under shared PG) failed:

```
TestSites_Postgres_CreateUpdateAndDisabledModels
postgres create site: expected 200, got 500: {"error":"Create site failed"}
```

Also hit token-routes PG tests that create sites with endpoints via API.

## Root cause

`service.CreateSite` used `tx.Exec` + `LastInsertId()`:

- **SQLite**: LastInsertId works.
- **PostgreSQL (lib/pq / pgx)**: LastInsertId is unsupported; the code fell back to a post-insert `SELECT id ... ORDER BY id DESC` **without checking Get error**, so a failed/empty select left `siteID=0`.
- Subsequent `UpsertSiteAPIEndpoints` then inserted `site_api_endpoints.site_id = 0`, violating FK → transaction error → handler maps to `"Create site failed"`.

Race with concurrent site creates could also make the SELECT-by-name/url/platform ambiguous under shared DBs.

## Fix

Insert with `RETURNING id` scanned via `QueryRowx` inside the open transaction so both dialects return the real PK before endpoint rows are written. Reject `siteID <= 0` explicitly.

## Verify

```bash
go test ./service ./handler/admin -count=1 -run 'Site|CreateSite'
# with PG_TEST_DSN set:
go test ./handler/admin -count=1 -run 'Sites_Postgres'
```
