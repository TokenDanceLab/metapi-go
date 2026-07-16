# Richer Prometheus + optional OTEL/Langfuse export (#117)

**Date:** 2026-07-17  
**Lane:** competitive learn / observability  
**SSOT code:** `handler/shared/metrics.go`, `app/prometheus.go`, `handler/proxy/upstream.go`  
**Peers:** LiteLLM `integrations/*` (Langfuse/OTEL/Prometheus), AxonHub observability surface

## Goal

Expand MetAPI beyond basic counters so operators can build SLOs (latency histograms, labeled outcomes) and optionally fan out privacy-safe terminal events to external APM/tracing backends **without** embedding a vendor SDK in the core binary.

## Prometheus series (additive)

Existing scrape series are preserved. New series are additive and only use **bounded** labels.

| Metric | Type | Labels | Notes |
|:-------|:-----|:-------|:------|
| `metapi_uptime_seconds` | gauge | — | unchanged |
| `metapi_proxy_requests_total` | counter | — | unchanged; +1 at `dispatchUpstream` entry |
| `metapi_proxy_errors_total` | counter | — | still present; also increments on non-success **terminal** outcomes |
| `metapi_proxy_streams_active` | gauge | — | unchanged |
| `metapi_active_channels` | gauge | — | unchanged |
| `metapi_db_connections_open` | gauge | — | unchanged |
| `metapi_route_rebuild_total{result}` | counter | `result` | unchanged (`completed`) |
| **`metapi_proxy_outcomes_total`** | counter | `endpoint`, `status`, `stream` | **new** terminal outcomes |
| **`metapi_proxy_request_duration_seconds`** | histogram | `endpoint`, `status` | **new** latency distribution |

### Label allowlists (cardinality policy)

| Label | Allowed values | Forbidden |
|:------|:---------------|:----------|
| `endpoint` | `chat`, `messages`, `responses`, `embeddings`, `completions`, `images`, `rerank`, `search`, `gemini`, `models`, `other` | raw URL paths, model names, site ids |
| `status` | `success`, `error`, `timeout`, `unavailable`, `upstream_error`, `client_error` | raw HTTP codes, free-text errors |
| `stream` | `true`, `false` | — |

Unknown endpoint/status strings collapse via `sanitizeEndpoint` / `sanitizeStatus`.

### Latency buckets (seconds)

`0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120` (+Inf)

Covers first-byte-ish short calls through long streaming completions. Bucket choice is fixed in-process (no config yet) to keep scrape stable.

## Hot-path recording

```
dispatchUpstream
  RecordProxyRequest()
  startedAt := now
  ...
  on terminal response (success or final error):
    ObserveProxyOutcome({endpoint, status, stream, latency})
      · outcomes counter
      · duration histogram
      · proxy_errors_total if status != success
      · Observer.ObserveProxy (optional export hook)
```

Latency for successful upstream attempts uses the existing first-byte/total attempt timer (`latencyMs` around `sendUpstreamRequest`). Outer selection failures use wall time since `dispatchUpstream` entry.

Auth/JSON validation failures that never enter `dispatchUpstream` still do **not** appear in proxy metrics.

## Optional export hook (Observer)

```go
// handler/shared
type ProxyObservation struct {
    Endpoint string        // bounded
    Status   string        // bounded
    Stream   bool
    Latency  time.Duration
}

type Observer interface {
    ObserveProxy(obs ProxyObservation)
}

func SetObserver(o Observer) // nil => no-op
func GetObserver() Observer  // never nil
```

App surface:

```go
app.SetMetricsObserver(mySink)
app.ObserveProxyOutcome(shared.ProxyObservation{...})
```

### Design rules

1. **No mandatory vendor dependency** — core stays zero-dep for Prometheus text exposition.
2. **No-op default** — production is safe without registration.
3. **Privacy-safe payload only** — no prompts, keys, cookies, upstream bodies, model names, site URLs.
4. **Non-blocking sinks** — implementers should buffer/async; `ObserveProxyOutcome` recovers panics from sinks so a bad hook cannot crash the proxy path.
5. **One event per terminal client response** — retries that later succeed emit a single success outcome for the winning attempt (not every intermediate soft fail).

### Example sink sketches (not shipped)

**OpenTelemetry-ish (metrics only):**

```go
type OTELSink struct{ hist metric.Float64Histogram }

func (s OTELSink) ObserveProxy(obs shared.ProxyObservation) {
    s.hist.Record(context.Background(), obs.Latency.Seconds(),
        metric.WithAttributes(
            attribute.String("endpoint", obs.Endpoint),
            attribute.String("status", obs.Status),
            attribute.Bool("stream", obs.Stream),
        ))
}
```

**Langfuse-style (metadata only, no prompts):**

```go
type LangfuseMetaSink struct{ enqueue func(map[string]any) }

func (s LangfuseMetaSink) ObserveProxy(obs shared.ProxyObservation) {
    s.enqueue(map[string]any{
        "name": "metapi.proxy",
        "endpoint": obs.Endpoint,
        "status": obs.Status,
        "stream": obs.Stream,
        "latency_ms": obs.Latency.Milliseconds(),
    })
}
```

Register at composition root (`cmd/server` / `app`) only when env/config opts in. Credentials for SaaS stay out of this repo.

## Privacy / non-goals

| In scope | Out of scope |
|:---------|:-------------|
| Bounded labels + histograms | High-cardinality model/site labels without aggregation policy |
| Optional Observer interface | Shipping Langfuse/OTEL SDKs or SaaS tokens in-repo |
| Prometheus text at `GET /metrics` | Auth on `/metrics` (still network-policy based) |
| Terminal outcome export | Full distributed traces with prompt payloads |

## Tests

- `handler/shared/metrics_test.go` — legacy counters, labeled outcomes, histogram buckets/count, sanitization of high-cardinality input, Observer hook.
- `app/prometheus_test.go` — `/metrics` exposition includes new HELP/TYPE lines and sample series; `SetMetricsObserver` surface.

## Residual risks

1. **Cardinality growth** is bounded by the allowlists, but operators enabling custom sinks must not re-introduce raw model labels.
2. **Error counter semantics expanded**: non-success terminal outcomes now increment `metapi_proxy_errors_total` (not only “unconfigured upstream”). Dashboards that treated that counter as “config errors only” need a one-time adjustment; prefer `metapi_proxy_outcomes_total{status=...}` going forward.
3. Soft protocol fallbacks / intermediate retries do not emit outcomes until a terminal response is written to the client.
4. Stream active gauge is still optional/manual; this issue does not re-plumb `RecordStreamStart/End` into every SSE path.
