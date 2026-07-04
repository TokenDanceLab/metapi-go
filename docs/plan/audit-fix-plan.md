# Audit Fix Plan

**Date**: 2026-07-04 | **Source**: 16-agent audit | **Findings**: 47 total (10 CRITICAL, 12 HIGH, 15 MEDIUM, 10 LOW)

## CRITICAL Fixes (must fix before v0.2.0)

### C1: Rate limiting missing on /api/ and OAuth routes
- **Audit**: audit-ratelimit
- **Current**: Zero per-IP rate limiting. TS has 21 rate limit guards + 6 rate-limiter-flexible instances
- **Fix**: Add token bucket rate limiter middleware for /api/* (100 req/min per IP) and stricter for OAuth sensitive routes (10 req/min)
- **Files**: auth/ratelimit.go, router/router.go

### C2: RWMutexStub is a no-op — race in stable-first routing
- **Audit**: audit-concurrency
- **Current**: `routing/weights.go:346` defines `RWMutexStub` with empty Lock/Unlock/RLock/RUnlock methods
- **Fix**: Replace with real `sync.RWMutex`
- **Files**: routing/weights.go

### C3: Backup export/import/factory-reset are stubs
- **Audit**: audit-backup
- **Current**: All 3 endpoints return empty data or do nothing
- **Fix**: Implement: export ALL 27 tables to JSON, import from JSON with conflict handling, factory-reset truncate all tables
- **Files**: handler/admin/settings_backup.go, handler/admin/settings_maintenance.go

### C4: Usage aggregation has no transaction — double-counting risk
- **Audit**: audit-db
- **Current**: `scheduler/usage_aggregation.go` applyBatch does per-row INSERTs without wrapping in BEGIN/COMMIT
- **Fix**: Wrap batch in DB transaction
- **Files**: scheduler/usage_aggregation.go

### C5: DB connection never closed on shutdown
- **Audit**: audit-shutdown
- **Current**: `app/app.go` Shutdown closes HTTP server but never calls store.Close()
- **Fix**: Add store.Close() to shutdown sequence
- **Files**: app/app.go

### C6: Proxy failures silently discarded in hot path
- **Audit**: audit-logging
- **Current**: proxy/surface.go and handler/proxy/upstream.go silently discard errors with no log
- **Fix**: Add structured error logging at WARN level
- **Files**: proxy/surface.go, handler/proxy/upstream.go

### C7: Raw Go errors exposed to HTTP clients
- **Audit**: audit-errors
- **Current**: Several handlers return raw error messages (e.g., "sql: no rows in result set")
- **Fix**: Wrap errors with user-facing messages, log internal details
- **Files**: handler/admin/*.go, handler/proxy/*.go

### C8: Platform shield challenge (acw_sc__v2) unsolvable
- **Audit**: audit-platform-parity
- **Current**: Go SolveAcwScV2 exists but anti-bot JS execution requires a JS engine
- **Fix**: Document as known limitation; add retry with clear error message
- **Files**: platform/newapi.go

### C9: SSE byte passthrough — no parsing, no error injection
- **Audit**: audit-streaming
- **Current**: `handleStreamUpstream` does raw byte passthrough with zero SSE event awareness
- **Fix**: Add SSE event parser that can inspect events for error detection
- **Files**: handler/proxy/upstream.go

### C10: AES key derivation has hardcoded fallback
- **Audit**: audit-security
- **Current**: `accountCredentialSecret` falls back to hardcoded "change-me-admin-token" if not set
- **Fix**: Warn on startup if using fallback; require explicit config
- **Files**: service/account_credential.go, config/config.go

## HIGH Fixes

- H1: OAuth coverage 24.9% — add tests for flow.go, connection.go, refresh.go, quota.go, import.go
- H2: SSE streaming has no empty-content failure detection
- H3: No WebSocket upgrade for Codex responses
- H4: Missing /v1/input_files endpoint
- H5: settings updateRuntime 0% coverage (most dangerous untested function)
- H6: Password validation: empty passwords accepted
- H7: proxy/session.go lease cleanup on process crash (no TTL-based auto-expiry)
- H8: Missing missing model-availability probe wiring
- H9: OAuth still uses legacy log.Printf instead of slog
- H10: No circuit breaker on upstream health checks
- H11: Content-Type not validated on non-streaming responses
- H12: Maximum body size not enforced on /v1/ endpoints

## Task Plan

| Task | Priority | Effort | Depends On |
|:-----|:---------|:-------|:-----------|
| C2: Fix RWMutexStub | CRITICAL | S (5 min) | None |
| C5: Store.Close in shutdown | CRITICAL | S (5 min) | None |
| C4: DB transaction for aggregation | CRITICAL | S (15 min) | None |
| C10: AES key warning | CRITICAL | S (10 min) | None |
| C6: Proxy error logging | CRITICAL | M (1h) | None |
| C7: Error wrapping | CRITICAL | M (1h) | None |
| C1: Rate limiting | CRITICAL | M (2h) | None |
| C3: Backup/import/factory-reset | CRITICAL | L (3h) | None |
| C9: SSE event parsing | CRITICAL | L (4h) | None |
| C8: Shield challenge doc | CRITICAL | S (30 min) | None |
| H1-H12: HIGH fixes | HIGH | L (4h) | After CRITICAL |
| M1-M15: MEDIUM fixes | MEDIUM | L (3h) | After HIGH |
