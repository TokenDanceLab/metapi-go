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
| Open gates / next gates | [`progress/MASTER.md`](progress/MASTER.md) |
| High-value next (ours vs original) | [`analysis/high-value-next.md`](analysis/high-value-next.md) |
| Formal readiness (对内正式 vs 对外完备) | [`analysis/formal-readiness.md`](analysis/formal-readiness.md) |
| UI/UX refresh design | [`analysis/ui-ux-refresh.md`](analysis/ui-ux-refresh.md) |
| Progress timeline | [`log.md`](log.md) |
| Package architecture | [`architecture.md`](architecture.md) |
| Backend design rules | [`design/BACKEND.md`](design/BACKEND.md) |
| UI design system | [`design/DESIGN.md`](design/DESIGN.md) |
| Full residual honesty inventory (ours) | [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md) |
| Upstream parity evidence matrix | [`analysis/original-gap-matrix.md`](analysis/original-gap-matrix.md) |
| Upstream sources snapshot | [`analysis/original-gap-sources.md`](analysis/original-gap-sources.md) |
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
    high-value-next.md            ← next-wave shortlist (ours vs original)
    residual-next-candidates.md   ← residual honesty SSOT (ours)
    db-pool-budget.md             ← PG pool profiles / #531
    formal-readiness.md           ← Track A/B 正式可用门禁
    ui-ux-refresh.md              ← UI 重构方向（GCP/glass/Apple）
    original-gap-matrix.md        ← parity evidence (historical; see banner)
    original-gap-sources.md       ← upstream snapshot 2026-07-16
    competitive/                  ← peer learning inventory
  plan/                     ← program / lane / roadmap (historical / closed)
  progress/                 ← MASTER only (open gates)
  specs/                    ← rewrite-era implementation specs (large; archival)
    review/                 ← historical audits/reviews
```

## Mental model (reduce load)

1. **Code is truth** for behavior; docs explain intent and residuals.
2. **STATE / MASTER / LOG** are three different jobs — do not merge them.
3. **Ours residual ≠ original issues** — shortlist separates them in `high-value-next.md`.
4. **Residuals stay honest**: 501 / process-local / design-only — no stub theater.
5. **`docs/specs/` is heavy rewrite history** — not a day-to-day runbook.
6. **One Issue per topic**; close duplicates the same day.
7. **HANDOFF is temporary** — never leave it as the only status file.

## Residual board (post v0.8.43 / M50)

- **STATE**: [`STATE.md`](STATE.md) — latest tag **v0.8.44**.
- **Next shortlist**: [`analysis/high-value-next.md`](analysis/high-value-next.md).
- **Residual SSOT**: [`analysis/residual-next-candidates.md`](analysis/residual-next-candidates.md).
- **Still not product without ACs**: WS-1, STICKY-B, UC-1.
- **Keep partial**: P0-585 (production e2e load-proof); P0-555 present-with-residual.

## Hygiene rules (short)

- Prefer **merge/update** over new parallel analysis files.
- Absolute dates (`2026-07-18`), not “recently”.
- Cross-link residual docs from `residual-next-candidates.md` / `high-value-next.md`.
- `docs/doc_hygiene_test.go` enforces public markdown hygiene (no local paths / false Redis sticky claims).

## Related program docs (historical)

| Doc | Role |
|:----|:-----|
| `plan/enterprise-program.md` | Closed enterprise program map |
| `plan/lane-charters.md` | File ownership for parallel WFs (ownership rules still useful) |
| `plan/feature-complete-roadmap.md` | F0 snapshot (closed / historical) |
| `plan/gap-inventory-acceptance.md` | G4 acceptance (closed) |

- P0-585 live e2e procedure: [`analysis/p0585-production-e2e-procedure.md`](analysis/p0585-production-e2e-procedure.md) (#557)
