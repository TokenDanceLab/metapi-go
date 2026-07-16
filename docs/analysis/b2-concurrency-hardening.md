# B2 concurrency hardening (#18)

Last updated: 2026-07-16

## Scope

CRITICAL residual races around shared stable-first maps, lease lock-order / lifecycle
hygiene, and request-context propagation on the upstream proxy path.

## Changes

### 1. Stable-first map locking (`routing/weights.go`)

- Removed the `sync_RWMutex` type alias; `stableFirstStateMu` is a real `sync.RWMutex`.
- `selectStableFirstCandidate` now reads `stableFirstLastSelectedSiteByKey` under
  `RLock`.
- `UpdateStableFirstObservationProgress` holds one exclusive lock for the full
  read-modify-write (progress + optional site cooldown) via locked helpers.
- Added `-race` coverage: `TestStableFirstStateMaps_ConcurrentRace`.

### 2. Lease lifecycle (`proxy/session.go`)

- Original touchLease-per-tick goroutine leak is already fixed (single expiry
  goroutine + non-blocking Touch via `expiryCh`).
- Hardened further:
  - `nextLeaseID` is `atomic.Int64` so `createTrackedLease` never takes `c.mu`
    while `state.mu` is held (lock order stays `c.mu` then `state.mu`).
  - `doneCh` is unbuffered close-only.
- Added stress test: `TestLeaseLifecycle_ConcurrentAcquireSnapshot`.

### 3. Request context (`handler/proxy/upstream.go`)

- Production dispatch already uses `r.Context()` for channel selection, request
  construction, success/failure recording, and SSE cancel checks.
- Hardened SSE path: upstream `resp.Body` is always closed via `defer` after
  stream handling (including early client disconnect).

## Verification

```bash
go test ./routing -run 'StableFirstStateMaps_ConcurrentRace|StableFirst' -race -count=1
go test ./proxy -run 'LeaseLifecycle_Concurrent|LeaseLifecycle|AcquireChannelLease' -race -count=1
go test ./handler/proxy -run 'Upstream|Stream' -count=1
```
