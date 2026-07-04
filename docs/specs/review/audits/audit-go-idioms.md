# Go Idioms Audit: metapi-go

**Date**: 2026-07-05
**Scope**: `proxy/*.go`, `handler/*/*.go`, `service/*.go`, `routing/*.go` (non-test files)
**Auditor**: Automated static analysis + manual review

---

## Executive Summary

| Dimension | Severity | Count | Summary |
|-----------|----------|-------|---------|
| Context propagation | CRITICAL | 4 sites | `context.Background()` in hot request-path code |
| Context propagation | HIGH | 8 sites | `context.Background()` in OAuth/scheduler/service code |
| Error wrapping with `%w` | PASS | -- | Consistent and correct throughout |
| Defer close patterns | PASS | -- | Correctly used for DB rows, bodies, transactions |
| Named result parameters | LOW | 2 sites | One naked return; otherwise acceptable |
| Interface segregation | PASS | -- | Excellent small interfaces across routing package |
| Package naming | LOW | 1 site | `proxyhandler` alias is non-idiomatic |
| sync.Pool usage | MEDIUM | 0 uses | Absent in production code where it would help |
| String concatenation | MEDIUM | 3 sites | `+=` in loops; custom datetime formatter |
| Map capacity pre-allocation | LOW | pervasive | Generally missing size hints |

---

## 1. Context Propagation

### CRITICAL: `context.Background()` in request-path code

**File**: `handler/proxy/upstream.go`
**Lines**: 55, 83

```go
// Line 55 - channel selection uses Background() instead of request context
selected, err := proxy.SelectProxyChannelForAttempt(
    context.Background(),  // SHOULD BE: r.Context()
    ...
)

// Line 83 - upstream HTTP request uses Background() instead of request context
req, err := http.NewRequestWithContext(context.Background(), r.Method, upstreamURL, ...)
// SHOULD BE: http.NewRequestWithContext(r.Context(), ...)
```

**Impact**: Client disconnection does not cancel upstream requests. If the downstream client disconnects, the goroutine continues to hold resources until the upstream responds or the OS-level TCP timeout fires. This blocks `http.Server.Shutdown()` during graceful shutdown and leaks goroutines. This is a **previously documented** finding (audit-concurrency.md, audit-shutdown.md, audit-leaks.md) that remains unfixed.

**Fix**: Replace `context.Background()` with `r.Context()` at both call sites. Pass `*http.Request` into `handleStreamUpstream` to monitor client disconnect.

### CRITICAL: `context.Background()` in HTTP helper functions

**File**: `service/proxy_util.go`
**Lines**: 42, 55

```go
func HTTPGet(proxyURL, requestURL string, headers map[string]string) (*http.Response, error) {
    req, err := http.NewRequestWithContext(context.Background(), ...)
    // Missing context parameter - should accept context.Context
}

func HTTPPost(proxyURL, requestURL, contentType string, body io.Reader, headers map[string]string) (*http.Response, error) {
    req, err := http.NewRequestWithContext(context.Background(), ...)
    // Missing context parameter
}
```

**Impact**: All callers of `HTTPGet`/`HTTPPost` (balance checks, check-in, OAuth token exchange, platform detection) run without cancellation propagation.

**Fix**: Add `ctx context.Context` as the first parameter to both functions and use it in `NewRequestWithContext`.

### HIGH: `context.Background()` in OAuth flow and scheduler

**Files and lines**:

| File | Lines | Context |
|------|-------|---------|
| `service/oauth/flow.go` | 105, 187, 201 | OAuth authorization URL, token exchange |
| `service/oauth/connection.go` | 197, 249, 254 | Route rebuild, model refresh after OAuth connect |
| `service/oauth/route_unit.go` | 268, 276, 399, 407 | Route rebuild after route unit mutations |
| `service/oauth/import.go` | 90 | OAuth account activation |
| `scheduler/balance.go` | 107 | Route rebuild in balance scheduler |
| `platform/registry.go` | 136 | Platform detection |
| `platform/detect.go` | 172 | Platform detection with timeout |

**Impact**: These call sites propagate a detached context, meaning any parent cancellation is lost. For scheduler tasks this is somewhat acceptable (they are background workers), but for OAuth flows triggered by user requests, the parent request context should flow through.

**Fix**: Thread the caller's `context.Context` through the call chain. For scheduler tasks, use a context derived from the scheduler's lifecycle context (e.g., `context.WithTimeout(s.lifecycleCtx, 30*time.Second)`).

### OK: `context.Background()` at application bootstrap

`app/app.go:80` and `cmd/server/main.go:108` use `context.Background()` with `context.WithTimeout` for startup health checks. This is the canonical pattern for application initialization and is correct.

---

## 2. Error Wrapping with `%w`

