# P4 Platform Adapters -- Implementation Review

**Review date**: 2026-07-04
**Spec**: `metapi-go/docs/specs/p4-platforms.md`
**TS reference**: `metapi/src/server/services/platforms/`
**Go implementation**: `metapi-go/platform/`
**Recommendation**: **NEEDS_FIX** (2 moderate findings; all other areas PASS)

---

## Summary

The Go implementation of 14 platform adapters is substantially complete and well-aligned with the spec. All adapters implement the `PlatformAdapter` interface, the 4-step detection pipeline is correct, all type definitions match, balance semantics for each fork family are accurate, and a comprehensive test suite exists (19 test files, 40+ tests). Two moderate findings require attention: the shield challenge retry loop is not wired into the request flow, and the adapter registration order relies on Go file-name ordering rather than the spec's explicit sequence.

---

## Finding Matrix

| ID | Severity | Area | Summary |
|----|----------|------|---------|
| F1 | MEDIUM | Shield challenge | `SolveAcwScV2` exists but never called from login/request flow |
| F2 | MEDIUM | Registration order | init() ordering by filename diverges from spec's required sequence |
| F3 | LOW | Shield challenge | XOR seed extraction relies on regex heuristics, may produce empty results |
| F4 | LOW | Detection pipeline | `context.Background()` used in DetectPlatform, no timeout |
| F5 | LOW | URL hint | Antigravity missing from DetectPlatformByURLHint host list |
| F6 | NOTE | Module layout | `title_hint.go` merged into `detect.go` (functionally equivalent) |

---

## Finding Details

### F1 [MEDIUM] Shield challenge retry loop not wired into request flow

**Spec reference**: `p4-platforms.md` lines 461-465 ("Cookie retry loop (fetchJsonRawWithCookie)")

**Current state**: The Go `base.go` function `fetchJSONRawWithCookie` makes a single request with cookie tracking. It does NOT:
- Detect HTML responses containing `var arg1=`, `acw_sc__v2`, `cdn_sec_tc`, or `<script` markers
- Call `SolveAcwScV2` to solve the shield challenge
- Retry up to 3 times as required by the spec

The `SolveAcwScV2` function exists in `newapi.go` (lines 1299-1431) but is exported as a public function and never invoked internally. In the TS reference (`newApi.ts` lines 715-775), `fetchJsonRawWithCookie` is a private method that:
1. Detects HTML/text responses (not JSON)
2. Calls `this.solveAcwScV2(text)` to solve the challenge
3. Injects the solved `acw_sc__v2` cookie
4. Recurses with the updated cookie header (up to 3 attempts)

**Impact**: NewApi deployments behind Alibaba Cloud WAF/CDN (which serve `acw_sc__v2` challenges) will fail login with "shield challenge blocked login" instead of retrying.

**Remediation**: Add shield detection and retry logic to `fetchJSONRawWithCookie` in `base.go`, or create a NewApi-specific wrapper in `newapi.go` that wraps each fetch with the retry loop.

**Relevant files**:
- `<repo>/platform/newapi.go:423` -- login returns "shield challenge blocked login" on parse failure
- `<repo>/platform/base.go:291-337` -- `fetchJSONRawWithCookie` lacks retry/shield logic
- `<metapi-ts>/src/server/services/platforms/newApi.ts:715-775` -- TS reference implementation

---

### F2 [MEDIUM] Registration order does not match spec

**Spec reference**: `p4-platforms.md` lines 289-315 ("Adapter registration order -- must strictly match")

**Current state**: Go adapters register via `init()` functions, whose execution order follows Go's file-name lexical order within a package:

| Position | Go order (filename) | Spec order |
|----------|--------------------|------------|
| 1 | antigravity | openai |
| 2 | anyrouter | codex |
| 3 | claude | claude |
| 4 | cliproxyapi | gemini |
| 5 | codex | gemini-cli |
| 6 | donehub | antigravity |
| 7 | gemini | cliproxyapi |
| 8 | gemini-cli | anyrouter |
| 9 | newapi | done-hub |
| 10 | oneapi | one-hub |
| 11 | onehub | veloera |
| 12 | openai | new-api |
| 13 | sub2api | sub2api |
| 14 | veloera | one-api |

