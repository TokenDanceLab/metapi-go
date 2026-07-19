# MASTER.md — MetAPI Go open gates

**Last verified**: 2026-07-19  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Mode**: GitHub Issues + Milestones (SDD) · product **maintenance**  
**Project**: https://github.com/orgs/TokenDanceLab/projects/1  
**Milestone**: [50](https://github.com/TokenDanceLab/metapi-go/milestone/50) closed with **v0.8.43**; **#531 closed** in **v0.8.44**

> **开放项 + 硬门禁**。现状 → [`../STATE.md`](../STATE.md) · 日志 → [`../log.md`](../log.md) · shortlist → [`../analysis/high-value-next.md`](../analysis/high-value-next.md)

## Open product board

| Issue | Track | Title |
|------:|:------|:------|
| — | UX | **M51 UI-REFRESH closed** on master (`168e8ee`, unreleased) — re-open only for live-shot/release residual |


## Hard gates

1. No invent WS-1 / STICKY-B / UC-1 without Milestone + ACs.  
2. **P0-585 stays partial** (unit load-proof ≠ production e2e).  
3. **P0-555 stays present-with-residual**.  
4. Pre-push: `go vet ./... && go test ./... -count=1 -race`.  
5. Ops pin SSOT: server `projects/metapi/STATE.md` (role LIMIT / pool may change fleet-wide).  
6. Ours residual ≠ original issues — use high-value-next.

## Optional next (not scheduled)

| Priority | Candidate |
|:---------|:----------|
| Reliability | P0-585 **production e2e** load-proof only |
| Billing | P0-555 media zeros / multi-instance lag |
| Product | #579 / #547 / #584 with ACs · WS-1 / STICKY-B / UC-1 |

## Quick status

```bash
gh issue list --state open --limit 20
gh pr list --state open
gh release view v0.8.43
gh project view 1 --owner TokenDanceLab
```
