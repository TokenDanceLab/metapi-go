# Probe boot wiring (#170)

**Date:** 2026-07-17  
**Lane:** enterprise polish / probe-boot  
**SSOT code:** `app/probe_wire.go`, `app/services.go`, `app/proxy_upstream.go`, `handler/proxy/channel_health_probe.go`  
**Peers:** #114 channel health probing, #119 admin channel test harness, #154 ops probe APIs

## Goal

Admin probe paths and background `ModelProbeScheduler` must hit **live upstream** and feed routing health — not skip every target because `SetProbeExecutor` / `SetHealthRecorder` were never injected at boot.

## Boot sequence

```
cmd/server
  ConfigureProxyUpstream(cfg)
    · build routing.TokenRouter
    · rememberProbeRouter(cfg, router)
    · WireGlobalModelProbeScheduler()   // no-op if scheduler not started yet
  StartBackgroundServices()
    · modelProbe := NewModelProbeScheduler(cfg)
    · WireModelProbeScheduler(modelProbe)   // inject probe + recorder
    · Register + StartAll
    · Start always SetGlobalModelProbeScheduler (enabled or disabled)
```

Admin `probe-now` / `probe-stream` / `POST /api/models/probe` and channel-recovery then use `GetGlobalModelProbeScheduler()` with a real executor.

## Components

| Piece | Package | Role |
|-------|---------|------|
| `ChannelHealthProbeExecutor` | `handler/proxy` | `GET /v1/models` (fallback `POST /v1/chat/completions` on 404/405) via `platform.DoWithProxy` |
| `tokenRouterHealthRecorder` | `app` | Adapts `TokenRouter.RecordProbeSuccess/Failure` |
| `WireModelProbeScheduler` | `app` | Composition-root inject into scheduler |

### Success / failure

- **2xx** → `RecordProbeSuccess` (clear credential-scoped cooldown + probe stamp)
- **Transport error / non-2xx** → `RecordProbeFailure` (cool **probed channel only**; never expire keys)
- **Missing token / channel load error** → inconclusive (no health mutation)

## Residual

- Platforms that expose neither OpenAI-style `/v1/models` nor `/v1/chat/completions` still probe as failure after both attempts (or after models-only when model name is empty). Native Anthropic/Gemini path-shaped sites may need a later adapter mode.
- OAuth-only accounts without a resolvable bearer token stay inconclusive.
- Ephemeral `NewModelProbeScheduler(nil)` fallbacks in admin handlers still have no HTTP executor until the global scheduler is started via `StartBackgroundServices`.

## Verify

```bash
go test ./app ./scheduler ./handler/proxy ./handler/admin -count=1
```
