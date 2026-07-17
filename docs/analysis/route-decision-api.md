# Route decision API (#171)

## Endpoints
- GET /api/routes/decision?model= -> TokenRouter.ExplainSelection
- POST /api/routes/decision/batch {models:[]} (max 50)
- by-route/route-wide batch currently alias model batch
- POST /api/routes/decision/refresh queues async warm of up to 20 enabled patterns

## Behavior
Requires UpstreamConfig.Router (503 if not configured). No empty success stubs.
