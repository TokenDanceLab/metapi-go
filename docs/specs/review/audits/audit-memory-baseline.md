# MetAPI-Go Memory Baseline Audit

**Date**: 2026-07-05
**Binary**: `metapi.exe` (Go, `-trimpath -ldflags="-s -w"`)
**Target**: `cmd/server/main.go` (full admin + proxy + 15 background schedulers)
**Database**: SQLite (`./data-baseline/hub.db`, auto-migrated from scratch)
**Configuration**: PORT=4000, proxy auth enabled, all 15 background schedulers active

---

## 1. Methodology

| Step | Description |
|------|-------------|
| 1 | Start server from cold (no pre-existing DB). Wait for bootstrap + migration + all 15 schedulers. |
| 2 | Capture idle baseline: WorkingSet64, goroutine count, heap stats via `/debug/vars`. |
| 3 | Round 1: 100 concurrent `POST /v1/chat/completions` proxy requests. |
| 4 | Capture post-load metrics immediately. |
| 5 | Wait 30s; capture stabilized metrics. |
| 6 | Round 2: 100 more concurrent requests; capture post-GC metrics. |
| 7 | Round 3: 200 concurrent requests; poll peak RSS every 500ms during load. |
| 8 | Round 4: 300 concurrent requests; poll peak RSS every 500ms during load. |
| 9 | Final 30s cooldown; goroutine stability check (3 samples, 1s apart). |

All requests targeted `POST /v1/chat/completions` with `Authorization: Bearer <PROXY_TOKEN>` and payload `{"model":"gpt-4","messages":[{"role":"user","content":"hello"}]}`. No upstream channels were configured -- requests exercised the full proxy middleware + auth + routing + channel-selection path but returned early (no available channels), so they exercised Go-side handler code paths without actual upstream HTTP calls.

**Total requests sent**: 600 (100 + 100 + 200 + 300). All returned HTTP 200.

---

## 2. Results

### 2.1 RSS (Working Set)

| Phase | WorkingSet64 (MB) | PrivateMemorySize64 (MB) | Delta from idle |
|-------|-------------------|--------------------------|-----------------|
| Idle (cold start) | 20.3 | 57.0 | -- |
| Post round 1 (100 req) | 21.2 | 57.6 | +0.9 MB |
| Stabilized (30s after R1) | 21.2 | 57.7 | +0.9 MB |
| Post round 2 (100 req) | 22.5 | 58.9 | +2.2 MB |
| Stabilized (30s after R2) | 22.6 | 58.8 | +2.3 MB |
| **Peak round 3** (200 req) | 22.6 | -- | +2.3 MB |
| **Peak round 4** (300 req) | **22.8** | -- | **+2.5 MB** |
| Final cooldown (30s after R4) | 22.3 | 59.0 | +2.0 MB |

**Idle-to-peak delta: +2.5 MB (12.3% increase)**. WorkingSet returned to 22.3 MB after cooldown, indicating no sustained leak.

### 2.2 Heap Memory

| Phase | heapAlloc (MB) | heapInuse (MB) | heapIdle (MB) | heapObjects | numGC |
|-------|---------------|----------------|---------------|-------------|-------|
| Idle | 1.20 | 3.60 | 11.71 | 5,320 | 1 |
| Post round 1 | 2.58 | 4.51 | 10.77 | 24,228 | 1 |
| Stabilized R1 | 2.62 | 4.52 | 10.77 | 25,287 | 1 |
| Post round 2 | 1.89 | 4.03 | 11.22 | 11,336 | 2 |
| Stabilized R2 | 2.89 | 4.66 | 10.46 | 26,100 | 3 |
| Post round 4 (heavy) | 2.30 | 4.09 | 10.91 | 19,031 | 5 |
| Final cooldown | 2.36 | 4.10 | 10.90 | 20,222 | 5 |

GC cycles triggered naturally (threshold-based): 1 -> 2 -> 3 -> 5 across the full run. After each GC, heapAlloc recovered to the 1.9-2.4 MB range. heapSys remained flat at ~15.2 MB. **No monotonic heap growth** -- GC reclaims objects effectively.

### 2.3 Goroutine Count

| Phase | Goroutines |
|-------|-----------|
| Idle | 19 |
| Post round 1 (100 req, concurrent) | 19 |
| Stabilized R1 | 19 |
| Post round 2 (100 req) | 19 |
| Post round 3 (200 req) | 19 |
| Post round 4 (300 req) | 19 |
| Final cooldown (sample 1) | 19 |
| Final cooldown (sample 2) | 19 |
| Final cooldown (sample 3) | 19 |

**Goroutine count: perfectly stable at 19 throughout all phases.** No goroutine leak. The 15 background schedulers plus HTTP server goroutines sum to 19, with no stray goroutines accumulating from request handling.

### 2.4 Stack Memory

| Phase | stackInuse (KB) | stackSys (KB) |
|-------|----------------|---------------|
| Idle | 704 | 704 |
| Post round 1 | 736 | 736 |
| Post round 4 | 1,024 | 1,024 |
| Final | 1,024 | 1,024 |

Minor stack growth from 704 KB to 1,024 KB (attributed to goroutine stack expansion during concurrent load). Stable -- no unbounded growth.

---

## 3. Assessment

### 3.1 Memory: PASS

| Criterion | Result |
|-----------|--------|
| Idle RSS under 50 MB | PASS (20.3 MB) |
| Peak RSS under 100 MB under load | PASS (22.8 MB peak) |
| RSS not monotonically growing | PASS (returned to 22.3 MB after cooldown) |
| GC reclaims heap | PASS (5 natural GCs, heapAlloc recovered each time) |
| heapSys flat (no OS heap growth) | PASS (15.2 MB stable) |

### 3.2 Goroutines: PASS

| Criterion | Result |
|-----------|--------|
| Goroutine count stable | PASS (19, zero deviation across all phases) |
| No leak under concurrent load | PASS (300 concurrent requests, same count) |
| Request-handling goroutines terminate | PASS (count did not increase after load) |

### 3.3 Stack: PASS

| Criterion | Result |
|-----------|--------|
| stackInuse stable | PASS (704 KB -> 1,024 KB, bounded) |
| No runaway recursion | PASS |

---

## 4. Bug Found and Fixed

During cold-start testing, the server panicked with a nil pointer dereference in `Sub2APIRefreshScheduler.Start()`:

```
panic: runtime error: invalid memory address or nil pointer dereference
goroutine 9 [running]:
github.com/tokendancelab/metapi-go/scheduler.(*Sub2APIRefreshScheduler).Start.func1()
    sub2api_refresh.go:58 +0x6f
```

**Root cause**: `app/services.go:68` called `registry.StartAll(nil)`, passing a nil `context.Context`. The scheduler's internal goroutine performed `<-ctx.Done()` on the nil context, causing a nil-interface dereference.

**Fix applied**: Changed `registry.StartAll(nil)` to `registry.StartAll(context.Background())` in `app/services.go`. Rebuilt binary successfully.

---

## 5. Recommendation

- **Memory**: No action required. The Go runtime manages memory efficiently. The 20 MB idle / 23 MB peak footprint is well within acceptable bounds for a single-binary API proxy.
- **Goroutines**: No action required. The scheduler goroutine pool plus HTTP handler goroutines are well-behaved.
- **GC**: GC runs at Go defaults. For high-throughput production with sustained load, consider `GOGC=50` or a soft memory limit via `GOMEMLIMIT` if the host has constrained RAM, but this is not needed at current scale.
- **Nil-context bug**: The fix in `app/services.go` should be committed. All other schedulers may have similar nil-context vulnerabilities -- audit recommended.
