# Ops probe wiring (#154)

## Changes
-  + process-global registry for admin triggers
-  queues a real probe pass (no  job id)
-  and SSE  call  over active channels (limit 32)
-  uses the global model probe executor when registered

## Residual
- Ephemeral  path has no injected HTTP probe executor until app wires  / global scheduler at boot
- Marketplace and model-check remain separate issues (#156)
