# Residual: account health refresh (#195)

**Date**: 2026-07-17  
**Issue**: [#195](https://github.com/TokenDanceLab/metapi-go/issues/195)  
**Lane**: residual accounts health

## Problem

`POST /api/accounts/health/refresh` previously returned success theater:

- `wait=true` → empty zeroed summary / empty results (no work)
- `wait=false` → fake `jobId: "stub-health-refresh"` with no runner

## Implemented behavior

Health probe is implemented by reusing the real balance-refresh path
(`service/balance.RefreshBalance`), which already writes `extraConfig.runtimeHealth`
(`healthy` / `unhealthy` / `disabled`, source `balance`) and updates balance fields.

| Mode | Behavior |
|------|----------|
| `wait=true` | Synchronously refresh target account(s); return `summary` + `results` + human `message` |
| `wait=false` | Queue in-process `StartBackgroundTask` (`type=account-runtime-health-refresh`) with real `jobId`/`taskId` and dedupe keys |
| `accountId` set | Single-account path; 404 if missing |
| `accountId` omitted | Batch over all account ids (`ORDER BY id`) |

### Per-account outcomes

| Case | `status` | Notes |
|------|----------|-------|
| Session-capable balance refresh OK | `success` | State usually `healthy` (or preserved checkin `degraded`) |
| Balance refresh error | `failed` | Runtime health set by balance service to `unhealthy` when applicable |
| Account/site disabled | `skipped` | Balance service marks `disabled` runtime health |
| Proxy-only / API-key / OAuth | `skipped` | Honest `reason=proxy_only` — no fake success, no upstream call |
| Unsupported platform / missing row mid-run | `failed` | No stub success |

### Async tracking

- Uses the existing in-memory admin task registry (`handler/admin/tasks.go`).
- Dedupe keys:
  - single: `refresh-account-runtime-health-{id}`
  - batch: `refresh-all-account-runtime-health`
- Response shape mirrors token-sync async:

```json
{
  "success": true,
  "queued": true,
  "reused": false,
  "jobId": "<hex>",
  "taskId": "<hex>",
  "status": "pending",
  "message": "已开始刷新账号运行健康状态，请稍后查看账号列表"
}
```

Task result payload (via `GET /api/tasks/:id`):

```json
{ "summary": { "...": 0 }, "results": [ { "accountId": 1, "status": "success", "state": "healthy" } ] }
```

## Residuals / honest limits

1. **No durable multi-instance job store.** Task registry is process-local; multi-replica deployments do not share `jobId` state. This is intentional and matches other admin background jobs (announcement sync, account-token sync).
2. **Probe mechanism = balance refresh only.** Model-discovery / channel probe paths are not invoked here. Proxy-only accounts remain skipped (TS parity: no session balance probe).
3. **No separate model-health probe for API keys.** If product later needs API-key runtime health via model list, that is a new residual outside #195.
4. **Batch wait mode is sequential** per account (simple correctness over fan-out). Large fleets may prefer async mode.
5. **UI currently calls without `wait`** (`api.refreshAccountHealth()`), so operators see toast + list reload; detailed summary is available when callers pass `wait: true` or poll `/api/tasks/:id`.

## Tests

```bash
go test ./handler/admin -count=1 -run Health
```

Coverage:

- sync batch: session success + apikey skip + persisted runtimeHealth/balance
- single `accountId` wait path
- missing `accountId` → 404
- async real `jobId` (not `stub-health-refresh`) + runner completion
- async dedupe reuse
