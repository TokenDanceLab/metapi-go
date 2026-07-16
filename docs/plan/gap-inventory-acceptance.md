# Gap Inventory Acceptance (G4)

> Acceptance date: 2026-07-17  
> Issue: [#11 G4](https://github.com/TokenDanceLab/metapi-go/issues/11)  
> Milestone: [M-GAP](https://github.com/TokenDanceLab/metapi-go/milestone/2)  
> Scope: **docs-only acceptance gate** — freeze the inventory; no product implementation claimed.

```
████████████████████████████████████████████████████████████████
█  G4 ACCEPTS THE INVENTORY, NOT PRODUCT FIXES.                █
█  Mandatory coverage is complete; backlog shells are filed.   █
█  Implementation is scheduled via M-FEATURE / individual PRs. █
████████████████████████████████████████████████████████████████
```

## Decision

**Accepted.** The original-metapi gap inventory track (G1→G3) is complete for M-GAP.

| Gate | Result |
| --- | --- |
| Mandatory source set present | **Pass** |
| Taxonomy vocabulary present | **Pass** |
| Evidence matrix present | **Pass** |
| Backlog shells filed & linked | **Pass** (`#36`–`#56`) |
| No product code claimed as G-track done | **Pass** |

Closing M-GAP means the **inventory is frozen as the planning SSOT**. It does **not** mean product gaps are fixed.

## Mandatory artifact set (on `master`)

| Artifact | Path | Gate | Role |
| --- | --- | --- | --- |
| Sources | `docs/analysis/original-gap-sources.md` | G1 (#8) | Upstream open issues/PRs capture |
| Taxonomy | `docs/analysis/original-gap-taxonomy.md` | G1 (#8) | Stable category vocabulary |
| Matrix | `docs/analysis/original-gap-matrix.md` | G2 (#9) | metapi-go evidence + status |
| Backlog | `docs/plan/original-gap-backlog.md` | G3 (#10) | Epic grouping + issue links |
| Acceptance | `docs/plan/gap-inventory-acceptance.md` | **G4 (#11)** | This document |

Verified present on `origin/master` at acceptance time (pre-this-commit for G1–G3; this commit adds G4).

## Coverage summary

### Mandatory high-value set

G1 mandatory numbers (29 upstream refs, all required to appear):

`#582, #568, #585, #573, #580, #590, #594, #591, #583, #579, #578, #570, #549, #547, #520, #584, #588, #550, #586, #577, #571, #569, #555, #526, #559, #496, #529, #538, #581`

| Check | Status |
| --- | --- |
| Present in sources inventory | **Complete** (`original-gap-sources.md` § Mandatory high-value numbers) |
| Mapped in taxonomy seed table | **Complete** (`original-gap-taxonomy.md`) |
| Evidence rows in matrix | **Complete** — matrix **mandatory row count: 29** |
| Status classified with code evidence | **Complete** (static `rg` evidence; no live traffic required for inventory) |

Mandatory status split (from matrix):

| status | Count | Notes |
| --- | ---: | --- |
| `present` | 9 | Already covered in metapi-go (not backlog shells) |
| `partial` | 13 | Surface exists; incomplete vs upstream intent |
| `missing` | 6 | No meaningful implementation found |
| `unknown-needs-runtime` | 1 | `#571` Codex OAuth gpt-5.5 |

### Broader inventory

| Metric | Value |
| --- | --- |
| Upstream open issues collected (G1) | 115 |
| Product-relevant open PRs included | 6 |
| Total source rows | 121 |
| Matrix rows (mandatory + additional sample) | 64 (29 mandatory) |

## Backlog issue range

G3 filed **21** `[backlog]` tracking shells for all **P0** and **high P1** matrix rows with `Backlog? = yes`.

| Range | Count | Priority mix |
| --- | ---: | --- |
| **[#36](https://github.com/TokenDanceLab/metapi-go/issues/36)–[#56](https://github.com/TokenDanceLab/metapi-go/issues/56)** | **21** | 11× P0 + 10× P1 |

Index (from `docs/plan/original-gap-backlog.md`):

| # | Pri | Epic | Upstream |
| ---: | --- | --- | ---: |
| 36–39 | P0 | Routing & failover | 568, 585, 387, 359 |
| 40–41 | P0 | Keys | 565, 405 |
| 42–44 | P0 | Stats | 555, 496, 491 |
| 45–46 | P0 | Admin UX | 515, 409 |
| 47–56 | P1 | Protocol | 580/581, 591, 571, 538, 531, 511, 507, 504, 489, 340 |

Labels on shells: `program:gap-inventory`, `status:backlog-only`, `priority:P0|P1`, `spec-driven`.

**Not incomplete:** Ops-checkin epic has no P0/P1 shells (matrix ops-checkin backlog rows are P3). P2–P5 product gaps remain in the deferred inventory section of the backlog doc.

Verify shells:

```bash
gh issue list -R TokenDanceLab/metapi-go --label status:backlog-only --limit 50
```

## What G4 does **not** accept

1. **Product code as done** — no Go/TS runtime behavior is claimed fixed by this gate.
2. **Closing backlog issues** — `#36`–`#56` stay open as tracking shells until real implementation PRs land.
3. **P2–P5 deferred rows** — remain inventory only until claimed by a feature/reliability wave.
4. **Architecture debt appendix** — metapi-go-native debt is out of M-GAP; own via backend/reliability lanes.

## Product fix scheduling (outside this gate)

Product and reliability work is **scheduled separately**, not under G4:

| Path | Owner | Notes |
| --- | --- | --- |
| **M-FEATURE** | Feature completeness | Roadmap: `docs/plan/feature-complete-roadmap.md` (F0 #23); milestone [M-FEATURE](https://github.com/TokenDanceLab/metapi-go/milestone/6) |
| Individual PRs | Any lane after schedule | One backlog shell → implementation PR with real AC; remove/replace `status:backlog-only` when work starts |
| CRITICAL reliability (B2 / R*) | Reliability / backend | May ship earlier than full M-FEATURE when correctness/failover demands it |

G4 freeze rule: inventory docs remain the planning SSOT; new upstream noise does not reopen M-GAP unless a deliberate re-inventory wave is opened.

## Exit criteria checklist (G4)

- [x] `docs/analysis/original-gap-sources.md` on master
- [x] `docs/analysis/original-gap-taxonomy.md` on master
- [x] `docs/analysis/original-gap-matrix.md` on master (mandatory set complete)
- [x] `docs/plan/original-gap-backlog.md` on master with issues **#36–#56**
- [x] `docs/plan/gap-inventory-acceptance.md` written (this file)
- [x] `docs/progress/MASTER.md` marks **M-GAP closed/accepted** with links
- [x] No product code in this acceptance change
- [x] Explicit: product fixes via **M-FEATURE / individual PRs**, not this gate

## Related docs

| Path | Role |
| --- | --- |
| `docs/plan/milestones-stack-gap.md` | M-STACK / M-GAP program plan; G4 exit criteria |
| `docs/plan/feature-complete-roadmap.md` | M-FEATURE scheduling after inventory |
| `docs/plan/enterprise-program.md` | Enterprise milestone map |
| `docs/progress/MASTER.md` | Progress SSOT |
