# Codex OAuth gpt-5.5 discovery & first-timeout

**Date:** 2026-07-17  
**Issues:** [#49](https://github.com/TokenDanceLab/metapi-go/issues/49) (upstream #571), [#55](https://github.com/TokenDanceLab/metapi-go/issues/55) (upstream #489)  
**SSOT code:** `service/oauth/codex_models.go`, `service/oauth/quota.go`, `service/oauth/account.go`

## Problem

1. **#49 / #571** — After Codex OAuth login, gpt-5.5 could appear blocked when allowlists/fixtures/quota probes only referenced obsolete models (e.g. gpt-5.4). Static audit found no hard `gpt-5.5` deny, but seed/probe paths were version-pinned.
2. **#55 / #489** — Upstream TS first model discovery used a fixed **12s** budget (`MODEL_DISCOVERY_TIMEOUT_MS = 12_000` in `modelService.ts`). Cold-start Codex cloud discovery (proxy + ChatGPT backend) often needs longer; timeout left operators with little guidance.

## Design

### Allowlist (#49)

| Helper | Rule |
|--------|------|
| `IsCodexGPT5FamilyModel` | Version-flexible `^gpt-5(\.|$|-)` (covers gpt-5.5, gpt-5.4-mini, gpt-5.2-codex, …) |
| `IsCodexModelAllowed` | gpt-5.x family always allowed; other models only if present in discovered set |
| `CodexSeedModels` | Fixture seed includes **gpt-5.5** plus recent predecessors (not obsolete-only) |
| `SelectCodexQuotaProbeModel` | Preference order starts with **gpt-5.5**, then gpt-5.4…; empty discovery → `gpt-5.5` fallback |
| `CodexQuotaProbeModelForAccount` | Uses `LastDiscoveredModels` when available |

### First discovery timeout (#55)

| Constant | Value | Notes |
|----------|-------|-------|
| `CodexModelDiscoveryTimeout` | **30s** | First attempt (was ~12s upstream) |
| `CodexModelDiscoveryRetryTimeout` | **45s** | Soft-retry budget after timeout |
| `CodexModelDiscoveryBackoff` | **750ms** | Pause between attempts |
| `CodexModelDiscoveryMaxAttempts` | **2** | Initial + one soft-retry |

Soft-retry applies **only** to timeout-class errors. Auth (`401` / unauthorized) and empty-model responses fail fast.

On exhausted timeout, `FormatCodexModelDiscoveryTimeoutStatus` writes an operator-facing message covering:

- attempt count and budget
- cold-start / proxy guidance
- `status=abnormal` until a successful discovery
- recommended budgets (30s / 45s)

`DiscoverCodexModelsWithSoftRetry` returns `CodexModelDiscoveryResult` with `Status` (`healthy`|`abnormal`), `ErrorCode`, `TimedOut`, `Attempts`, and `Models`.

`BuildOauthModelDiscoveryPatch` maps the result onto `OauthInfo` fields already stored in `extraConfig.oauth` (`modelDiscoveryStatus`, `lastModelSyncAt`, `lastModelSyncError`, `lastDiscoveredModels`).

### Cloud discovery HTTP

`DiscoverCodexModelsFromCloud` GETs `{base}/models?client_version=1.0.0` with:

- `Authorization: Bearer …`
- `Originator: codex_cli_rs`
- optional `Chatgpt-Account-Id`

Payload parsing accepts array / `models` / `data` / `items` with `id`|`slug`|`model` fields (parity with upstream `platformDiscoveryRegistry.ts`).

## Recommended operator config

| Surface | Recommendation |
|---------|----------------|
| First discovery budget | ≥ 30s (package default) |
| Soft-retry budget | ≥ 45s |
| Account proxy | Set reachable `extraConfig.proxyUrl` when MetAPI host cannot reach ChatGPT directly |
| After timeout | Re-run connection model refresh; inspect `modelDiscoveryStatus=abnormal` + `lastModelSyncError` |

Env knobs for generic model **probe** (`MODEL_AVAILABILITY_PROBE_TIMEOUT_MS`, default 15s) remain separate from this OAuth first-discovery path.

## Tests

```bash
go test ./service/oauth/ -count=1
```

Coverage includes:

- gpt-5.5 allowed with empty / older-only discovery lists
- seed + quota probe preference not obsolete-only
- success first attempt
- timeout → backoff → success
- timeout exhausted → abnormal + actionable message
- unauthorized → no soft-retry

## Residual

- Full wire-up of `DiscoverCodexModelsWithSoftRetry` into a future `refreshModelsForAccount` implementation (outside this oauth-file ownership when model service lands) should call these helpers.
- Live end-to-end OAuth + gpt-5.5 chat still needs a real Codex account (runtime probe).
