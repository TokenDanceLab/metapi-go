# Background channel health probing (#114)

ModelProbeScheduler collects probe outcomes and applies them via ApplyProbeOutcome
(RecordProbeSuccess/Failure + model_availability upsert). SetGlobalModelProbeRecorder
wires TokenRouter from ConfigureProxyUpstream.

Residual: live HTTP probeRuntimeModel still TODO.
