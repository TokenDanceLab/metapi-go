# Ops probe wiring (#154)

## Changes
- ModelProbeScheduler TriggerNow + process-global registry for admin triggers
- POST /api/models/probe queues a real probe pass (no stub-probe job id)
- POST /api/sites/{id}/probe-now and SSE probe-stream call ProbeSite over active channels (limit 32)
- channel_recovery.probeCandidate uses the global model probe executor when registered

## Residual
- Ephemeral NewModelProbeScheduler(nil) path has no injected HTTP probe executor until app wires SetProbeExecutor / global scheduler at boot
- Marketplace and model-check remain separate issues (#156)