**Verdict: PASS**

The codebase consistently uses `fmt.Errorf("...: %w", err)` across all layers examined:

- `routing/*.go` -- 4 sites, all correct
- `store/*.go` -- 11 sites, all correct
- `service/*.go` -- 4 sites, all correct
- `handler/admin/*.go` -- 3 sites, all correct
- `platform/*.go` -- 14 sites, all correct
- `cmd/migrate/main.go` -- 14 sites, all correct
- `service/notify/*.go` -- 12 sites, all correct
- `service/oauth/*.go` -- 12 sites, all correct
- `proxy/executor.go` -- 6 sites, all correct

No instances of bare `%v` or `%s` for errors were found in the audited non-test files. Error wrapping is a strong point of this codebase.

---

## 3. Defer Close Patterns

**Verdict: PASS**

Resource cleanup is handled correctly:

- **DB rows**: `defer rows.Close()` at 22+ sites across store, scheduler, handlers. Correctly paired with `rows.Next()` loops.
- **DB transactions**: `defer tx.Rollback()` before `tx.Commit()` in `service/site_service.go:249` and `handler/admin/settings_backup.go`. Standard pattern.
- **HTTP response bodies**: `defer resp.Body.Close()` in `service/proxy_util.go:83`, `platform/sub2api.go:71`, and handler code.
- **Multipart writers**: `defer writer.Close()` + `defer pipeWriter.Close()` in `handler/proxy/multipart.go:67-68`.

One minor note: `handler/proxy/upstream.go:115` calls `resp.Body.Close()` directly rather than deferring it. This is acceptable since it is the final operation before return, but a `defer` would be more defensive against future code additions.

---

## 4. Named Result Parameters

**Verdict: LOW severity**

### Naked return (anti-pattern)

**File**: `routing/round_robin.go`
**Lines**: 47-67

```go
func ApplyRoundRobinCooldown(
    consecutiveFailCount int64,
    cooldownLevel int64,
    nowMs int64,
    configuredMaxSec int,
) (nextConsecutiveFailCount int64, nextCooldownLevel int64, cooldownUntilISO *string) {
    // ...
    return  // <-- naked return, non-obvious which values are returned
}
```

**Recommendation**: Use explicit returns. If the named results serve documentation, keep the names but return values explicitly.

### Acceptable named results

- `routing/decision.go:36` -- `(exactModelCount int, wildcardRouteCount int, err error)` documents three return values clearly. Not a naked return.
- `routing/round_robin.go:145` -- `(year int, month int, day int)` for a pure calculation helper. Acceptable.
- `routing/weights.go:521` -- named returns for complex struct types used to document what is returned. Acceptable.

---

## 5. Interface Segregation

**Verdict: PASS -- Strong**

The routing package demonstrates excellent interface design:

| Interface | Methods | File |
|-----------|---------|------|
| `ModelProvider` | 2 | `routing/ports.go` |
| `TokenProvider` | 2 | `routing/ports.go` |
| `PricingProvider` | 2 | `routing/ports.go` |
| `ChannelLoadSnapshotProvider` | 1 | `routing/ports.go` |
| `RouteRebuilder` | 1 | `routing/ports.go` |
| `DecisionDB` | 5 | `routing/decision.go` |
| `SnapshotDB` | 5 | `routing/snapshot.go` |
| `SettingsStore` | 2 | `routing/runtime_health.go` |

One interface that could be split:

**`ChannelSelectorDB`** (20 methods, `routing/selector.go:12-83`). This is the main DB abstraction for the routing engine and groups route loading, channel loading, OAuth route unit operations, channel mutation, and runtime health. Consider splitting into role-specific interfaces:

```go
type RouteReader interface { ... }       // LoadEnabledRoutes, FindAllEnabledRoutes, LoadRouteGroupSources
type ChannelReader interface { ... }     // LoadRouteChannels, LoadChannelWithAccount, LoadCredentialScopedChannelIDs
type OAuthRouteUnitReader interface { ... }  // LoadOAuthRouteUnitSummaries, LoadOAuthRouteUnitMembers, FindRouteIDsByOAuthRouteUnitID
type ChannelWriter interface { ... }     // UpdateChannelCooldownFields, UpdateChannelSuccessFields, etc.
```

However, this is a **LOW** priority refactoring since the interface is internal to the routing package.

---

## 6. Package Naming Conventions

**Verdict: LOW severity**

### `proxyhandler` alias

**File**: `handler/proxy/upstream.go:1`

```go
package proxyhandler
```

The directory is `handler/proxy` but the package is named `proxyhandler`. This is because the top-level `proxy/` package already exists, causing a name collision. In Go, packages in the same module share a namespace, so a directory named `proxy` would conflict with `import ".../proxy"`.

