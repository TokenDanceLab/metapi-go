# `/v1/models` response shape

> Date: 2026-07-17  
> Issue: TokenDanceLab/metapi-go #53 (upstream cita-777/metapi #507)  
> Scope: proxy surface `handler/proxy/models.go`

## Purpose

Document the **owned MetAPI listing** returned by `GET /v1/models`, how it differs from **live upstream** model listing, and the client-facing fields required for OpenAI / Claude / Hermes-class clients.

## Endpoint

| Item | Value |
|------|--------|
| Path | `GET /v1/models` |
| Auth | Proxy auth (`Authorization: Bearer …` or managed key middleware) |
| Handler | `handler/proxy/models.go` → `HandleModels` |
| Format selection | Claude when `anthropic-version` **or** `x-api-key` is present; otherwise OpenAI |

## Owned vs upstream listing

| Mode | What it is | Status in metapi-go |
|------|------------|---------------------|
| **Owned listing** (current) | MetAPI constructs the list itself from enabled `token_routes` via `TokenRouter.GetAvailableModels`, then filters by downstream routing policy (`SupportedModels` / deny-all / route-id constraints). `owned_by` is always `"metapi"`. | **Implemented** (#169) |
| **Upstream listing** | Proxy or merge of each site/account’s native `/v1/models` (or platform equivalent), including vendor `context_length` / `max_model_len` when present. | **Not implemented** (and not the default). This surface never scrapes live upstream `/v1/models`. |

### Catalog resolution (`resolveOwnedModelCatalog`)

| Condition | Catalog source |
|-----------|----------------|
| `UpstreamConfig.Router` implements `AvailableModelsSource` (production `routing.TokenRouter`) | `GetAvailableModels(ctx)` over enabled/visible routes |
| Router listing returns an error | Empty list + warn log (no silent stub) |
| Router is present but selection-only (no listing method) | Last-resort stub catalog (keeps e2e / selection mocks shape-compatible) |
| `UpstreamConfig` nil **and** `METAPI_ENABLE_PROXY_STUB` on | Last-resort stub catalog (unit-test default) |
| `UpstreamConfig` nil **and** stub disabled | Empty list + one-shot warn (production safety) |

Implications:

1. Clients always see a MetAPI-owned OpenAI/Claude envelope, not a raw vendor body.
2. Model IDs are the names MetAPI is willing to route (route catalog ∩ policy), not necessarily a complete dump of every upstream account’s models.
3. `owned_by: "metapi"` intentionally avoids vendor-specific client branches (e.g. Hermes’ `owned_by == "llamacpp"` → `/v1/props` path).

Residual / future hardening:

- Prefer `token_routes.context_length` (SC2 additive column) over built-in defaults when set; today `knownModelContextLength` still supplies family defaults/heuristics only.
- Optionally merge per-site discovered context metadata from platform model refresh.
- Route-ID-aware listing when policy has only `AllowedRouteIDs` (currently empty until joined).

## OpenAI-compatible shape (default)

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o",
      "object": "model",
      "created": 1710000000,
      "owned_by": "metapi",
      "context_length": 128000
    }
  ]
}
```

### Required fields (product clients)

| Field | Location | Notes |
|-------|----------|--------|
| `object` | top | always `"list"` (including empty `data`) |
| `data` | top | array (may be empty under deny-all / filtered policy) |
| `id` | item | model identifier |
| `object` | item | always `"model"` |
| `created` | item | Unix seconds (`int`); request-time stamp for owned catalog |
| `owned_by` | item | always `"metapi"` for owned listing |

### Optional fields

| Field | Location | Notes |
|-------|----------|--------|
| `context_length` | item | Token window when known. Omitted for unknown/custom IDs so clients can fall back to their own defaults/probes. |

`context_length` addresses upstream #507 / Hermes auto-detection: Hermes and similar agents scan `/v1/models` for keys such as `context_length`, `max_model_len`, `n_ctx`, etc. MetAPI emits the common OpenRouter-style key `context_length` for known catalog models.

## Claude-compatible shape

Triggered by either:

- `anthropic-version: …`
- `x-api-key: …` (without requiring Anthropic version)

```json
{
  "data": [
    {
      "id": "claude-sonnet-4-20250514",
      "type": "model",
      "display_name": "claude-sonnet-4-20250514",
      "created_at": "2026-03-19T00:00:00Z"
    }
  ],
  "first_id": "claude-sonnet-4-20250514",
  "last_id": "claude-sonnet-4-20250514",
  "has_more": false
}
```

Notes:

- No top-level `object: "list"` (OpenAI-only).
- Pagination fields mirror Anthropic Models API / upstream metapi `modelsSurface` (`first_id`, `last_id`, `has_more`).
- Empty list: `data: []`, `first_id: null`, `last_id: null`, `has_more: false`.

## Policy filtering

| Policy | Listing behavior |
|--------|------------------|
| Empty allow-all | Full owned catalog (route-backed or last-resort fallback) |
| `DenyAllWhenEmpty` with no supported models / routes | Empty `data` |
| `SupportedModels` patterns | Catalog filtered by wildcard/exact match |
| `AllowedRouteIDs` only (no supported models) | Empty until route-aware listing is wired (`AllowedRouteIDs` still enforced at channel selection) |

## Client quirks

| Client / class | Quirk | MetAPI behavior |
|----------------|-------|-----------------|
| OpenAI SDKs / LiteLLM | Expect `object` + `data[].object` + `owned_by` | Always present on OpenAI path |
| Hermes / similar agents | Read `context_length` (and aliases) from `/v1/models`; treat `owned_by: "llamacpp"` specially | Emit `context_length` when known; `owned_by` is `"metapi"` (not llamacpp) |
| Claude Code / Anthropic SDKs | Prefer Models API fields (`type`, `display_name`, pagination) | Claude path when Anthropic headers present |
| Empty catalog | Some UIs break on missing `data` | Always return `data` array |

## Tests

- Unit: `handler/proxy/models_test.go` (`TestGetAvailableModels_*`, `TestBuildOpenAIModelsResponse*`, `TestBuildClaudeModelsResponse*`, `TestHandleModels_*`)
- E2E: `e2e/e2e_flow_test.go` Phase 5 asserts OpenAI required fields including `owned_by` / `created` (selection-only mock router still uses last-resort catalog)

## References

- Issue: TokenDanceLab/metapi-go #169 (route-backed listing), #53 (shape / `context_length`)
- Upstream issue: [cita-777/metapi#507](https://github.com/cita-777/metapi/issues/507)
- Upstream surface: `src/server/proxy-core/surfaces/modelsSurface.ts`
- Schema home for future route context: `store.TokenRoute.ContextLength` / `token_routes.context_length`
- Router API: `routing.TokenRouter.GetAvailableModels`