The spec explicitly states the rationale: "Specific forks before generic adapters for better auto-detection" and "OneApi last (its HTTP probe is the broadest condition, serving as catch-all)."

**Impact**: While the 4-step pipeline mitigates practical impact (URL hint at step 1 catches 11/14 platforms before sequential probe), the current order causes:
- `cliproxyapi` HTTP probe runs early (position 4 vs spec 7), making an unnecessary HTTP request for every URL that falls through to step 3
- `openai` runs last among keyword matchers (position 12 vs spec 1), though this is purely cosmetic since keyword checks are fast
- `antigravity` URL keyword check runs first (position 1 vs spec 6) -- low risk but deviates from intent

**Remediation**: Replace file-name-driven `init()` registration with an explicit `InitRegistry()` function called at startup that iterates a hardcoded ordered slice. Or rename files with numeric prefixes to force the desired order (e.g., `01_openai.go`, `02_codex.go`). The test at `registry_test.go:27` already acknowledges this gap:

```go
// order is determined by init() which runs in file-name order;
// the key invariant is that all 14 are registered uniquely
```

**Relevant files**:
- `<repo>/platform/registry.go:49-52` -- `Register()` called from per-file `init()`
- `<repo>/platform/registry_test.go:27-51` -- test checks presence, not order

---

### F3 [LOW] Shield challenge XOR seed extraction may always return empty

**Spec reference**: `p4-platforms.md` lines 453-458 ("solveAcwScV2")

**Current state**: The `parseChallengeXorSeed` function in `newapi.go` (lines 1380-1431) cannot extract the XOR seed without executing JavaScript. The function attempts regex heuristics in `solveXorSeedThroughRegex` but ultimately returns empty:

```go
func solveXorSeedThroughRegex(html string) string {
    // ... regex extraction ...
    return "" // always returns empty
}
```

The test at `newapi_test.go:247-258` documents this:
```go
t.Logf("SolveAcwScV2 result (may be empty without JS VM): %q", result)
```

**Impact**: Even once F1 is fixed (wiring shield challenges into the request flow), the solver may still produce empty results for real-world Alibaba WAF challenges that require JS execution.

**Remediation**: Either integrate a JS VM (e.g., `goja`, `v8go`) to execute the `a0i()`/`a0j(0x115)` functions, or document this as a known limitation and accept that shield-protected NewApi deployments will require manual cookie import via the site UI as a workaround.

**Relevant files**:
- `<repo>/platform/newapi.go:1380-1431` -- `parseChallengeXorSeed`

---

### F4 [LOW] No timeout on DetectPlatform sequential probe context

**Spec reference**: Implicit -- edge case "platform response timeout" (line 758)

**Current state**: `DetectPlatform` at `registry.go:78` creates `context.Background()` with no deadline for the sequential probe loop:

```go
ctx := context.Background()
for _, a := range registry {
    ok, _ := a.Detect(ctx, rawURL)
    if ok {
        return a
    }
}
```

Individual adapters that make HTTP requests (NewApi, OneApi, Veloera, CliProxyApi, Sub2Api) rely on the HTTP client's 30-second timeout from `DoWithProxy`. For URL-keyword-only adapters, this is instant.

**Impact**: In the worst case, if multiple HTTP-probe adapters time out at 30s each, the detection pipeline could take minutes. The TS title-hint code uses a 5-second timeout (`fetchTextWithTimeout`).

**Remediation**: Add a context with a reasonable timeout (e.g., 30 seconds total for the entire sequential probe) and respect context cancellation.

**Relevant files**:
- `<repo>/platform/registry.go:97-103`

---

### F5 [LOW] Antigravity missing from URL hint host list

**Spec reference**: `p4-platforms.md` table row for Antigravity: "Detect: URL keyword: antigravity"

