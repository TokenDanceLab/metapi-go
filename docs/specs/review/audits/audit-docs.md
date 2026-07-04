# Documentation Quality Audit: metapi-go

**Date**: 2026-07-04
**Scope**: README.md, docs/*.md, docs/specs/*.md, all Go source packages
**Auditor**: Automated documentation audit

---

## 1. doc.go Files -- CRITICAL

**Zero doc.go files exist across all 17 Go packages.**

Go convention requires each package to have a `doc.go` file (or at minimum a package-level doc comment in any file) to describe the package's purpose. None of the following packages have one:

| Package | Files | Has doc.go? | Has package comment? |
|---------|-------|-------------|---------------------|
| `app/` | 3 | No | Yes (app.go line 1: `package app`) |
| `auth/` | 8 | No | No |
| `config/` | 2 | No | No |
| `handler/admin/` | 18+ | No | No |
| `handler/proxy/` | 14+ | No | No |
| `platform/` | 18+ | No | No |
| `proxy/` | 10+ | No | No |
| `router/` | 2 | No | No |
| `routing/` | 14+ | No | No |
| `scheduler/` | 14+ | No | No |
| `service/` | 20+ | No | No |
| `service/oauth/` | 8+ | No | No |
| `service/checkin/` | 3+ | No | No |
| `service/notify/` | 3+ | No | No |
| `service/alert/` | 2 | No | No |
| `service/daily/` | 1+ | No | No |
| `store/` | 10+ | No | No |
| `transform/` | 14+ | No | No |

**Recommendation**: Create `doc.go` files for every package. This is standard Go practice and required by `go doc` for package overview text.

---

## 2. API Reference (docs/api.md) vs Actual Routes -- HIGH

The API reference is significantly out of date. Approximately **25-30 implemented endpoints are missing** from the documentation, and **5 endpoints have incorrect paths or methods**.

### 2.1 Path/Method Mismatches

| docs/api.md | Actual Code | Issue |
|---|---|---|
| `GET /api/events/unread-count` | `GET /api/events/count` | Path differs |
| `POST /api/events/mark-all-read` | `POST /api/events/read-all` | Path differs |
| `PUT /api/events/:id/read` | `POST /api/events/{id}/read` | Method differs (PUT vs POST) |
| `POST /api/checkin/run` | `POST /api/checkin/trigger` | Path differs |
| `GET /api/monitor/status` | `GET /api/monitor/config` | Path and purpose differ |

### 2.2 Endpoints Missing from docs/api.md

The following endpoints are registered in the Go code but not documented:

**Sites** (6 endpoints):
- `POST /api/sites/batch` -- batch enable/disable/delete
- `GET /api/sites/:id/disabled-models`
- `PUT /api/sites/:id/disabled-models`
- `GET /api/sites/:id/available-models`
- `POST /api/sites/:id/probe-now`
- `GET /api/sites/:id/probe-stream` (SSE)
- `POST /api/sites/detect` -- platform auto-detect

**Accounts** (9 endpoints):
- `POST /api/accounts/login` -- OAuth/session login
- `POST /api/accounts/verify-token`
- `POST /api/accounts/:id/rebind-session`
- `POST /api/accounts/batch` -- batch enable/disable/delete/refreshBalance
- `POST /api/accounts/health/refresh`
- `POST /api/accounts/:id/balance` -- refresh single account balance
- `GET /api/accounts/:id/models`
- `POST /api/accounts/:id/models/manual`

**Account Tokens** (5 endpoints):
- `POST /api/account-tokens/batch`
- `POST /api/account-tokens/:id/default` -- set default token
- `GET /api/account-tokens/:id/value` -- get token plaintext
- `POST /api/account-tokens/sync/:accountId`
- `POST /api/account-tokens/sync-all`

**Checkin** (1 endpoint):
- `POST /api/checkin/trigger/:id` -- single account trigger

**Downstream Keys** (1 endpoint):
- `POST /api/downstream-keys/batch` -- batch enable/disable/delete/resetUsage/updateMetadata

**Update Center** (4 endpoints):
- `PUT /api/update-center/config`
- `POST /api/update-center/deploy`
- `POST /api/update-center/rollback`
- `GET /api/update-center/tasks/:id/stream` (SSE stream for task logs)

**Monitor** (4 endpoints):
- `PUT /api/monitor/config` -- save LDOH cookie
- `POST /api/monitor/session` -- create monitor session
- `ALL /monitor-proxy/ldoh` -- LDOH reverse proxy (outside /api scope)
- `ALL /monitor-proxy/ldoh/*` -- wildcard LDOH proxy

**Site Announcements** (4 endpoints):
- `POST /api/site-announcements/:id/read`
- `POST /api/site-announcements/read-all`
- `DELETE /api/site-announcements`
- `POST /api/site-announcements/sync`

**Events** (1 endpoint):
- `DELETE /api/events` -- delete all events

### 2.3 Endpoints Documented but Not Verified in Code

- `GET /api/site-announcements/:id` -- claimed in api.md but not found in route registration
- `POST /api/checkin/reset-attempts` -- claimed in api.md but not found in route registration
- `GET /api/downstream-keys/:id/overview` -- not confirmed
- `GET /api/downstream-keys/:id/trend` -- not confirmed
- `POST /api/downstream-keys/:id/reset-usage` -- not confirmed

**Summary**: api.md documents approximately 68 endpoints. The actual codebase has approximately 95+ admin endpoints. The document is missing roughly 30% of the API surface.

---

## 3. Architecture Documentation (docs/architecture.md) -- HIGH

### 3.1 Package Layout Inaccuracy

The architecture diagram and package layout section describe a structure that diverges significantly from the actual code:

| Architecture.md | Actual Code | Severity |
|---|---|---|
| `proxycore/` with 8 sub-packages | `proxy/` (flat structure) | Critical |
| `protocol/` (protocol transformers) | `transform/` | Critical |
| `handler/admin/` (18 files, 110 endpoints) | `handler/admin/` (19+ files) | Minor |
| `scheduler/` (15 schedulers) | `scheduler/` (14 Go files) | Minor |
| Not mentioned | `routing/` package (token router, cache, matcher, selector, etc.) | Critical |
| Not mentioned | `service/` package (oauth, checkin, notify, alert, daily sub-packages) | High |
| Not mentioned | `e2e/` directory | Low |

### 3.2 proxycore/ vs proxy/ structural divergence

Architecture.md claims:
```
proxycore/
├── profiles/       # Platform profile detection
├── session/        # Request session management
├── retry/          # Retry policy engine
├── selector/       # Channel selection
├── endpoint/       # Endpoint flow execution
├── failure/        # Failure classification
├── surface/        # Response surface formatting
└── conductor/      # Orchestration conductor
```

Actual code (`proxy/`):
```
proxy/
├── types/types.go
├── profiles/
│   ├── registry.go
│   ├── codex.go
│   ├── claude_code.go
│   └── gemini_cli.go
├── channel_selection.go
├── conductor.go
├── detect.go
├── endpoint_flow.go
├── executor.go
├── failure_judge.go
├── profile.go
├── retry_policy.go
├── session.go
└── surface.go
```

The actual code is a flat package rather than deeply nested. Only `profiles/` and `types/` are subdirectories. All other concerns are single files at the package root.

### 3.3 routing/ package undocumented

The `routing/` package is a major architectural component containing:
- `router.go` -- TokenRouter (main routing engine)
- `matcher.go` -- model pattern matching
- `selector.go` -- channel selector
- `cache.go` -- route cache
- `workflow.go` -- route refresh workflow
- `decision.go` -- route decisions
- `snapshot.go` -- decision snapshots
- `cooldown.go` -- channel cooldown management
- `pricing.go` -- pricing integration
- `weights.go` -- routing weight configuration
- `round_robin.go` -- round-robin strategy
- `stable_first.go` -- stable-first strategy
- `route_units.go` -- OAuth route units
- `runtime_health.go` -- channel health tracking
- `ports.go` -- interface ports

None of this is mentioned in architecture.md, despite being central to the proxy flow.

### 3.4 transform/ vs protocol/ naming

Architecture.md mentions "Protocol Transformers" and a `protocol/` package. The actual package is `transform/` with sub-packages:
- `transform/canonical/` -- canonical types, envelope, OpenAI bridge
- `transform/openai/chat/` -- inbound OpenAI chat transform
- `transform/openai/completions/` -- completions request transform
- `transform/openai/embeddings/` -- embeddings request transform
- `transform/openai/images/` -- images request transform
- `transform/openai/responses/` -- responses compact transform
- `transform/anthropic/messages/` -- Anthropic messages conversion
- `transform/gemini/generate_content/` -- Gemini compatibility
- `transform/shared/` -- shared utilities

---

## 4. Public Function Documentation -- MEDIUM

### 4.1 Generally Adequate but Inconsistent

Most exported functions in major packages have doc comments. The `config/`, `routing/`, `proxy/`, and `auth/` packages show good documentation discipline. However:

- **handler/admin/ payloads** (`payloads/sites.go`, `payloads/accounts.go`, `payloads/account_tokens.go`): Request/response structs have minimal or no doc comments.
- **platform/ adapters**: Some adapter methods lack Go doc comments.
- **service/ sub-packages**: Inconsistent; some exported structs lack comments.
- **scheduler/**: Most scheduler functions have comments, but several helper and internal functions do not.

### 4.2 Count of Exported Functions (sampling)

| Package | Approx. Exported Functions | Fully Documented? |
|---------|---------------------------|-------------------|
| `proxy/` | ~30 exported items | Mostly yes |
| `routing/` | ~25+ exported items | Mostly yes |
| `config/` | 5 exported | Yes |
| `auth/` | ~10 exported | Yes |
| `store/` | ~15 exported | Partial |
| `platform/` | ~20 exported | Partial |
| `handler/proxy/` | ~12 exported | Partial |
| `handler/admin/` | 19+ Register* functions | Yes |

---

## 5. README.md -- LOW

### 5.1 Issues

- Claims "14 specification documents (P0-P13)" -- there are only 14 spec documents (P0-P13), which is correct.
- Lists docs/specs/ as "14 specification documents" but the directory also contains a `review/` subdirectory with ~28 additional files (impl-review and review documents) that are not mentioned.
- The "Features" section is accurate but does not mention the OAuth subsystem or monitor/LDOH proxy features.
- Quick start references `make web-build` and `make run` which work correctly.

---

## 6. Spec Documents (docs/specs/) -- LOW

### 6.1 Coverage

All 14 spec documents (P0 through P13) exist as expected. They are:
- P0: Go project skeleton + Config + Chi Router + Docker
- P1: DB Schema (27 tables) + SQLite/PG Migration
- P2: Auth Middleware (admin + downstream proxy)
- P3: Sites + Accounts + AccountTokens CRUD
- P4: Platform Adapters (14 platforms)
- P5: Checkin + Balance + Notify
- P6: OAuth Subsystem (4 Providers + Route Units)
- P7: Token Router (model matching + channel selection)
- P8: ProxyCore (orchestration core)
- P9: Protocol Transformers
- P10: Proxy Routes (v1/ handlers)
- P11: Admin API (Stats/Settings/Backup/etc.)
- P12: Schedulers (background tasks)
- P13: Embed + CI

### 6.2 Issues

- P8 (proxy-core) references a `proxycore/` directory structure that does not match the actual `proxy/` flat package structure.
- P9 (transformers) references a `protocol/` directory -- actual code uses `transform/`.
- P8 describes a complex sub-package layout (conductor, endpoint_flow, channel_selection, surface, session, failure_judge, etc. as separate packages) -- actual code has them as files within one `proxy/` package.
- P11's endpoint inventory is very detailed and accurate vs. the actual handler implementations.
- P12 (schedulers) references 15 schedulers -- actual `scheduler/` directory has 14 Go files. Some may be combined.

---

## 7. Other Docs Files -- LOW

| File | Status |
|------|--------|
| docs/deployment.md | Accurate. References current env vars and docker-compose setup. |
| docs/migration.md | Accurate. Describes TS-to-Go migration flow correctly. |
| docs/analysis/project-overview.md | Exists but not linked from README. |
| docs/analysis/module-inventory.md | Exists but not linked from README. |
| docs/analysis/risk-assessment.md | Exists but not linked from README. |
| docs/plan/task-breakdown.md | Exists but not linked from README. |
| docs/plan/dependency-graph.md | Exists but not linked from README. |
| docs/plan/milestones.md | Exists but not linked from README. |

---

## Summary by Severity

| Severity | Count | Key Issues |
|----------|-------|------------|
| **Critical** | 2 | Zero doc.go files; Architecture.md package layout is wrong (proxycore vs proxy, protocol vs transform, routing undocumented) |
| **High** | 3 | api.md missing ~30% of endpoints (25-30 undocumented); 5 endpoints with wrong path/method; routing package not in architecture docs |
| **Medium** | 2 | Inconsistent public function doc comments; service/ sub-package docs thin |
| **Low** | 4 | README missing docs/analysis and docs/plan links; OAuth not listed in features; spec P8/P9 use outdated package names; minor count discrepancies |

## Recommended Actions (Priority Order)

1. **Create doc.go files** for all 17 packages (CRITICAL -- Go standard practice)
2. **Update docs/architecture.md** to match actual package structure: proxy/ (not proxycore/), transform/ (not protocol/), add routing/ and service/ descriptions
3. **Update docs/api.md** with all 25-30 missing endpoints and fix the 5 path/method mismatches
4. **Update specs P8 and P9** to reference correct package names (proxy/, transform/)
5. **Add README links** to docs/analysis/ and docs/plan/ directories
6. **Audit and improve** doc comments on exported types in handler/admin/payloads/, platform/, and service/ sub-packages
