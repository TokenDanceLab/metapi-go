# Ops probe wiring (#154)

**Date:** 2026-07-17  
**Issue:** [#154](https://github.com/TokenDanceLab/metapi-go/issues/154)  
**Lane:** program:enterprise-ops / lane:ops-probe  
**Code:**
- `handler/admin/sites.go` (`probeNow` / `probeStream`)
- `handler/admin/site_probe.go` (forced site probe runtime)
- `handler/admin/channel_test_harness.go` (#119 transport reuse)
- `handler/admin/stats.go` (`POST /api/models/probe`)
- `scheduler/model_probe.go` + `scheduler/model_probe_jobs.go`
- `scheduler/channel_recovery.go` (`probeCandidate` → `ApplyProbeOutcome`)

## Goal

Replace admin probe stubs with real runtime paths that already exist in scheduler model probe + the #119 channel test harness.

## Before → After

| Surface | Before | After |
|---------|--------|-------|
| `POST /api/sites/{id}/probe-now` | Always `{totalModels:0, results:[]}` | Loads site accounts/models, forces chat probes via harness transport, upserts `model_availability`, auto-disables unsupported models |
| `GET /api/sites/{id}/probe-stream` | Instant `probe-start` + zero `complete` | SSE progress: `start`/`model`/`action`/`complete` (+ compatibility `probe-*` aliases) |
| `POST /api/models/probe` | `jobId: "stub-probe"` | UUID-like job id via in-memory job registry; optional `wait=true` sync completion; triggers `ModelProbeScheduler.TriggerNow` when scheduler has started |
| `channel_recovery.probeCandidate` | Log-only TODO | Injected `ChannelHealthProbe` + `ChannelHealthRecorder` through `ApplyProbeOutcome` |

## Runtime flow

### Site forced probe

```
probeNow / probeStream
  → loadSiteProbeTargets(site)
       · model names from model_availability ∪ token_model_availability ∪ route_channels.source_model
       · prefer channel+account for each model; fall back to first active account
  → for each model:
       channelTestHandler.executeProbe(mode=chat)   // #119 harness, injectable transport
       upsert model_availability(account, model, available, latency)
       if unsupported → ensure site_disabled_models row + SSE action=disabled
       stamp routing.RecordSiteProbeOutcome (site runtime only; no credential cascade)
  → summary { totalModels, available, unavailable, probed, unsupported, results[], emptyReason? }
```

Empty sites return `success=true` with `totalModels=0` and a clear `emptyReason` (no accounts/models), not a hard error.

### SSE contract (frontend Sites.tsx)

Primary event names (what the SPA parses):

| Event | Payload highlights |
|-------|--------------------|
| `start` | `scope`, `modelsCount`, `startedAt` |
| `model` | `modelName`, `status` (`supported`/`unsupported`/`skipped`), `latencyMs`, `reason`, `latencyExceeded` |
| `action` | `action=disabled`, `modelName` |
| `complete` | `probed`, `unsupported`, `available`, `unavailable`, `results` |
| `error` | `message` |

Compatibility aliases still emitted: `probe-start`, `probe-model-checked`, `probe-model-result`.

### Global model probe job

```
POST /api/models/probe { accountId?, wait? }
  → scheduler.EnqueueModelProbeJob
       · real job id (uuid-like), never "stub-probe"
       · wait=false → 202 { queued:true, jobId, status }
       · wait=true  → 200 { jobId, status:completed, summary }
  → trigger hook (registered in ModelProbeScheduler.Start) calls TriggerNow()
       · same budgeted loadProbeTargets + account leases as ticker path
```

### Channel recovery

```
probeCandidate
  → loadRecoveryProbeTarget(channel → account/site/model)
  → ChannelHealthProbe.ProbeChannel
  → ApplyProbeOutcome(recorder, target, outcome)
       · success → RecordProbeSuccess
       · failure → RecordProbeFailure (channel-local; no key expiry)
       · missing deps → skipped (safe no-op)
```

Composition root may inject probe/recorder via `SetProbeExecutor` / `SetHealthRecorder` (same ports as model-probe). Until injected, recovery remains a safe skip rather than a crash.

## Tests

```bash
go test ./handler/admin ./scheduler -count=1
```

Coverage added:
- site probe success + availability upsert (fake transport)
- empty site `emptyReason`
- SSE multi-event stream (not instant zero complete)
- unsupported auto-disable
- `/api/models/probe` real job id + wait summary
- channel recovery success/failure via fakes + `ApplyProbeOutcome`
- model probe job registry async/sync

## Non-goals

- Full marketplace catalog rewrite
- Redis multi-instance probe coordination
- Frontend redesign (`web/**` only consumed existing SSE field names)
- Wiring a production HTTP probe executor for channel recovery (still injected at composition root; ports are live)
