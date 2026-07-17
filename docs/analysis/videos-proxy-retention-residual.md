# Residual: proxy_video_tasks retention / TTL / GC (#254)

**Date**: 2026-07-17  
**Issue**: [#254](https://github.com/TokenDanceLab/metapi-go/issues/254)  
**Lane**: p89 / residual honesty  
**SSOT code**: `handler/proxy/videos.go` (`SaveProxyVideoTask`, `upsertProxyVideoTaskDB`)  
**Parent residual**: [`videos-proxy-residual.md`](./videos-proxy-residual.md) (#225 / #235 / #244)

## Scope (this wave)

Honest documentation (and optional code comment) that durable rows in
`proxy_video_tasks` have **no TTL and no GC** in this wave.

**No** silent mass delete. **No** retention scheduler product. **No** API or
schema change.

## What exists today

| Surface | Behavior |
|---------|----------|
| Create success (`POST /v1/videos`) | `SaveProxyVideoTask` → memory map + best-effort `INSERT … ON CONFLICT DO UPDATE` into `proxy_video_tasks` |
| GET by publicId | memory, then durable cold load; does **not** expire rows |
| DELETE by publicId | clears memory + `DELETE FROM proxy_video_tasks` for that id only |
| Scheduler / prune job | **none** for video tasks |

Contrast: proxy **files** have a dedicated retention scheduler
(`scheduler/file_retention.go` / Proxy File Retention). Video task mapping rows
do **not** share that path.

Schema already indexes `created_at`
(`proxy_video_tasks_created_at_idx`), which is useful for a **future**
age-based delete, but nothing consumes that index for GC today.

## Honest residual

1. **No TTL.** Rows written by create rewrite stay until an explicit DELETE for
   that `public_id`, or manual DB intervention.
2. **No GC job.** There is no interval prune, lease-backed cleaner, or
   `file_retention`-like scheduler for `proxy_video_tasks`.
3. **Operators may accumulate rows.** Busy video create traffic grows the table
   unbounded; storage and backup size scale with create volume, not with active
   client polling.
4. **DELETE is client-driven only.** If clients never call `DELETE /v1/videos/{id}`,
   durable mappings remain even after upstream video objects are gone.
5. **Out of scope for #254:** product design for retention windows, purge
   policies, or mass delete tooling. Do **not** invent silent bulk purge here.

## Operator / risk note

| Risk | Why it matters | Mitigation this wave |
|------|----------------|----------------------|
| Table growth | Every successful create dual-writes a row | Monitor row count / DB size; optional manual SQL cleanup with ops review |
| Backup bloat | `proxy_video_tasks` is in backup export set | Same as growth; not auto-pruned |
| Stale publicId | Old publicIds still resolve via durable load | Accept residual; GET without map still passthrough |
| TokenValue sensitivity | Mapping stores token material in DB | Protect DB backups (already noted in parent residual) |

Recommended ops posture until a product retention wave lands:

- Treat `proxy_video_tasks` as an append-friendly mapping ledger, not a cache.
- Prefer client DELETE when video lifecycle ends.
- Any ad-hoc purge must be **explicit**, reviewed, and issue-backed — never a
  silent default.

## Future (explicitly out of scope)

Possible follow-ups (design required; not this residual):

1. **Age-based delete** using `created_at` (or `updated_at` / `last_polled_at`)
   with a configurable retention window.
2. **`file_retention`-like scheduler** that prunes completed/stale video task
   rows under multi-instance-safe rules (lease or single-writer ops).
3. **Status-aware GC** (e.g. only prune after terminal status + grace period)
   if status snapshot fields are populated productively.

None of the above are claimed by #244 durable mapping or by this honesty doc.

## Related

- Parent videos residual: `docs/analysis/videos-proxy-residual.md`
- Durable dual-write implementation: `handler/proxy/videos.go`
- Proxy file retention (different table): `scheduler/file_retention.go`
- Schema: `store/migrate.go` / `store/schema.go` table `proxy_video_tasks`

## Tests

No new behavior. Regression only:

```bash
test -f docs/analysis/videos-proxy-retention-residual.md
go test ./handler/proxy -count=1 -run Video
```
