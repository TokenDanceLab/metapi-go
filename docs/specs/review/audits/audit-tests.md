# MetAPI Go Test Audit

**Date**: 2026-07-04  
**Scope**: 96 test files, 1,697 test functions, across all packages  
**Standard**: testify vs manual, mocks, table-driven, error-path coverage, flaky detection

---

## 1. Assertion Library: Manual (No testify)

### Finding: All assertions use bare `if` checks and `t.Errorf`/`t.Fatalf`

**Severity**: Low (consistent style, no fragmentation)

The project has **zero dependency on testify**. The `go.mod` file contains no `stretchr/testify` entry. All 1,697+ test functions use the standard library's `testing.T` with manual `if got != expected { t.Errorf(...) }` patterns.

```go
// Pattern used everywhere:
if result.StatusCode != 403 {
    t.Errorf("expected 403, got %d", result.StatusCode)
}
```

**Assessment**: Not a defect. The codebase is internally consistent. Switching to testify would add a dependency for marginal readability gain. Notably absent are testify's `assert.Equal`, `require.NoError`, and `assert.GreaterOrEqual` patterns that would reduce boilerplate for the 97 table-driven test cases. The lack of `github.com/stretchr/testify/assert` means no elegant diff output on struct mismatches, but this is a cosmetic concern.

**Recommendation**: Keep the current approach. Consistency across 96 files is more valuable than switching tooling. If testify is adopted, do it incrementally (one package at a time), not across the whole codebase.

---

## 2. Mock Consistency

### Finding: Inconsistent mock patterns across packages

The codebase uses **three distinct mocking strategies**:

| Strategy | Packages | Example |
|---|---|---|
| Function-valued struct fields | `proxy`, `routing` | `mockRouter{ selectChannel: func(...) }` |
| Interface-implementing structs | `scheduler`, `routing/workflow_test.go` | `mockScheduler struct{}` implementing `Scheduler` |
| Real in-memory SQLite DB | `auth`, `store`, `service` | `Open(DialectSQLite, ":memory:", false)` |

**Detailed breakdown**:

- **`proxy/channel_selection_test.go`**: Uses `mockRouter` with function-valued fields (`selectChannel`, `selectNextChannel`, `selectPreferredChannel`). This is the most flexible mock pattern -- callers inject behavior per test case. Good pattern.

- **`proxy/session_test.go`**: Uses real `ProxyChannelCoordinator` with `config.Load()` + `config.Set()` + real mutex-based concurrency. No mocks -- tests operate against the real coordination layer with controlled config. Pragmatic for stateful components.

- **`scheduler/scheduler_test.go`**: Uses `mockScheduler` struct implementing the `Scheduler` interface, plus `panicScheduler` and `errorStopScheduler` for error-path testing. Clean interface-based mocking.

- **`routing/workflow_test.go`**: Uses `mockModelProvider`, `mockRouteRebuilder`, `mockPricingProvider` -- all interface-implementing structs. Consistent with the file's scope.

- **`routing/algorithm_test.go`**: Uses `staticModel()` closure and `buildTestCandidate()` helpers instead of mocks. Tests operate on pure in-memory data structures.

- **`auth/proxy_test.go` + `auth/downstream_test.go`**: Uses a real in-memory SQLite database (`:memory:`) via `store.EnsureRuntimeDatabase()`. Tests insert rows, then call auth functions that read from the DB singleton. This is more of an integration-test pattern than mock.

