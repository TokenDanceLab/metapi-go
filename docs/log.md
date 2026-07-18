# log.md — MetAPI Go progress log

> **进度日志**（append-only）。不是现状 SSOT。  
> 现状 → [`STATE.md`](STATE.md) · 开放项 → [`progress/MASTER.md`](progress/MASTER.md)

## [2026-07-18] v0.8.42 cron validation + prod roll-forward

- Fix: config `validateCronExpr` accepts default 5-field crons (parity with scheduler normalize).
- Ship/tag v0.8.42; deploy hk3 pin 0.8.42; generate `ACCOUNT_CREDENTIAL_SECRET` when missing (no OAuth client invent).
- Residual: OAuth client placeholders remain intentional until real client IDs are configured.

## [2026-07-18] deploy v0.8.41 to hk3 (0.6.5 → 0.8.41)

- Tags: v0.8.40 (PG pool + docs) · **v0.8.41** (request_id index upgrade fix for old DBs).
- Prod: Azure PG `tokendance-pg` / role `metapi`; container `td-metapi` healthy; migrations sc2_001–006 applied.
- Ops fix: role CONNECTION LIMIT 2→15; app pool max_open=5 idle=2.
- Evidence: `/health` `/ready database=ok`; admin auth OK; 103 sites; public 302 to ID.

## [2026-07-18] neat-freak: STATE/MASTER/LOG roles + branch hygiene

- Closed M49 / shipped **v0.8.39**; board empty.
- Post-tag **#526** landed on master: explicit PostgreSQL pool budget (config + store + docs).
- Progress docs split: **STATE** = 现状, **MASTER** = 开放门禁, **LOG** = 本文件; no HANDOFF SSOT.
- Pruned ~255 agent worktrees → main only; deleted merged-PR remote heads (~200+) and abandoned leftovers; local non-master cleaned.
- Memory pointer updated for metapi-go docs map.

## [2026-07-18] v0.8.39 / M49 adversarial bugfix residual

- Product: RR fail-count, used_requests 429 order, Redis admit rollback, max_cost wire, Gemini path/stream, retention RFC3339 (#511–#516).
- Docs honesty #517; release docs #525; tag + GitHub Release published; Milestone 49 closed.

## Earlier residual train

- v0.8.18–v0.8.38 narrative: root `CHANGELOG.md` + GitHub Releases (do not duplicate here).
