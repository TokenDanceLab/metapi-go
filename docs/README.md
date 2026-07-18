# docs/ — MetAPI Go documentation map

**Last updated**: 2026-07-18  
**Purpose**: one-screen orientation so humans and agents do not dig 180+ markdown files blindly.

## Progress SSOT roles

| File | Role | Not for |
|:-----|:-----|:--------|
| [`STATE.md`](STATE.md) | **现状** — verified product facts | Session diary, open TODO lists |
| [`progress/MASTER.md`](progress/MASTER.md) | **开放项 + 硬门禁** | Full changelog, ops host pins |
| [`log.md`](log.md) | **进度日志** (append-only) | Overriding STATE |
| Temporary HANDOFF / session summary | Disposable | **Never SSOT** — archive or delete after merge |

Ops live host/image: server `projects/metapi/STATE.md` (may lag repo tip).

## Start here

| If you need… | Read |
|:-------------|:-----|
| Current product status | [`STATE.md`](STATE.md) |
| Open gates / next | [`progress/MASTER.md`](progress/MASTER.md) |
| Progress timeline | [`log.md`](log.md) |
| Package architecture | [`architecture.md`](architecture.md) |
| Backend design rules | [`design/BACKEND.md`](design/BACKEND.md) |
| UI design system | [`design/DESIGN.md`](design/DESIGN.md) |
| What is still residual (not product) | [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) |
| Upstream parity matrix | [`analysis/original-gap-matrix.md`](analysis/original-gap-matrix.md) |
| Version history | root [`CHANGELOG.md`](../CHANGELOG.md) |
| Agent hard rules | root [`AGENTS.md`](../AGENTS.md) |
| Deploy / ops vars | [`deployment.md`](deployment.md) |
| HTTP API | [`api.md`](api.md) |

## Layout

```
docs/
  README.md                 ← this map
  STATE.md                  ← 现状 SSOT (keep slim)
  log.md                    ← progress log (append-only)
  architecture.md           ← as-built package & request path
  api.md                    ← public API notes
  deployment.md             ← run / Docker / ops vars
  migration.md              ← SQLite → PG / schema upgrade
  design/                   ← living design SSOT (BACKEND, DESIGN, a11y)
  analysis/                 ← evidence audits, residuals, gap matrix
    residual-next-candidates.md   ← residual honesty SSOT
    original-gap-matrix.md        ← parity evidence
    competitive/                  ← peer learning inventory
  plan/                     ← program / lane / roadmap (historical / closed)
  progress/                 ← MASTER only (open gates)
  specs/                    ← rewrite-era implementation specs (large; archival)
    review/                 ← historical audits/reviews
```

## Mental model (reduce load)

1. **Code is truth** for behavior; docs explain intent and residuals.
2. **STATE / MASTER / LOG** are three different jobs — do not merge them.
3. **Residuals stay honest**: 501 / process-local / design-only — no stub theater.
4. **`docs/specs/` is heavy rewrite history** — not a day-to-day runbook.
5. **One Issue per topic**; close duplicates the same day.
6. **HANDOFF is temporary** — never leave it as the only status file.

## Residual board (post v0.8.39 / M49 closed)

- **STATE**: [`STATE.md`](STATE.md) (tip may include post-tag commits such as #526).
- **Latest release tag**: [v0.8.39](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.39) · board empty.
- **Residual SSOT**: [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) (optional **v0.8.40+** only with ACs).
- **Still not product without ACs**: WS-1, STICKY-B, UC-1.
- **Keep partial**: P0-585 (load-proof required).

## Hygiene rules (short)

- Prefer **merge/update** over new parallel analysis files.
- Absolute dates (`2026-07-18`), not “recently”.
- Cross-link residual docs from `residual-next-candidates.md`.
- `docs/doc_hygiene_test.go` enforces public markdown hygiene (no local paths / false Redis sticky claims).

## Related program docs (historical)

| Doc | Role |
|:----|:-----|
| `plan/enterprise-program.md` | Closed enterprise program map |
| `plan/lane-charters.md` | File ownership for parallel WFs (ownership rules still useful) |
| `plan/feature-complete-roadmap.md` | F0 snapshot (closed / historical) |
| `plan/gap-inventory-acceptance.md` | G4 acceptance (closed) |