- **`e2e/e2e_test.go`**: Uses its own `mockRouter` (different from `proxy` package's) with channel stacks, and `httptest.NewServer` for mock upstreams. This is an end-to-end pattern with hand-rolled fakes.

**Issues**:
1. **Duplicate mock implementations**: `mockRouter` exists in both `proxy/channel_selection_test.go` and `e2e/e2e_test.go` with different shapes. No shared mock package.
2. **Real DB as mock**: The `auth/` package tests depend on `store.EnsureRuntimeDatabase()` creating a real in-memory SQLite DB. This ties unit tests to the full migration + schema, making them slower and coupling auth tests to store implementation.
3. **No mock generation tool**: No use of `gomock`, `mockery`, or `counterfeiter`.

**Recommendation**: The function-valued struct mock pattern (`proxy/`) is the strongest. Consider extracting shared mock utilities into `testing/mocks/` or a test helper package. For the real-DB-as-mock pattern in `auth/`, a SQLite interface abstraction would make unit tests faster and more isolated.

---

## 3. Table-Driven Tests

### Finding: Good adoption but uneven distribution

**Stats**:
- **97 occurrences** of `tests := []struct{...}` across 43 files
- **380 uses** of `t.Run()` across 33 files
- **1,697** total test functions

**Packages with strong table-driven coverage**:

| Package/File | Table-Driven Tests | Quality |
|---|---|---|
| `routing/matcher_test.go` | 10 table-driven blocks (~50 cases) | Excellent -- exhaustive model pattern testing |
| `routing/cooldown_test.go` | 3 table-driven blocks | Good -- Fibonacci, resolve, round-robin |
| `routing/runtime_health_test.go` | Multiple per-category blocks | Good -- 8 penalty categories, transient, breaker |
| `scheduler/scheduler_test.go` | 7 table-driven blocks | Good -- clamp, max, cron validation |
| `store/dialect_test.go` | 3 table-driven blocks | Good -- btype, rtype, serialPK |
| `proxy/retry_policy_test.go` | 2 table-driven blocks | Good -- maxAttempts, maxRetries |
| `service/site_service_test.go` | Multiple test+subtest | Good -- platform detection is exhaustive |
| `service/account_mutation_test.go` | 2 table-driven blocks | Good -- credential mode, capabilities |
| `proxy/channel_selection_test.go` | 1 table-driven block | Good -- IsLoopbackClientIP |
| `transform/shared/shared_test.go` | 2 table-driven blocks | Good |

**Packages with weak or no table-driven tests**:

- `auth/proxy_test.go` (41 test functions, 0 table-driven): Each edge case gets its own `TestXxx` function. These 41 functions test a single function `extractProxyToken()` and could be consolidated into 3-4 table-driven blocks (Bearer, x-api-key, priority rules). **This is the largest missed opportunity.**

- `auth/admin_test.go` (72 test functions, 0 table-driven): Similar pattern -- one function per test case. The `normalizeIP`, `parseAllowlist`, `isIPAllowed`, and `extractClientIP` test groups could each be table-driven.

- `service/account_credential_test.go` (14 functions, only one sub-function uses table-driven): The `TestDecryptInvalidFormat` and `TestEncryptDecryptMultiplePasswords` functions use subtests but most encrypt/decrypt tests are standalone.

- `store/postgres_test.go` (12 functions, 0 table-driven): Schema verification and CRUD tests are individual.

- `platform/*_test.go` (various): Most platform tests use `t.Run()` with URL lists but few use formal table-driven structs.

**Recommendation**: Good overall. Main remediation target: collapse `auth/proxy_test.go` (41 functions for one function) and `auth/admin_test.go` (72 functions) into table-driven blocks. This would reduce line count by ~60% while maintaining or improving coverage clarity.

---

## 4. Error-Path Coverage

### Finding: Excellent -- comprehensive edge-case coverage

The test suite demonstrates strong error-path awareness across all packages:

**Strengths**:

- **Null/zero-value handling**: Nearly every test file has `nil` checks: `TestParseISO8601_Invalid`, `TestDetectSite_EmptyString`, `TestResolveStoredCredentialMode_NoConfigNoAccessToken`, `TestExtractProxyToken_NoTokenSource`, `TestAuthorizeDownstreamToken_EmptyToken`, etc.

- **Boundary conditions**: Thorough edge-case tests: `TestResolveFailureBackoffSec_Ceiling` (fib overflow), `TestNormalizeGlobalWeight_ClampMin/Max`, `TestRecordManagedKeyCostUsage_NegativeCost/NaN/Inf`, `TestShouldRetryProxyRequest_EdgeCases`.

- **Invalid inputs**: Covered: `TestDecryptInvalidFormat`, `TestParseAllowlist_InvalidCIDR`, `TestDetectSite_InvalidURL`, `TestOpenInvalidDialect`, `TestCronRunner_InvalidExpr`.

- **Unique constraint violations**: Tested in `store/migrate_test.go` (platform+url dup), `auth/downstream_test.go` (key dup), `store/sqlite_test.go` (CHECK constraint).

- **Panic recovery**: `TestRegistryStartAll_PanicRecovery`, `TestCronRunner_PanicRecovery`, `TestMigrateFunctionDoesNotPanic`.

- **Idempotency**: `TestAutoMigrateIdempotent` and `TestAutoMigrateIdempotentWithData` verify double-migration safety.

- **Concurrent access**: `TestPostgresConcurrentAccess`, `TestRateLimit` (E2E), `TestRouteCache_ConcurrentReadsDontPanic`.

- **Floating-point edge cases**: `TestRecordManagedKeyCostUsage_NaN`, `TestRecordManagedKeyCostUsage_Inf`, `TestClampNumber` with boundary values.

**Gaps identified**:

1. **Context cancellation/timeout**: No tests verify that long-running proxy operations respect `ctx.Done()`. The proxy flow passes `context.Background()` in tests.

2. **Large payload / overflow**: `TestSettingStoreLargeValue` tests 10KB, but no test for near-max SQLITE_MAX_LENGTH (~1GB) or PostgreSQL max field sizes.

3. **Race conditions**: The proxy concurrency test (`TestRateLimit`) uses `time.Sleep(50ms)` in the mock upstream, but `go test -race` coverage is not verifiable from static analysis.

4. **Nil config/empty config**: Most tests provide valid configs. `TestMigrateFunctionDoesNotPanic` tests nil DB, but few tests exercise nil/malformed config paths end-to-end.

**Assessment**: Error-path coverage is a **strong point** of this test suite. The few gaps listed are niche and low-risk.

---

## 5. Flaky Test Detection

### Finding: `time.Sleep` usage in 5 files -- scheduler tests are the primary concern

**29 total `time.Sleep` calls across 5 files**:

| File | Occurrences | Risk | Notes |
|---|---|---|---|
| `scheduler/scheduler_test.go` | 17 | **HIGH** | `time.Sleep(50ms)` waiting for goroutine start; `time.Sleep(2500ms)` for cron job verification; `time.Sleep(1500ms)` for cron removal. These are **timing-dependent** and will flake on slow CI. |
| `proxy/session_test.go` | 8 | **MEDIUM** | `time.Sleep(100ms)` after lease release. Tests operate on real goroutines and mutexes. Fast on dev machines, may fail on CI with constrained CPU. |
| `routing/cache_test.go` | 2 | **LOW** | `time.Sleep(150ms)` to wait for 100ms TTL to expire. Close to the boundary; may pass ocassionally due to timer granularity. |
| `e2e/e2e_test.go` | 1 | **LOW** | `time.Sleep(50ms)` in mock upstream to simulate processing time for concurrency test. |
| `store/dialect_test.go` | 1 | **LOW** | `time.Sleep(1ms)` to ensure timestamps differ. Necessary for format consistency; negligible risk. |

**No `math/rand` or `rand.` usage detected** in any test file. Zero non-deterministic random-based flake risk.

**Detailed flaky-risk analysis**:

**`scheduler/scheduler_test.go` (CRITICAL)**:
```go
// TestCronRunner_StartStop:
cr.start()
time.Sleep(2500 * time.Millisecond)  // Wait for cron to fire
cr.stop()
if count.Load() < 1 { t.Error(...) }  // Assert it fired at least once
```
This test will fail if CI is under load and 2500ms isn't enough for the cron scheduler to tick. The `"* * * * * *"` cron fires every second, so 2500ms should guarantee 2 firings -- but scheduler startup overhead can eat into this window.

```go
// TestRegistryStartAll:
r.StartAll(ctx)
time.Sleep(50 * time.Millisecond)  // Allow goroutines to run
if !m1.started.Load() { t.Error(...) }
```
50ms is tight for goroutine scheduling on a loaded CI runner.

**Remediation paths**:
1. **Add synchronization primitives**: Replace `time.Sleep` with channels, `sync.WaitGroup`, or condition variables. Example: the mock scheduler can send on a channel when `Start()` completes.
2. **Poll with timeout**: Replace `time.Sleep(2500ms)` with a polling loop: `for i := 0; i < 50; i++ { if count.Load() >= 1 { break }; time.Sleep(100ms) }`.
3. **Test hooks**: Add a `Started() <-chan struct{}` to schedulers so tests can block until the scheduler is actually running.

---

## 6. Additional Observations

### 6.1 Golden file tests
`routing/algorithm_test.go` and `routing/weights_test.go` write golden files to `routing/testdata/`. These are **both tests and side effects** -- the golden files are written unconditionally, meaning test runs always overwrite them. This is unusual; golden files should be read-only artifacts committed to the repo. The current implementation effectively logs probabilities, not validates them.

### 6.2 Test helper duplication
Multiple packages define identical helpers:
- `ptrInt()`, `ptrFloat()`, `ptrStr()`, `strPtr()` appear in `routing/cooldown_test.go`, `routing/weights_test.go`, `routing/route_units_test.go`, `proxy/session_test.go`, `e2e/e2e_test.go`, `service/account_mutation_test.go`
- `buildTestCandidate()` / `makeCandidate()` / `makeChannel()` appear in `routing/algorithm_test.go`, `routing/weights_test.go`, `routing/workflow_test.go`, `routing/selector_compare_test.go`, `proxy/endpoint_flow_test.go`
- `openTestSQLite()` / `setupTestDB()` / `openTestDB()` appear in `store/sqlite_test.go`, `store/setting_store_test.go`, `service/account_mutation_test.go`, `auth/downstream_test.go`

A shared `internal/testutil` package would reduce this duplication.

### 6.3 Missing benchmarks
The codebase has zero `Benchmark*` functions. The routing weight calculation (`CalculateWeightedSelection`), model pattern matching (`MatchesModelPattern`), and channel filtering logic are performance-sensitive hot paths that would benefit from benchmarks, especially since they run on every API request.

### 6.4 Test naming convention
Consistent `Test<FunctionName>_<Scenario>` pattern observed throughout. Names are descriptive. No anonymous/unclear test names.

### 6.5 Coverage
`coverage.out` (979KB) exists at the repo root, indicating coverage is collected. Without parsing it, the sheer volume and breadth of tests suggest coverage is at least 70%+ for core packages.

---

## Summary

| Dimension | Grade | Notes |
|---|---|---|
| Assertion consistency | **A-** | No testify, but consistent manual style across all 96 files |
| Mock consistency | **B** | Three strategies coexist; `mockRouter` is duplicated; no shared mock package |
| Table-driven tests | **B+** | Good in routing/scheduler; `auth/` package would benefit most from consolidation |
| Error-path coverage | **A** | Excellent null/boundary/edge-case coverage; minor gaps in ctx cancellation |
| Flaky test risk | **B-** | Scheduler tests rely on `time.Sleep`; 17 occurrences in one file; cache expiry tests near boundary |
| Code duplication | **B** | Helper functions duplicated across packages; no shared `testutil` |
| No rand-based flakiness | **A+** | Zero `math/rand` usage in tests |

**Top 3 action items**:
1. Replace `time.Sleep` waits in `scheduler/scheduler_test.go` with synchronization primitives (channels or polling loops).
2. Consolidate the 41 `TestExtractProxyToken_*` functions in `auth/proxy_test.go` into 3-4 table-driven blocks.
3. Extract shared test helper utilities (`ptrInt`, `ptrFloat`, `ptrStr`, `buildTestCandidate`) into a single `internal/testutil` package to reduce the ~15 duplicate helper definitions.
