# MASTER.md — MetAPI Go open gates

**Last verified**: 2026-07-18  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Mode**: GitHub Issues + Milestones (SDD) · product **maintenance**

> **开放项 + 硬门禁**（不是现状全文，也不是日志）。  
> 现状 → [`../STATE.md`](../STATE.md)  
> 时间线 → [`../log.md`](../log.md)  
> 文档地图 → [`../README.md`](../README.md)  
> 禁止把临时 HANDOFF / session 摘要当 SSOT。

## Open product board

| Issue | Track | Title |
|------:|:------|:------|
| — | — | **Empty** — no open residual product milestone |

## Hard gates (do not violate)

1. **No invent**: WS-1 / STICKY-B Redis sticky / UC-1 updateAvailable without dedicated Milestone + ACs.
2. **Honesty**: keep **P0-585 partial**; **P0-555 present-with-residual**.
3. **v0.8.40+** only with dedicated ACs — default is maintenance.
4. **Pre-push**: `go vet ./... && go test ./... -count=1 -race` (hook enforced).
5. **One Issue per topic**; no conflict markers in squash merges.
6. **Ops pin ≠ tip**: production image/host lives in server `projects/metapi/STATE.md`; do not claim fleet is on tip without ops evidence.

## Optional residual candidates (not scheduled)

SSOT inventory: [`../analysis/residual-next-candidates.md`](../analysis/residual-next-candidates.md)

| Priority if AC arrives | Candidate |
|:-----------------------|:----------|
| Reliability | P0-585 multi-channel load-proof |
| Billing polish | P0-555 residual media / multi-instance lag |
| Product | WS-1 · STICKY-B · UC-1 (each needs product AC) |

## Tracking surfaces

| Surface | Role |
|:--------|:-----|
| [`../STATE.md`](../STATE.md) | 现状 |
| This file | 开放项 + 硬门禁 |
| [`../log.md`](../log.md) | 进度日志（append-only） |
| GitHub Issues/Milestones | Task SSOT when board reopens |
| `CHANGELOG.md` | Release narrative |
| `AGENTS.md` | Engineering rules |

## Quick status commands

```bash
gh issue list --state open --limit 20
gh pr list --state open
gh release view v0.8.39
git log --oneline origin/master -10
```