**Current state**: `DetectPlatformByURLHint` in `detect.go` checks host-based patterns for 11 platforms but does NOT include an antigravity host check. The spec says the Detect method uses URL keyword match, and antigravity's Detect does `strings.Contains(lower, "antigravity")`. However, Step 1 (URL hint) does not check for antigravity in the host.

**Impact**: If an antigravity site has "antigravity" only in the path (not host), it falls through to step 3 (sequential probe) instead of being caught at step 1. If it's also in the host, the host wouldn't match any current URL hint rule. Step 3 would catch it via antigravity's Detect at position 1 (URL keyword fast), so the practical impact is negligible.

**Remediation**: Add `strings.Contains(host, "antigravity")` check to `DetectPlatformByURLHint`.

**Relevant files**:
- `<repo>/platform/detect.go:69-125` -- URL hint function

---

### F6 [NOTE] Module file layout differs from spec

**Spec reference**: `p4-platforms.md` module structure lists `title_hint.go` as a separate file.

**Current state**: All title hint logic (`titleRules`, `titleFirstPlatforms`, `DetectPlatformByTitle`, `extractHTMLTitle`) resides in `detect.go` alongside the URL hint and detection pipeline functions.

**Impact**: None -- functionally equivalent. The spec's module structure is a recommendation, not a requirement for correctness.

**Relevant files**:
- `<repo>/platform/detect.go` -- contains both detection pipeline and title hint logic

---

## Acceptance Criteria Checklist

| Criterion | Status | Notes |
|-----------|--------|-------|
| 14 adapters implement PlatformAdapter interface | PASS | All 14 registered via init() |
| detectPlatform 4-step pipeline, correct registration order | NEEDS_FIX | F2: order deviates from spec |
| Inheritance chain correct | PASS | StandardAdapter -> OpenAI/Claude/Gemini/CliProxyApi, OneApi -> OneHub -> DoneHub, NewApi -> AnyRouter, Gemini -> GeminiCli |
| StandardApiProvider returns unsupported for login/checkin/balance | PASS | StandardAdapter defaults confirmed |
| NewApi: cookie fallback + user-ID probing + shield challenge | NEEDS_FIX | F1: shield retry not wired; cookie fallback + user-ID probing complete |
| Sub2Api: subscription summary + envelope + URL resolution | PASS | Full implementation with all edge cases |
| Veloera: 1,000,000 divisor | PASS | Confirmed at veloera.go:103 |
| DoneHub: quota+used formula (quota=remaining) | PASS | Confirmed at donehub.go:49-51 |
| OneApi: double DELETE (trailing slash fallback) | PASS | Confirmed at oneapi.go:213-251 |
| SiteProxy SOCKS5/HTTP proxy | PASS | Confirmed at site_proxy.go |
| At least one happy-path integration test per platform | PASS | 19 test files covering all adapters + detection pipeline |
| platformUserId as optional parameter in all methods | PASS | `*int` type on all 13 methods |

---

## Test Coverage Assessment

The test suite covers:

