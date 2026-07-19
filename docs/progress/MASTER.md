# MASTER.md — MetAPI Go open gates

**Last verified**: 2026-07-20  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Mode**: GitHub Issues + Milestones (SDD) · product **maintenance**  
**Project**: https://github.com/orgs/TokenDanceLab/projects/1  
**Milestone**: [52 UI-POLISH](https://github.com/TokenDanceLab/metapi-go/milestone/52) — Wave1 + first-run Wave2 closed; **v0.8.45** tagged (RE2 + UI)

> **开放项 + 硬门禁**。现状 → [`../STATE.md`](../STATE.md) · 日志 → [`../log.md`](../log.md) · shortlist → [`../analysis/high-value-next.md`](../analysis/high-value-next.md)

## Open product board

| Issue | Track | Title |
|------:|:------|:------|
| — | — | **empty** — M52 epic #548 closed; #553/#554 merged |

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
| **Ops** | Pin/up **0.8.45** on hk3 after GHCR + **≥15min soak** (受控停服; restart=no until authorized) |
| Docs/visual | Recapture empty-DB page shots after first-run (#553/#554); shots in repo still pre-Wave2 |
| UX | Optional VIS-1 theme preset / NAV-1 first-run sidebar — see [`ui-original-parity-2026-07-20.md`](../analysis/ui-original-parity-2026-07-20.md); CONSOLE-1 density/hi-res on tip |
| Reliability | P0-585 **production e2e** load-proof only |
| Billing | P0-555 media zeros / multi-instance lag |
| Product | #579 / #547 already closed · WS-1 / STICKY-B / UC-1 need ACs |

## Quick status

```bash
gh issue list --state open --limit 20
gh pr list --state open
gh release view v0.8.44
gh project view 1 --owner TokenDanceLab
```
