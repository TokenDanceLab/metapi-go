# Ops probe wiring (#154)

## Changes
- ModelProbeScheduler TriggerNow + process-global registry for admin triggers
- POST /api/models/probe queues a real probe pass (no stub-probe job id)
- POST /api/sites/{id}/probe-now and SSE probe-stream call ProbeSite over active channels (limit 32)
- channel_recovery.probeCandidate uses the global model probe executor when registered

## Residual
- Boot wiring of SetProbeExecutor + health recorder is #170 (`docs/analysis/probe-boot-wiring.md`)
- Ephemeral `NewModelProbeScheduler(nil)` admin fallbacks still lack HTTP executor until `StartBackgroundServices` registers the global scheduler
- Marketplace and model-check remain separate issues (#156)
