# MASTER.md — MetAPI Go open gates + parity program

**Last verified**: 2026-07-21  
**Repo**: https://github.com/TokenDanceLab/metapi-go  
**Mode**: **GITHUB_FULL** capable · product **parity program scheduled in docs** (board Issues TBD)  
**Project**: https://github.com/orgs/TokenDanceLab/projects/1  
**Tip**: `a87ef7e` · tag **v0.8.45** (RE2 + UI) · unreleased: parity KEYS/WS/#514/UC-1 + P0-555 media fold + cloud-ops + embed SPA honesty  
**Program plan**: [`../plan/original-parity-complete-2026-07-20.md`](../plan/original-parity-complete-2026-07-20.md)

> **开放项 + 硬门禁**。现状 → [`../STATE.md`](../STATE.md) · 日志 → [`../log.md`](../log.md) · shortlist → [`../analysis/high-value-next.md`](../analysis/high-value-next.md)

## Current status

| Fact | Value |
|:-----|:------|
| Active work | parity core shipped; docs honesty (formal-readiness/About); next P0-585 e2e / ops pin auth |
| User decisions | WS = **full TS parity**; sticky = **single-instance honesty**; UC = **hide/external deploy** |
| Ops | hk3 pin still **0.8.44 Exited** until authorized **0.8.45** soak (server STATE) |
| Board | empty of open feature issues (M52 closed); new wave not yet filed |

## Open product board

| Issue | Track | Title |
|------:|:------|:------|
| — | — | **empty** — parity core shipped; file REL/runtime probe issues only if scheduling work |

## Hard gates

1. **No invent** WS frames / Hijack-silent-close / fake updateAvailable / cluster sticky without AC.  
2. **WS-1 C1–C3 present** — C4 single-instance honesty only; no STICKY-B without reopen.  
3. **STICKY-B Redis deferred** (single-instance / LB pin honesty only).  
4. **UC-1**: hide or external ops deploy — **not** invent registry.  
5. **P0-585 stays partial** until production e2e.  
6. **P0-555 stays present-with-residual** (media detail fold present; multi-instance lag residual).  
7. Pre-push: `go vet ./... && go test ./... -count=1 -race`.  
8. Ops pin SSOT: server `projects/metapi/STATE.md`.  
9. Electron = **non-goal**.

## Scheduled next (from parity plan)

| Order | Wave | Work | Status |
|------:|:-----|:-----|:-------|
| 0 | DOC | Truth: #534/#520 present; matrix/MASTER/high-value | **this session** |
| 1 | KEYS | **#547** present · **#584** present · **#579** present | allow-list bind shipped |
| 2 | WS | **C1** upgrade+HTTP bridge → **C2** multi-turn → **C3** upstream wss | **C1+C2+C3 present** |
| 3 | ROUTE | **#514** multi-tier ctx | **present** |
| 4 | REL | P0-585 prod e2e · P0-555 residual | pending |
| 5 | UC | Hide/external Update Center honesty | **present** (UI external card + API residual) |
| ops | — | Pin/up **0.8.45** + ≥15min soak | needs admin auth |

## Optional / not blockers

| Priority | Candidate |
|:---------|:----------|
| Docs/visual | Empty-DB page shot recapture (`METAPI_UI_AUTH_TOKEN`) |
| UX | VIS-1 theme preset / NAV-1 first-run sidebar |
| Runtime | #571 Codex OAuth gpt-5.5 · #577 AnyRouter live |

## Quick status

```bash
gh issue list --state open --limit 20
gh pr list --state open
gh release view v0.8.45
gh project view 1 --owner TokenDanceLab
```

## Next agent

1. Read [`../plan/original-parity-complete-2026-07-20.md`](../plan/original-parity-complete-2026-07-20.md) + [`../STATE.md`](../STATE.md).  
2. REL: P0-585 needs **production e2e** (do not flip present from unit alone).  
3. Ops pin **0.8.45** only with admin auth + ≥15min soak.  
4. Do **not** invent UC registry / STICKY-B / fake WS terminals.
