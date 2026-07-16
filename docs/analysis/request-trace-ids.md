# Request trace IDs across retries (#110)

## Behavior
- Chi `RequestID` middleware continues to set `X-Request-Id` on responses.
- Proxy JSON errors echo `error.request_id` when the header is present so clients can correlate without relying solely on headers.
- Successful `proxy_logs` rows store `request_id` inside `billing_details` (until a dedicated column is added).
- Success/failure slog lines include `request_id` when available so multi-channel retries share one correlation key.

## Residual
- No dedicated `proxy_logs.request_id` column yet (schema additive change deferred).
- Full OTEL/Langfuse export remains a separate learn item (#117).
