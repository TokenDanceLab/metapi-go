# Final Documentation Audit Report

**Date**: 2026-07-05  
**Scope**: README.md, README_CN.md, AGENTS.md, docs/api.md, docs/architecture.md, docs/deployment.md, docs/migration.md  
**Auditor**: Automated docs-check

---

## Severity Legend

| Level | Meaning |
|-------|---------|
| **CRITICAL** | Broken link, wrong env var, command that will not work |
| **HIGH** | Factual inconsistency across documents, missing content parity |
| **MEDIUM** | Misleading description, missing section, stale data |
| **LOW** | Formatting, grammar, minor polish |

---

## Findings

### C1. Docker badge version is stale (CRITICAL)

- **Files**: `README.md:9`, `README_CN.md:9`
- **Current text**: `ghcr-v0.2.0-blue`
- **Problem**: Git tags show latest is `v0.3.0`. The badge and its link target still reference v0.2.0.
- **Fix**: Update to `v0.3.0`.

### C2. Go version badge is stale (CRITICAL)

- **Files**: `README.md:7`, `README_CN.md:7`
- **Current text**: `Go-1.24`
- **Problem**: `go.mod` declares `go 1.25.0`. The badge shows an outdated Go version.
- **Fix**: Update to `Go-1.25`.

### C3. `DATABASE_URL` used instead of `DB_URL` in deployment.md and migration.md (CRITICAL)

- **Files**: `docs/deployment.md:29,102,119`, `docs/migration.md:136`
- **Problem**: The Go config only reads env var `DB_URL` (`config/config.go:359`). `DATABASE_URL` does not exist. Any user following these instructions will silently get SQLite instead of PostgreSQL, or the DB path will be empty.
- **Fix**: Replace all occurrences of `DATABASE_URL` with `DB_URL` in both files. Update the config table in deployment.md accordingly.

### C4. deployment.md bare-metal step 1 mandates frontend build, contradicting "pre-built and embedded" claim (CRITICAL)

- **File**: `docs/deployment.md:93-95`
- **Current text**: Step 1 shows `cd web && npm ci && npx vite build` as a prerequisite.
- **Problem**: `web/dist/` is already checked into the repository (contains `index.html`, `assets/`, etc.). README line 97 explicitly states "The frontend is pre-built and embedded -- just build and run". The deployment guide's step 1 is unnecessary and contradicts the README. A user without Node.js would be misled into thinking they cannot deploy.
- **Fix**: Change step 1 to explain the frontend build is only needed when modifying the SPA. The default flow should skip it.

### C5. architecture.md references wrong package name `proxycore/` (HIGH)

- **File**: `docs/architecture.md:54-62`
- **Current text**: Lists `proxycore/` with sub-packages (profiles, session, retry, selector, endpoint, failure, surface, conductor).
- **Problem**: The actual directory is `proxy/` (confirmed by filesystem). The sub-packages listed (`profiles/`, `session/`, etc.) do not exist as subdirectories under `proxy/` -- only `profiles/` and `types/` exist. The code is organized as flat `.go` files inside `proxy/`. Both `README.md:182` and `AGENTS.md:26` correctly refer to `proxy/`.
- **Fix**: Change `proxycore/` to `proxy/` and remove the fictional sub-package listing. Replace with a description of the flat file organization or list only the actual subdirectories (`profiles/`, `types/`).

### C6. Table count discrepancy: "18" vs "27" (HIGH)

- **Files**: `docs/migration.md:109` vs `README.md:68,188`, `README_CN.md:72,163`, `AGENTS.md:24`
- **Problem**: migration.md says "18 tables are transferred". All other docs say "27 tables". The full schema has 27 tables; the migration tool transfers 18 data tables (not system/config tables). This is correct behavior but the migration.md never explains why the number differs from the total table count mentioned everywhere else.
- **Fix**: Add a clarifying sentence: "The full schema comprises 27 tables; the migration tool transfers the 18 data-bearing tables."

### C7. Docker image size inconsistency: "~15 MB" vs "<25MB" (HIGH)

- **Files**: `README.md:34` ("~15 MB"), `README_CN.md:37` ("~15 MB") vs `docs/architecture.md:108` ("<25MB"), `docs/migration.md:203` ("<25MB")
- **Problem**: The README claims ~15 MB Docker image. Architecture and migration docs claim <25MB. These are significantly different numbers.
- **Fix**: Pick one accurate number and use it consistently. Measure the actual compressed image size from ghcr and use that.

