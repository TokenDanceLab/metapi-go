# Milestones — Stack Modernization + Gap Inventory

**Program**: dual-track SDD for frontend stack modernization and original-metapi gap inventory  
**Project**: https://github.com/orgs/TokenDanceLab/projects/1  
**MASTER**: `docs/progress/MASTER.md`

| Milestone | Title | URL |
|:----------|:------|:----|
| **M-STACK** | Frontend stack: TS7 + React19 + Vite8 | https://github.com/TokenDanceLab/metapi-go/milestone/1 |
| **M-GAP** | Original metapi gap inventory (docs-only) | https://github.com/TokenDanceLab/metapi-go/milestone/2 |

## Scope boundary

This program **does not** implement original-gap product fixes.

| Track | In scope | Out of scope |
|:------|:---------|:-------------|
| M-STACK | Dependency bumps, React 19 test adaptation, Vite 8 tooling, CI/Docker/embed regression gate, CHANGELOG | Product features, backend Go feature work, original-gap bug fixes |
| M-GAP | Capture upstream open issues, taxonomy, capability gap matrix with code evidence, backlog issue publish, docs-only acceptance gate | Any product implementation, runtime behavior changes, closing gaps in code |

## Wave overview

| Wave | Status | Stack lane | Gap lane | Goal |
|:-----|:-------|:-----------|:---------|:-----|
| **Wave 0** | ✅ Done | Branch hygiene; labels; project; milestones; issues #3–#11 | same | Tracking surface ready |
| **Wave 1** | 🔄 Active | **S1** core lock (TS 7.0.2 + React 19.2.7 + Vite 8.1.5) | **G1** issue capture + taxonomy · **G2** gap matrix | Parallel stack bump + gap evidence base |
| **Wave 2** | Pending | **S2** React 19 test adaptation · **S3** Vite 8 / vitepress-mermaid tooling | **G3** publish `[backlog]` issues from matrix | Tests + tooling green; backlog visible |
| **Wave 3** | Pending | **S4** CI / Docker / embed regression gate + CHANGELOG | **G4** gap inventory acceptance (docs-only) | Ship stack gate; freeze inventory |

Parallelism notes:

- Wave 1 runs **S1 ∥ G1→G2** (independent file lanes).
- Wave 2 runs **S2/S3 ∥ G3**.
- Wave 3 runs **S4 + G4** as acceptance gates (stack may block on S2/S3; gap gate is docs-only).

## Issue map

### M-STACK — Frontend stack modernization

| Issue | ID | Wave | Size | Focus |
|------:|:---|:-----|:-----|:------|
| #3 | S0 | Wave 0→1 | S | Bootstrap MASTER + this plan (docs-only gate) |
| #4 | S1 | Wave 1 | M | Bump TS 7.0.2 + React 19.2.7 + Vite 8.1.5 (core lock) |
| #5 | S2 | Wave 2 | L | React 19 test adaptation (~79 `react-test-renderer` files) |
| #6 | S3 | Wave 2 | M | Vite 8 plugin / vitepress-mermaid tooling closure |
| #7 | S4 | Wave 3 | M | CI / Docker / embed regression gate + CHANGELOG |

### M-GAP — Original metapi gap inventory (docs-only)

| Issue | ID | Wave | Size | Focus |
|------:|:---|:-----|:-----|:------|
| #8 | G1 | Wave 1 | M | Capture original metapi open issues + taxonomy |
| #9 | G2 | Wave 1 | L | metapi-go capability gap matrix with code evidence |
| #10 | G3 | Wave 2 | M | Publish `[backlog]` GitHub issues from gap matrix |
| #11 | G4 | Wave 3 | S | Gap inventory acceptance (docs-only gate) |

## Target versions

| Version | Role relative to this program |
|:--------|:------------------------------|
| **v0.3.0** | Current released baseline (pre-stack program) |
| **v0.4.0** | Hardening target from prior program; stack may land as post-v0.4.0 patch/minor after **S4** |
| **post-S4 tag** | Frontend stack modernized (TS7 + React19 + Vite8) with CI/Docker/embed green |
| **post-G4** | No version bump required — inventory + backlog only |

Exact tag naming for the stack ship is decided at S4 (CHANGELOG + release notes). Gap track never requires a product release by itself.

## Out of scope (explicit)

1. Implementing any original-metapi product gap or parity feature under M-GAP.
2. Backend Go feature work, schema changes, or API contract changes driven by gap findings.
3. Closing backlog issues created by G3 within this program.
4. Unrelated dependency upgrades outside the S1 core lock and S3 tooling closure.
5. Force-push / rewrite of `main`/`master` history as part of stack bumps.

## Exit criteria

| Gate | Pass when |
|:-----|:----------|
| S0 | MASTER shows M-STACK / M-GAP; this plan exists; hygiene test green |
| S1 | Core lockfile/package versions at targets; frontend unit path still invocable |
| S2 | React 19 test suite adapted; no remaining broken `react-test-renderer` assumptions in scope |
| S3 | Vite 8 plugins / vitepress-mermaid tooling build cleanly |
| S4 | CI + Docker + embed regression green; CHANGELOG updated |
| G1 | Upstream open-issue capture + taxonomy documented |
| G2 | Gap matrix with code evidence checked in |
| G3 | Backlog issues published and linked from matrix |
| G4 | Docs-only acceptance: inventory complete, **no product code claimed as done** |

## Related docs

- Hardening milestones: `docs/plan/milestones-hardening.md`
- Rewrite milestones: `docs/plan/milestones.md`
- Progress SSOT: `docs/progress/MASTER.md`