| Test File | Coverage |
|-----------|----------|
| `registry_test.go` | Adapter count (14+), registration uniqueness, GetAdapter, NormalizePlatformAlias (22 aliases), DetectPlatform URL hints (11 platforms), title rules (17 titles), titleFirstPlatforms set, normalizeURLToOrigin |
| `adapter_test.go` | Interface type definitions |
| `base_test.go` | Login token extraction, GetUserInfo, VerifyToken, Checkin default, cookie helpers, auth headers, `hasUsableSessionCookie`, token list parsing (6 formats), `findFirstEnabledToken`, `pickTokenID`, `buildCookieCandidates`, `mergeSetCookie` |
| `standard_test.go` | StandardAdapter unsupported messages, `fetchModelsFromStandardEndpoint`, URL normalization |
| `newapi_test.go` | Login (cookie fallback, session cookie), Checkin (bearer + cookie + sign_in + alt userID), GetBalance (quota+used formula, cookie fallback), GetModels (openai compat + user models + cookie), Token CRUD (list/create/delete + cookie fallback), VerifyToken, GetUserGroups, Shield challenge solver (empty inputs, partial inputs) |
| `oneapi_test.go` | Checkin, GetBalance (quota-used formula), GetModels, Token CRUD, double DELETE |
| `onehub_test.go` | GetModels /api/available_model fallback, GetUserGroups inheritance |
| `donehub_test.go` | Checkin always unsupported, GetBalance remaining-quota, GetSiteAnnouncements |
| `veloera_test.go` | Checkin (requires platformUserId), GetBalance 1M divisor, GetModels |
| `sub2api_test.go` | Login unsupported, Checkin unsupported, GetBalance (USD conversion), GetModels multi-endpoint, Token CRUD, GetUserGroups (5 endpoints + key inference), GetSiteAnnouncements, URL resolution, envelope parsing |
| `anyrouter_test.go` | Detect URL keyword, inheritance confirmation |
| `openai_test.go` | GetModels /v1/models |
| `claude_test.go` | GetModels (native + /anthropic strip + OpenAI-compat fallback) |
| `codex_test.go` | Login OAuth-only, Checkin unsupported, GetUserInfo nil, GetBalance zero, GetModels empty |
| `gemini_test.go` | Detect URL keywords, GetModels 3-path |
| `gemini_cli_test.go` | Detect override cloudcode-pa |
| `antigravity_test.go` | Login unsupported, Checkin unsupported, GetModels /v1internal:fetchAvailableModels |
| `cliproxyapi_test.go` | Detect (port 8317, cliproxy keyword, x-cpa-* headers), GetModels |
| `detect_test.go` | URL hint, title hint (matching + non-matching), detect pipeline |
| `site_proxy_test.go` | DoWithProxy, explicit proxy, custom headers, InsecureSkipTLS |

Missing from test suite:
- NewApi shield challenge retry loop integration test (not testable since solver not wired -- see F1)
- NewApi login shield challenge with `acw_sc__v2` HTML response simulation
- `DetectPlatformByURLHint` test for antigravity host keyword (see F5)
- `DetectPlatform` step 2 title-hint short-circuit integration test (currently tested at unit level only)

---

## Architecture Notes

1. **Go struct embedding for inheritance**: The Go implementation correctly uses struct embedding to mirror the TS class inheritance chain. `StandardAdapter` embeds `*BaseAdapter`, adapter-specific structs embed their parent. Method overriding works through Go's method promotion rules.

2. **fetchJSON as universal HTTP helper**: `base.go` provides `fetchJSON`, `fetchJSONRaw`, `fetchJSONRawWithCookie`, and `fetchText` as package-level helpers used by all adapters. This centralizes HTTP concerns (proxy, error formatting, JSON parsing) but creates a coupling point -- any change to `fetchJSON` affects all 14 adapters.

3. **Gob decoding**: The `extractGobFieldInts` function in `newapi.go` (lines 208-259) implements the full Gob binary protocol for user-ID extraction from cookie payloads. This matches the TS implementation's `decodeGobSignedInt` + `extractGobFieldInts` (spec lines 442-448).

4. **DetectPlatformByURLHint vs adapter Detect methods**: The URL hint function at step 1 checks host-based patterns (host == "api.openai.com", etc.) while per-adapter Detect methods check URL keywords (strings.Contains). This means a site like `https://my-proxy.example.com/antigravity/v1` would not match at step 1 (host doesn't contain "antigravity") but would match antigravity's Detect at step 3. This is intentional -- step 1 is a fast host-based filter, step 3 is the more thorough check.

---

## Conclusion

The implementation is **functionally sound for the common case** -- all 14 adapters implement their specified behavior correctly, the detection pipeline is correct, balance semantics match each fork family, and the test suite provides good coverage. The two moderate findings (F1 and F2) should be addressed before production deployment:

- **F1** (shield challenge wiring) is the more impactful gap -- it prevents login on Alibaba WAF-protected NewApi sites.
- **F2** (registration order) is a spec compliance issue that has limited practical impact due to the 4-step pipeline but should be fixed to match the explicit ordering rationale in the spec.
