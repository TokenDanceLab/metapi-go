# Gemini `/v1beta/models` list via owned catalog

> Date: 2026-07-17  
> Issue: TokenDanceLab/metapi-go #215  
> Scope: proxy surface `handler/proxy/gemini.go` (`HandleGeminiModelsList`)

## Purpose

Document how MetAPI serves Gemini Generative Language **models.list** responses from the **MetAPI-owned model catalog**, instead of a hard-coded success stub or a live upstream scrape.

## Endpoints

| Item | Value |
|------|--------|
| Paths | `GET /v1beta/models`, `GET /gemini/{geminiApiVersion}/models` |
| Auth | Proxy auth required (missing → **401**) |
| Handler | `HandleGeminiModelsList` / `HandleGeminiModelsListDynamic` |
| Generate paths | Unchanged: `POST …/models/*` and Gemini CLI still use `dispatchUpstream` |

## Owned vs hard-coded vs upstream

| Mode | Behavior | Status |
|------|----------|--------|
| **Owned listing** (current) | Resolve catalog via `getAvailableModels` → `resolveOwnedModelCatalog` / `AvailableModelsSource`, filter gemini-ish names when mixed, map to Gemini list JSON | **Implemented** (#215) |
| **Hard-coded stub list** | Always returned `gemini-2.5-pro` + `gemini-2.5-flash` on success | **Removed** |
| **Upstream Generative Language list** | Proxy vendor `models.list` body | **Not implemented** (not the default) |

Catalog resolution is shared with `GET /v1/models` (see `docs/analysis/models-response-shape.md`):

| Condition | Catalog source |
|-----------|----------------|
| Router implements `AvailableModelsSource` | `GetAvailableModels(ctx)` (normalized + sorted) |
| Router listing error | Empty list + warn log |
| Selection-only router (no listing method) | Last-resort stub catalog residual |
| No router + `METAPI_ENABLE_PROXY_STUB=1` | Last-resort stub catalog residual (unit tests) |
| No router + stub disabled | Empty list + one-shot warn (production safety) |

Policy filtering (`SupportedModels`, `DenyAllWhenEmpty`, …) is applied by `getAvailableModels` before Gemini mapping.

## Gemini response shape

```json
{
  "models": [
    {
      "name": "models/gemini-2.5-pro",
      "displayName": "Gemini 2.5 Pro",
      "description": "Gemini 2.5 Pro model",
      "supportedGenerationMethods": [
        "generateContent",
        "streamGenerateContent",
        "countTokens"
      ]
    }
  ]
}
```

| Field | Notes |
|-------|--------|
| `models` | Always present; may be `[]` when catalog empty / deny-all / listing error |
| `name` | Resource form `models/<id>`; optional input `models/` prefix is stripped then re-applied |
| `displayName` | Lightweight humanization of the id (`gemini-2.5-flash` → `Gemini 2.5 Flash`) |
| `description` | `{displayName} model` (stable client-facing filler; not scraped from vendor) |
| `supportedGenerationMethods` | Fixed capability set for owned Gemini entries |

## Gemini-ish filtering

When the owned catalog is **mixed** (OpenAI + Claude + Gemini route names), list entries whose names contain `gemini` (case-insensitive) are preferred.

If **no** gemini-ish names exist, **all** owned models are still mapped into the Gemini envelope. Empty `models` is reserved for truly empty catalogs (or policy denial / listing failure), not for “no gemini substring”.

## Residual / stub fallback

- Last-resort stub catalog remains a **shared residual** of `resolveOwnedModelCatalog` for unit tests and selection-only routers. It is not a Gemini-handler hard-code.
- When that residual catalog is mixed, Gemini list still filters to gemini-ish names (`gemini-2.5-pro`, `gemini-2.5-flash` today).
- Production with a wired `TokenRouter` that implements `AvailableModelsSource` never depends on the residual stub for success.

## Auth

Unauthorized callers (no proxy auth context) receive **401** with the standard JSON error envelope. Catalog resolution is not attempted.

## Verify

```bash
go test ./handler/proxy -count=1 -run Gemini
```

## Non-goals

- Live upstream Gemini `models.list` merge
- Per-model method discovery from vendor metadata
- Changing generateContent / Gemini CLI dispatch behavior
