# Site initialization presets (#214)

**Date**: 2026-07-17  
**Issue**: [#214](https://github.com/TokenDanceLab/metapi-go/issues/214)  
**Spec**: `docs/specs/p3-sites-accounts.md` — `SiteCreatePayload.initializationPresetId`, `POST /api/sites/detect`

## Problem

Frontend site creation can send `initializationPresetId` (from
`web/shared/siteInitializationPresets.js`), but Go `createSite` ignored the
field (`TODO(P4)`). Mismatched / unknown presets were accepted silently.
`detectSite` also never returned `initializationPresetId`.

## Solution

Port the 13-preset registry to Go and validate on create:

| API | Behavior |
|-----|----------|
| Registry | `service.List/Get/DetectSiteInitializationPreset` |
| `POST /api/sites` | If `initializationPresetId` set → must exist, platform match, and URL match when host/path rules exist; else 400. Response includes `initializationPresetId` when known. |
| `POST /api/sites/detect` | When URL matches a preset, return protocol-family `platform` (`openai`/`claude`) + `initializationPresetId`. |

## Registry parity

Source of truth for product labels/models remains the JS file; Go is a port for
server-side validation/detection.

| id | platform | defaultUrl host/path match |
|----|----------|----------------------------|
| codingplan-openai | openai | coding.dashscope.aliyuncs.com `/v1` |
| codingplan-claude | claude | coding.dashscope.aliyuncs.com `/apps/anthropic` |
| zhipu-coding-plan-openai | openai | open.bigmodel.cn `/api/coding/paas/v4` |
| zhipu-coding-plan-claude | claude | **manual only** (no URL auto-match) |
| deepseek-openai | openai | api.deepseek.com `/`, `/v1` |
| deepseek-claude | claude | api.deepseek.com `/anthropic` |
| moonshot-openai | openai | api.moonshot.cn `/`, `/v1` |
| moonshot-claude | claude | api.moonshot.cn `/anthropic` |
| minimax-openai | openai | api.minimaxi.com `/`, `/v1` |
| minimax-claude | claude | api.minimaxi.com `/anthropic` |
| modelscope-openai | openai | api-inference.modelscope.cn `/v1` |
| modelscope-claude | claude | api-inference.modelscope.cn `/` |
| doubao-coding-openai | openai | ark.cn-beijing.volces.com `/api/coding/v3` |

Detection order (JS parity):

1. First preset whose host+paths match (optional platform filter).
2. If platform is set, fall back to `analyzePrimarySiteUrl` equality against
   each preset `defaultUrl` (covers manual-only zhipu Claude when platform is
   already `claude`).

## createSite validation

```text
empty id                → allowed (optional)
unknown id              → 400 Unknown initializationPresetId
platform mismatch       → 400 does not match platform
has match rules + bad URL → 400 does not match site URL
manual-only + platform OK → allowed without URL check
```

When the client omits the id but the URL is a known preset entry, create
auto-attaches the detected id on the response (not persisted as a DB column;
sites still store protocol/vendor `platform` only).

## DetectSite interaction

`service.DetectSite` now prefers preset host/path matches **before** vendor
hostname heuristics. That means:

- `https://api.deepseek.com/v1` → platform `openai` + `deepseek-openai`
  (not vendor tag `deepseek`)
- `https://platform.deepseek.com` → still vendor heuristic `deepseek`
- non-preset hosts keep previous heuristic platforms

This matches the frontend create flow, which selects presets with protocol
families `openai` / `claude`.

## Files

- `service/site_initialization_presets.go` (+ tests)
- `service/site_detect.go`
- `handler/admin/sites.go` (+ tests)
- `docs/analysis/site-init-presets.md`

## Residuals / honest limits

1. Preset metadata (recommended models, docs) is not persisted on the `sites`
   row; only echoed on create/detect responses.
2. Vendor heuristics (`deepseek`, `moonshot`, `zhipu`, …) remain for non-preset
   URLs; they are not rewritten to openai/claude unless a preset matches.
3. JS and Go registries can drift if one side is updated alone — treat the JS
   file as product UX SSOT and re-port when presets change.
4. `updateSite` does not accept `initializationPresetId` (create-only, same as
   payload schema).

## Tests

```bash
go test ./service ./handler/admin -count=1 -run 'Preset|Detect|Site'
```