**Recommendation**: Consider renaming either the top-level `proxy/` package or the `handler/proxy/` package. If `handler/proxy/` were renamed to `handler/proxyhandler/` (matching the package name), it would be clearer. Alternatively, the top-level `proxy/` could be named `proxylib/`.

### Other packages

All other packages follow Go conventions: lowercase, no underscores, short names (`routing`, `store`, `auth`, `config`, `service`). Sub-packages like `service/oauth`, `service/notify`, `service/checkin` are correctly structured.

---

## 7. sync.Pool Usage

**Verdict: MEDIUM severity -- absent**

`sync.Pool` is not used anywhere in production code (only in audit docs as recommendations). There are several hot paths where pooled buffers would reduce allocation pressure:

### Recommendation 1: SSE streaming buffer

**File**: `handler/proxy/upstream.go:179`

```go
buf := make([]byte, 4096)  // Allocated per-stream
```

**Fix**:
```go
var sseBufPool = sync.Pool{
    New: func() any { return make([]byte, 4096) },
}

buf := sseBufPool.Get().([]byte)
defer sseBufPool.Put(buf)
```

### Recommendation 2: Selection candidate slices

**File**: `routing/selector.go:257`

```go
var available []RouteChannelCandidate  // Allocated per selection
```

A pool keyed by capacity bracket could recycle these slices across requests.

### Recommendation 3: JSON body buffer in `swapModelInJSON`

**File**: `handler/proxy/upstream.go:262`

The `json.RawMessage` map allocation in `swapModelInJSON` could be pooled if profiling shows it as a hot allocation site.

---

## 8. String Concatenation

**Verdict: MEDIUM severity**

### String `+=` in loop (anti-pattern)

**File**: `routing/router.go:424-436`

```go
func formatReasons(reasons []string) string {
    result := ""
    for i, r := range reasons {
        if i > 0 {
            result += "、"
        }
        result += r
    }
    return result
}
```

**Fix**: Use `strings.Builder`:
```go
var b strings.Builder
for i, r := range reasons {
    if i > 0 {
        b.WriteString("、")
    }
    b.WriteString(r)
}
return b.String()
```

### String `+=` in float formatting

**File**: `routing/runtime_health.go:1267-1287`

```go
func fmtFloat(v float64) string {
    // ...
    s = fmtInt(intPart)
    s += "."
    for i := 0; i < 6; i++ {
        s += string(rune('0' + d))  // Allocates per digit
    }
    // ...
}
```

**Fix**: Use `strings.Builder` with `Grow()` hint for predictable length.

### Good patterns observed

- `routing/decision.go:89-178` -- `marshalDecision` uses `[]byte` appends to build JSON efficiently.
- `routing/runtime_health.go:1151-1254` -- `marshalPayload` uses `[]byte` appends for JSON serialization.
- `service/account_service.go:367` -- `strings.Join` for SQL SET clause building.
- `store/open.go:70` -- `strings.Builder{}` used for URI path decoding.

### Custom time formatting (anti-pattern)

**File**: `routing/round_robin.go:79-164`

A custom `timeTime` struct and manual date calculation (`daysToDate`) reimplements `time.Time.Format()`. This is 85 lines of code that could be replaced with:

```go
func timeMsToISO(ms int64) string {
    return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}
```

**Impact**: The custom implementation may have edge-case bugs (leap years, timezone offsets). It also makes the code harder to maintain.

---

## 9. Map Capacity Pre-Allocation

**Verdict: LOW severity -- pervasive but low-impact**

Most `make(map[...])` calls do not provide a capacity hint. Examples:

- `routing/selector.go:329` -- `layers := make(map[int64][]RouteChannelCandidate)`
- `routing/selector.go:379` -- `allowSet := make(map[int64]bool)`
- `routing/router.go:92` -- `names := make(map[string]bool)`

When the approximate size is known (e.g., `allowSet` where `len(policy.AllowedRouteIDs)` is known), providing a size hint avoids rehash during population:

```go
allowSet := make(map[int64]bool, len(policy.AllowedRouteIDs))
```

Positive examples exist:
- `auth/downstream.go:365` -- `result := make(map[int64]float64, len(rawMap))`
- `handler/proxy/client_detect.go:15` -- `result := make(map[string]string, len(headers))`

---

## 10. Additional Findings

### Custom JSON marshaling alongside `encoding/json` (code smell)

**File**: `routing/runtime_health.go`

The file imports `encoding/json` but also contains manual JSON marshalers (`marshalPayload`, `marshalState`, `unmarshalPayload`) that build JSON with byte slices. The `marshalPayload` function (lines 1151-1203) is 53 lines of manual JSON building that could be replaced with `json.Marshal()`. This creates maintenance risk: adding a field requires updating both the struct and the manual marshaler.

