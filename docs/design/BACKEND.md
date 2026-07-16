# MetAPI-Go — Backend Design Philosophy (BACKEND.md)

**Status**: Draft skeleton — Lane **BACKEND** / Issue **#16 B0** owns completion  
**Last updated**: 2026-07-16

## Principles

1. **Single binary** — React SPA prebuilt + `go:embed`; no Node in production image  
2. **Dual dialect** — SQLite dev/test, PostgreSQL prod; `store.Open(dialect, dsn)` only  
3. **API compatibility** — JSON camelCase; env names identical to TS MetAPI (no prefix)  
4. **Fail-closed auth** — admin + proxy auth default deny  
5. **Channel isolation** — one channel failure must not cascade; cooldown + circuit breaker  
6. **Request-scoped context** — upstream work cancels with client disconnect where safe  
7. **Thread-safe shared state** — no no-op mutex stubs in hot paths  
8. **Clear package boundaries** — see dependency rules below  

## Package map (truth — B0 must verify against tree)

| Package | Responsibility |
|:--------|:---------------|
| `cmd/server` | Entry |
| `app` | Lifecycle, health, metrics |
| `config` | Env load/validate |
| `auth` | Admin/proxy auth, rate limit, policy |
| `router` | Chi routes, SPA fallback |
| `handler/admin` | Admin REST |
| `handler/proxy` | `/v1/*` |
| `proxy` | ProxyCore orchestration, session, stickiness |
| `routing` | Weights, cooldown, health |
| `platform` | Upstream adapters |
| `transform` | Protocol conversion |
| `store` | DB |
| `service` | Checkin, balance, oauth, backup, notify |
| `scheduler` | Background jobs |
| `web` | Embed only |

## Forbidden imports (draft)

- `handler/*` must not import `cmd/*`  
- `store` must not import `handler` or `proxy`  
- `platform` must not import `handler`  
- UI packages do not exist in Go  

## Error model

- Use shared `APIError` / WriteError helpers  
- Never HTTP 200 with error body on admin mutations  
- Classify upstream: auth-expired vs billing vs model vs transient  

## Concurrency CRITICAL (B2)

- Real `RWMutex` for weight/runtime maps  
- Lease touch goroutines bound to session/context  
- No unbounded goroutine per stream without cancel  

## Ownership

| Issue | Focus |
|:------|:------|
| #16 B0 | This doc + architecture.md truth |
| #17 B1 | Boundary cleanup |
| #18 B2 | CRITICAL concurrency |
| #19 B3 | Unified errors |