### C8. Node.js memory baseline inconsistency: "85 MB" vs "~150MB+" (HIGH)

- **Files**: `README.md:34` ("85 MB"), `README_CN.md:37` ("85 MB") vs `docs/migration.md:199` ("~150MB+")
- **Problem**: Two different numbers for the TS version's memory usage. The 85 MB figure appears in the comparison table; 150MB+ appears in the migration differences table. This undermines the performance claims.
- **Fix**: Use the same number in both places, or clarify that one is idle and one is under load.

### C9. Docker Compose command discrepancy (HIGH)

- **Files**: `README.md:107-108`, `README_CN.md:111-112` vs `docs/deployment.md:42-43`
- **Problem**: README quick start uses `docker compose up -d` (defaults to `docker-compose.yml`, a minimal 179-byte file). Deployment guide uses `docker compose -f docker-compose.prod.yml up -d` (1049 bytes, proper production config). The README quick start produces a different (likely incomplete) deployment than the deployment guide.
- **Fix**: README quick start should use `docker compose -f docker-compose.prod.yml up -d` or at minimum mention both files and their purposes.

### C10. AGENTS.md contains non-portable Windows absolute paths (HIGH)

- **File**: `AGENTS.md:8,70,71,72`
- **Current text**: `D:\Code\TokenDance\metapi\src\server\`, `D:\Code\TokenDance\tokendance-gateway`, `C:\Users\Ding\.claude\skills\metapi\SKILL.md`
- **Problem**: These are absolute Windows paths specific to one developer's machine. They will be dead references for anyone else cloning this repository. This breaks the "Related Repos" and "Golden Rules" sections.
- **Fix**: Replace with relative paths from the repo root (e.g., `../metapi/src/server/`) or GitHub URLs.

### C11. README EN missing "Project Structure" section (MEDIUM)

- **File**: `README.md` (compare to `README_CN.md:158-175`)
- **Problem**: README_CN has a full directory tree under "项目结构" showing `cmd/server/`, `cmd/migrate/`, `config/`, `store/`, etc. The English README jumps directly from Configuration to Documentation with no equivalent section.
- **Fix**: Add a "Project Structure" section to README.md, translated from the CN version but using the correct package name (`proxy/` not some other name).

### C12. README CN has extra feature bullet not in EN (MEDIUM)

- **File**: `README_CN.md:77` vs `README.md`
- **Current text (CN only)**: "15 个后台调度器覆盖签到、余额、探测、清理、聚合"
- **Problem**: This bullet about 15 background schedulers exists in CN Features but is absent from the EN version. Content parity between the two README files should be maintained.
- **Fix**: Add the equivalent English bullet to README.md Features > Operations section.

### C13. architecture.md says `web/dist/` is "gitignored, generated" but it is checked in (MEDIUM)

- **File**: `docs/architecture.md:69`
- **Current text**: `web/dist/ — Built React SPA (gitignored, generated)`
- **Problem**: `web/dist/` exists in the repository with real built files. The `.gitignore` does not exclude it. The CI even does `mkdir -p web/dist` to ensure it exists for go:embed. This annotation is simply wrong.
- **Fix**: Change to "Built React SPA (pre-built, embedded via go:embed, rebuild with `make web-build`)".

### C14. migration.md sample output has impossible timestamp (MEDIUM)

- **File**: `docs/migration.md:88`
- **Current text**: `timestamp: 1720000000000`
- **Problem**: 1720000000000 ms = July 3, 2024. This is visibly stale placeholder data in a document last modified 2026-07-04. It undermines trust in the accuracy of the sample output.
- **Fix**: Use a realistic recent timestamp, or replace with a placeholder like `<current-unix-ms>`.

### C15. deployment.md and README config tables use different variable sets (MEDIUM)

- **Files**: `docs/deployment.md:13-29` vs `README.md:142-151`
- **Problem**: The deployment guide lists `DATA_DIR`, `DATABASE_URL` (wrong, see C3), and `TZ`. The README lists `DB_TYPE`, `DB_URL`, `PORT`. Neither is complete; they show different subsets of the ~100 available variables. The README table doesn't include `DATA_DIR` at all. The deployment guide's table doesn't include `PORT` or `DB_TYPE`.
- **Fix**: Unify the config tables. Both should show the primary variables consistently, with a link to `.env.example` for the full list.

### C16. README quick start `go build` lacks optimization flags (LOW)

- **Files**: `README.md:98`, `README_CN.md:101` vs `Makefile:5`
- **Current**: `go build -o metapi ./cmd/server`
- **Makefile**: `go build -trimpath -ldflags="-s -w" -o metapi ./cmd/server`
- **Problem**: The quick start produces a larger, non-reproducible binary compared to the Makefile target. The `-ldflags="-s -w"` strips debug info (significant size reduction) and `-trimpath` enables reproducible builds.
- **Fix**: Align the quick start command with the Makefile, or explain the flags are optional for development.

### C17. README does not mention MySQL was dropped (LOW)

- **Files**: `README.md`, `README_CN.md` vs `docs/architecture.md:105`, `docs/migration.md:202`
- **Problem**: The architecture comparison and migration guide explicitly note "MySQL support: No (SQLite + PG only)". The README's feature list and migration section never mention this dropped support, which could surprise TS users migrating from a MySQL deployment.
- **Fix**: Add a note to the README migration section that MySQL is not supported in the Go version.

### C18. Command examples assume `AUTH_TOKEN` and `PROXY_TOKEN` are set but quick start hardcodes (LOW)

- **Files**: `README.md:99` vs `docs/migration.md:137`, `docs/deployment.md:100-101`
- **Problem**: README quick start uses `AUTH_TOKEN=admin PROXY_TOKEN=sk-proxy ./metapi` (inline). Migration and deployment guides use `export AUTH_TOKEN=...` followed by `./metapi`. Both work, but the inconsistency in style across documentation is confusing. More importantly, the deployment guide shows `export DATABASE_URL=...` (see C3).
- **Fix**: Normalize to one style and fix C3.

---

## Link Validation

All internal document links resolve correctly:

| Link | Target | Status |
|------|--------|--------|
| `docs/deployment.md` (from README:158) | Exists | PASS |
| `docs/architecture.md` (from README:159) | Exists | PASS |
| `docs/api.md` (from README:160) | Exists | PASS |
| `docs/migration.md` (from README:161) | Exists | PASS |
| `docs/specs/` (from README:162) | Exists (14 p*.md files) | PASS |
| `.env.example` (from README:152) | Exists | PASS |
| `[English](README.md)` (from README_CN:16) | Exists | PASS |
| `docs/plan/` (from AGENTS.md:63) | Exists (3 files) | PASS |
| `docs/progress/MASTER.md` (from AGENTS.md:64) | Exists | PASS |
| `docs/analysis/` (from AGENTS.md:65) | Exists (3 files) | PASS |
| External: GitHub repo links | TokenDanceLab org | PASS |
| External: `cita-777/metapi` link | GitHub | PASS |

**No broken links found.**

---

## Grammar and Style Issues

1. **README.md:62** -- "Sticky sessions for conversation continuity" -- fine.
2. **README_CN.md:60** -- "含排队" sounds awkward. Consider "含排队机制".
3. **docs/api.md** -- No issues found. Consistent format throughout.
4. **docs/deployment.md:17-18** -- "Server exits if missing" is duplicated verbatim for both `AUTH_TOKEN` and `PROXY_TOKEN`. Fine but could be more descriptive.
5. **docs/migration.md:63-93** -- The sample migration output includes a visual progress bar with `...`. This is fine as sample output.

---

## Summary

| Severity | Count | Key Themes |
|----------|-------|------------|
| CRITICAL | 5 | Stale badges (v0.2.0->v0.3.0, Go 1.24->1.25), wrong env var `DATABASE_URL`, contradictory frontend build requirement |
| HIGH | 5 | Package name `proxycore/` vs `proxy/`, table count 18 vs 27, Docker image size 15MB vs 25MB, memory baseline 85MB vs 150MB, Docker Compose file mismatch, Windows paths |
| MEDIUM | 6 | EN/CN content parity gaps, misleading `web/dist` gitignore claim, stale timestamp, config table divergence |
| LOW | 3 | Missing optimization flags, no MySQL-dropped mention, command style inconsistency |

**Overall**: No broken links. The most impactful issues are C3 (wrong env var -- users following deployment.md will get broken PostgreSQL config) and C4 (phantom Node.js dependency in deployment steps). The architecture.md `proxycore/` error (C5) also needs immediate correction as it misrepresents the codebase structure. The two README files need a synchronization pass to ensure EN/CN parity.