### `sync_RWMutex` type alias

**File**: `routing/weights.go:347`

```go
type sync_RWMutex = sync.RWMutex
```

This alias has no purpose and violates Go naming conventions (underscores in type names). The global state maps (`stableFirstLastSelectedSiteByKey`, etc.) use `sync_RWMutex` while other files use `sync.RWMutex` directly. This is confusing.

**Fix**: Remove the alias and use `sync.RWMutex` consistently.

### `stringsTrimSpace` reimplementation

**File**: `routing/router.go:438-453`

```go
func stringsTrimSpace(s string) string {
    return stringsTrimSpaceImpl(s)
}

func stringsTrimSpaceImpl(s string) string {
    start := 0
    for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
        start++
    }
    // ...
}
```

**Fix**: Replace with `strings.TrimSpace(s)`. This is 16 lines of unnecessary code. The `strings` package is already imported in this file.

### `go func()` without lifecycle management

**File**: `routing/pricing.go:69`

```go
go func() {
    _ = p.provider.RefreshModelPricingCatalog(ctx, candidate.Site, candidate.Account, modelName)
}()
```

Fire-and-forget goroutines in request-path code (this is called from `SelectChannel` path) can accumulate during high load. Consider using a bounded worker pool or at minimum add panic recovery.

---

## Summary of Recommended Actions

| Priority | Finding | File(s) | Effort |
|----------|---------|---------|--------|
| **P0** | Replace `context.Background()` with `r.Context()` in upstream dispatch | `handler/proxy/upstream.go:55,83` | Small |
| **P0** | Add `context.Context` parameter to `HTTPGet`/`HTTPPost` | `service/proxy_util.go:40,53` | Medium (all callers must update) |
| **P1** | Thread context through OAuth flow callbacks | `service/oauth/flow.go`, `connection.go`, `route_unit.go`, `import.go` | Medium |
| **P1** | Fix naked return in `ApplyRoundRobinCooldown` | `routing/round_robin.go:66` | Small |
| **P1** | Replace custom `timeTime` with `time.UnixMilli()` | `routing/round_robin.go:79-164` | Medium (remove 85 lines) |
| **P2** | Use `strings.Builder` in `formatReasons` | `routing/router.go:424-436` | Small |
| **P2** | Replace `stringsTrimSpace` with `strings.TrimSpace` | `routing/router.go:438-453` | Small |
| **P2** | Remove `sync_RWMutex` alias | `routing/weights.go:347` | Small |
| **P2** | Add panic recovery to fire-and-forget goroutines | `routing/pricing.go:69` | Small |
| **P3** | Add `sync.Pool` for SSE buffers | `handler/proxy/upstream.go:179` | Small |
| **P3** | Use `strings.Builder` in `fmtFloat` | `routing/runtime_health.go:1267` | Small |
| **P3** | Add map capacity hints where size is known | various | Small (pervasive) |
| **P4** | Replace manual JSON marshalers with `encoding/json` | `routing/runtime_health.go:1151-1254` | Medium |
| **P4** | Consider splitting `ChannelSelectorDB` interface | `routing/selector.go:12-83` | Medium |
| **P4** | Rename `proxyhandler` package or directory | `handler/proxy/*.go` | Medium |

---

## Files Examined

**routing/**: `cache.go`, `cooldown.go`, `decision.go`, `matcher.go`, `ports.go`, `pricing.go`, `round_robin.go`, `route_units.go`, `router.go`, `runtime_health.go`, `selector.go`, `snapshot.go`, `stable_first.go`, `weights.go`, `workflow.go`

**service/**: `account_credential.go`, `account_extra_config.go`, `account_health.go`, `account_service.go`, `account_token_service.go`, `localtime.go`, `proxy_util.go`, `site_detect.go`, `site_endpoint_service.go`, `site_service.go`, `today_reward.go`

**handler/proxy/**: `chat.go`, `client_detect.go`, `completions.go`, `embeddings.go`, `files.go`, `gemini.go`, `images.go`, `input_files.go`, `messages.go`, `models.go`, `multipart.go`, `responses.go`, `responses_ws.go`, `router.go`, `search.go`, `sse_parser.go`, `surface.go`, `upstream.go`, `videos.go`

**handler/admin/**: `accounts.go`, `account_tokens.go`, `auth_settings.go`, `checkin_routes.go`, `downstream_keys.go`, `events.go`, `monitor.go`, `oauth_routes.go`, `search.go`, `settings.go`, `settings_backup.go`, `settings_database.go`, `settings_maintenance.go`, `settings_notify.go`, `sites.go`, `site_announcements.go`, `stats.go`, `tasks.go`, `test.go`, `token_routes.go`, `update_center.go`
