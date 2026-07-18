# docs/ — MetAPI Go documentation map

**Last updated**: 2026-07-18  
**Purpose**: one-screen orientation so humans and agents do not dig 180+ markdown files blindly.

## Start here

| If you need… | Read |
|:-------------|:-----|
| Session / program status | [`progress/MASTER.md`](progress/MASTER.md) |
| Package architecture | [`architecture.md`](architecture.md) |
| Backend design rules | [`design/BACKEND.md`](design/BACKEND.md) |
| UI design system | [`design/DESIGN.md`](design/DESIGN.md) |
| What is still residual (not product) | [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) |
| Upstream parity matrix | [`analysis/original-gap-matrix.md`](analysis/original-gap-matrix.md) |
| Version history | root [`CHANGELOG.md`](../CHANGELOG.md) |
| Agent hard rules | root [`AGENTS.md`](../AGENTS.md) |
| Deploy / ops | [`deployment.md`](deployment.md) |
| HTTP API | [`api.md`](api.md) |

## Layout

```
docs/
  README.md                 ← this map
  architecture.md           ← as-built package & request path
  api.md                    ← public API notes
  deployment.md             ← run / Docker / ops
  migration.md              ← SQLite → PG / schema upgrade
  design/                   ← living design SSOT (BACKEND, DESIGN, a11y)
  analysis/                 ← evidence audits, residuals, gap matrix
    residual-next-candidates.md   ← residual honesty SSOT (read often)
    original-gap-matrix.md        ← parity evidence
    competitive/                  ← peer learning inventory
  plan/                     ← program / lane / roadmap (historical / closed)
  progress/                 ← MASTER index only (keep slim)
  specs/                    ← rewrite-era implementation specs (large; archival)
    review/                 ← historical audits/reviews
```

## Mental model (reduce load)

1. **Code is truth** for behavior; docs explain intent and residuals.
2. **Residuals stay honest**: 501 / process-local / design-only — no stub theater.
3. **MASTER is a pointer**, not a changelog. Releases → `CHANGELOG.md` + GitHub Release.
4. **`docs/specs/` is heavy rewrite history** — do not treat as day-to-day runbook.
5. **One Issue per topic**; close duplicates the same day.

## Residual board (post v0.8.36)

- **Latest release**: [v0.8.36](https://github.com/TokenDanceLab/metapi-go/releases/tag/v0.8.36) · M46 closed · active milestone **none**.
- **Residual SSOT**: [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) (optional **v0.8.37+** only with dedicated ACs).
- **Still not product without ACs**: full Responses WS (WS-1), Redis sticky (STICKY-B), update-center remote deploy (UC-1).
- **Keep partial**: P0-585 cascade (load-proof required; honesty tests do not flip present).

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
