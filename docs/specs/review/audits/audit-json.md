# JSON Encoding Audit Report

**Project:** metapi-go (<repo>)
**Audit Date:** 2026-07-05
**Scope:** All Go source files (304 files), with focus on JSON serialization paths used in API responses
**Auditor:** Automated code analysis (no runtime execution)

---

## Executive Summary

Three distinct JSON serialization implementations coexist. One of them (the proxy handler's custom `appendJSON`) has **critical correctness defects** affecting NaN/Inf and control-character escaping. The other two (standard-library based) are safe but suffer from code duplication. The project also has hand-rolled JSON in persistence code (runtime_health) and an SSE string-interpolation pattern that bypasses full validation.

**Severity summary:**
- CRITICAL: 2 (NaN/Inf in proxy responses, incomplete control-character escaping)
- HIGH: 3 (silent null fallback for unknown types, json.NewEncoder trailing newline, int64 precision loss in custom itoa)
- MEDIUM: 3 (duplicate writeJSON implementations, SSE JSON passthrough without validation, custom JSON for persistence without string escaping)
- LOW: 2 (non-deterministic key ordering, no DisallowUnknownFields on decoders)

---

## 1. NaN / Infinity Handling

JSON (RFC 8259) does not define `NaN`, `Infinity`, or `-Infinity` as valid tokens. Go's `encoding/json` correctly returns an error when asked to marshal these values. However, `strconv.FormatFloat` produces the literal strings `"NaN"`, `"+Inf"`, `"-Inf"`.

### 1.1. CRITICAL: `ftoa` produces invalid JSON for NaN/Inf

**File:** `handler/proxy/messages.go`, line 162-168

```go
func ftoa(f float64) string {
    if f == float64(int64(f)) && f < 1e15 && f > -1e15 {
        return itoa(int64(f))
    }
    return strconv.FormatFloat(f, 'f', -1, 64)
}
```

- **NaN path:** `NaN < 1e15` is `false` (all NaN comparisons return false), so NaN falls through to `strconv.FormatFloat` which returns `"NaN"` -- an invalid JSON literal.
- **Inf path:** `+Inf < 1e15` is `false`, so Inf also falls through to `strconv.FormatFloat` which returns `"+Inf"`.
- **Reproduction:** Any upstream response containing `NaN` or `Inf` in a numeric field that reaches `appendJSON` via `writeJSON` will emit invalid JSON. Downstream clients will fail to parse.

**Fix:** Add NaN/Inf guards before calling `strconv.FormatFloat`:

```go
func ftoa(f float64) string {
    if math.IsNaN(f) || math.IsInf(f, 0) {
        return "null" // or "0" depending on API contract
    }
    // ... rest
}
```

### 1.2. Guarded (Safe): Balance service

**File:** `service/balance/balance.go`, lines 250-257, 272-273, 455-466

All paths in the balance service that touch float values from upstream JSON use explicit `math.IsNaN` / `math.IsInf` guards before assigning or using the values. This is correct and prevents NaN/Inf from entering the response pipeline at this layer.

### 1.3. Guarded (Safe): `PickFiniteNumber` / `EnsureIntTimestamp`

**File:** `transform/shared/utils.go`, lines 136-160

Both functions check `math.IsNaN` / `math.IsInf` and return safe fallbacks (0). This is correct.

### 1.4. Guarded (Safe): `fmtFloat` in runtime_health

**File:** `routing/runtime_health.go`, lines 1256-1260

```go
func fmtFloat(v float64) string {
    if math.IsNaN(v) || math.IsInf(v, 0) {
        return "0"
    }
    // ...
}
```

Correctly guards against NaN/Inf.

### 1.5. INFO: `jsonNumRaw` silently drops NaN/Inf

**File:** `transform/shared/utils.go`, lines 378-381

```go
func jsonNumRaw(f float64) string {
    b, _ := json.Marshal(f)
    return string(b)
}
```

Go's `json.Marshal` returns an error for NaN/Inf -- this error is silently discarded, producing an empty string. This is safe (no invalid JSON emitted) but silently loses data.

---

## 2. Control Character Escaping

JSON strings MUST escape control characters U+0000 through U+001F (RFC 8259, Section 7).

### 2.1. CRITICAL: `jsonEscape` / `jsonSafeString` incomplete escaping

**File:** `handler/proxy/router.go`, lines 89-109
**File:** `handler/proxy/messages.go`, lines 66-86

Both functions escape only: `"`, `\`, `\n`, `\r`, `\t`

Missing: `\b` (0x08), `\f` (0x0C), and all other control characters (0x00-0x07, 0x0B, 0x0E-0x1F).

**Impact:** If any string value flowing through `writeJSON` (proxy handler) or `writeJSONError` contains a control character like NUL, BEL, BS, VT, FF, or SI, the emitted JSON will be syntactically invalid.

**Affected call sites:**
- `writeJSONError` in `handler/proxy/router.go:84` -- error messages are concatenated with `jsonEscape`
- `writeJSON` in `handler/proxy/messages.go:88-96` -- all string values go through `jsonEscapeStr`
- `writeStubResponse` in `handler/proxy/upstream.go:128-133` -- model name embedded via `jsonSafeString`

**Fix:** Add handling for remaining control characters using `\uXXXX` escapes, or replace with `encoding/json` for string serialization.

---

## 3. Custom JSON Serializer -- Type Safety

### 3.1. HIGH: `appendJSON` default case silently drops to `null`

**File:** `handler/proxy/messages.go`, lines 98-156

The `appendJSON` switch handles: `map[string]any`, `[]map[string]any`, `[]any`, `string`, `float64`, `int`, `int64`, `bool`, `nil`, `time.Time`. Any other type (e.g., `int32`, `uint64`, `json.Number`, custom struct) falls into `default` and produces `"null"`.

This silently drops data in responses without logging or error indication.

### 3.2. MEDIUM: Missing type coverage

Types not handled that commonly appear in `map[string]any` from `json.Unmarshal`:
- `json.Number` (appears when using `json.Decoder` with `UseNumber()`)
- Actually `json.Unmarshal` into `map[string]any` always produces `float64` for numbers, so `json.Number` won't appear spontaneously. But manually-constructed maps could contain any Go numeric type.

---

## 4. Integer Precision (> 2^53 in JavaScript)

### 4.1. HIGH: `itoa` in `transform/shared/utils.go` uses float64 intermediate

**File:** `transform/shared/utils.go`, lines 383-386

```go
func itoa(n int64) string {
    return strings.TrimSpace(strings.Replace(
        strings.Replace(jsonNumRaw(float64(n)), "e+00", "", 1), ".0", "", 1))
}
```

Casts `int64` to `float64` before serializing via `json.Marshal`. IEEE 754 float64 cannot precisely represent integers beyond 2^53 (9,007,199,254,740,992). For example, `9223372036854775807` (max int64) becomes `9.223372036854776e+18` in JSON, which JavaScript's `JSON.parse` will round to `9223372036854776000`.

**Impact:** This function is used for ID generation (`CreateStreamTransformContext` line 67: `itoa(now.UnixMilli())`) and in `StringifyUnknownValue` (line 182). Chat completion IDs generated with timestamps risk precision loss if consumed by JavaScript clients.

**Contrast:** The `itoa` in `handler/proxy/messages.go` (line 43-63) is correct -- it does integer-to-decimal conversion without float64 intermediate.

---

## 5. `json.NewEncoder` Trailing Newline

### 5.1. HIGH: Unwanted trailing `\n` in API responses

**File:** `auth/admin.go`, line 283
**File:** `handler/admin/sites.go`, line 798

```go
json.NewEncoder(w).Encode(data)
```

Go's `json.Encoder.Encode` appends a trailing newline after the JSON value. This is valid per ECMA-404 (whitespace is allowed outside JSON values), but:
- Some strict downstream parsers may reject it
- Adds 1 byte to every admin response
- The proxy handler's `writeJSON` does NOT add a trailing newline, creating inconsistency between admin and proxy endpoints

**Fix:** Either use `json.NewEncoder(w).Encode(data)` and then strip the trailing `\n`, or switch to `json.Marshal` + `w.Write`.

---

## 6. Duplicate `writeJSON` Implementations

### 6.1. MEDIUM: Three conflicting implementations

| Location | Implementation | Safe? |
|---|---|---|
| `handler/proxy/messages.go:88` | Custom `appendJSON` | No (NaN, escaping bugs) |
| `handler/admin/sites.go:795` | `json.NewEncoder(w).Encode(data)` | Yes (trailing `\n`) |
| `auth/admin.go:280` | `json.NewEncoder(w).Encode(body)` | Yes (trailing `\n`) |

These are package-level functions, not exported, but their duplication means fixes must be applied in three places. The proxy handler version is the only unsafe one -- admin/auth endpoints are safe.

---

## 7. Hand-Rolled JSON in Persistence Code

### 7.1. MEDIUM: `marshalPayload` / `marshalState` -- no string escaping

**File:** `routing/runtime_health.go`, lines 1151-1254

These functions build JSON by direct byte concatenation. String keys are embedded directly without escaping. If a site ID key or any string value contained `"` or `\`, the output would be invalid JSON.

**Blast radius:** Limited -- this JSON is persisted to disk/DB, not served to API clients. But it could cause deserialization failures on restart.

---

## 8. SSE JSON Passthrough

### 8.1. MEDIUM: Raw byte relay bypasses validation

**File:** `handler/proxy/upstream.go`, lines 167-218

`handleStreamUpstream` reads upstream SSE bytes and writes them directly to the downstream client without JSON validation. If upstream emits malformed JSON, it reaches the client unmodified. The post-hoc `ParseAndAnalyzeSseStream` only runs for logging purposes.

This is architecturally intentional (zero-latency passthrough), but means the gateway cannot protect clients from malformed upstream SSE JSON.

---

## 9. Null vs Omitted Fields

### 9.1. PASS: `omitempty` usage in store/schema structs

**File:** `store/schema.go`

All store struct definitions use `omitempty` on optional pointer fields (e.g., `*string`, `*int64`, `*float64`). When these fields are nil, the JSON key is omitted entirely rather than serialized as `null`. When they have a value, the pointed-to value is serialized.

This behavior correctly distinguishes "field not set" from "field explicitly set to null" for the Go standard library path. Note: the custom `appendJSON` serializer does NOT use these structs -- it operates on `map[string]any`.

### 9.2. PASS: Empty array serialization

**File:** `handler/proxy/messages.go`, lines 114-122, 123-131

Both `[]map[string]any` and `[]any` cases in `appendJSON` correctly serialize as `[]` when empty.

---

## 10. Special Character Handling in `map[string]any`

### 10.1. PASS: Standard library path

Admin handlers use `json.NewEncoder`/`json.Marshal` for response serialization, which handles:
- Unicode escape sequences correctly
- UTF-8 encoding (standard library is UTF-8 native)
- All control characters via `\uXXXX`

### 10.2. FAIL: Custom serializer path (see Issue 2.1)

The proxy handler's `appendJSON` has the incomplete escaping documented above.

---

## 11. UTF-8 BOM

### 11.1. PASS: No BOM generation

Go's `encoding/json` does not emit a BOM. No code in the project prepends a BOM. Standard Go `net/http` response writers do not add BOMs.

---

## 12. Trailing Comma Tolerance

### 12.1. PASS: Input rejection

**File:** `handler/admin/edge_cases_test.go`, line 889

The admin endpoints are tested to reject trailing commas in request bodies (Go's `json.NewDecoder` rejects them per RFC 8259).

---

## 13. Go `json.Encoder.SetEscapeHTML`

### 13.1. INFO: Default behavior (safe)

No code in the audit set calls `enc.SetEscapeHTML(false)`. Go's default behavior escapes `&`, `<`, `>` as `&`, `<`, `>` -- this is safe for HTML embedding and produces valid UTF-8 JSON.

---

## 14. `json.NewDecoder` Without `DisallowUnknownFields`

### 14.1. LOW: Silent acceptance of unknown fields

The project uses `json.NewDecoder(r.Body).Decode(&body)` in request body parsing (e.g., `handler/admin/accounts.go:121`, `service/balance/balance.go:152`). Without `DisallowUnknownFields()`, the decoder silently ignores JSON keys that do not match struct fields. This could mask client errors (typoed field names).

---

## Fix Priority Summary

| Priority | Issue | File | Fix |
|---|---|---|---|
| **P0** | `ftoa` emits NaN/Inf as invalid JSON | `handler/proxy/messages.go:162-168` | Add `math.IsNaN`/`math.IsInf` guard, return `"null"` or `"0"` |
| **P0** | `jsonEscape` missing control chars 0x00-0x1F | `handler/proxy/router.go:89-109`, `messages.go:66-86` | Add full control-char escaping (`\uXXXX`) |
| **P1** | Custom `appendJSON` silently drops unknown types | `handler/proxy/messages.go:152-153` | Log warning in default case, or use `fmt.Sprint` |
| **P1** | `itoa` precision loss via float64 | `transform/shared/utils.go:383-386` | Rewrite as pure integer-to-string, or use `strconv.FormatInt` |
| **P2** | `json.NewEncoder` trailing newline | `auth/admin.go:283`, `handler/admin/sites.go:798` | Switch to `json.Marshal` + `w.Write` |
| **P3** | Unify three `writeJSON` implementations | Multiple | Extract shared `writeJSON` to a common package |
| **P3** | Runtime health custom JSON lacks string escaping | `routing/runtime_health.go:1151-1254` | Replace with `encoding/json` or add escaping |
| **P4** | `json.NewDecoder` missing `DisallowUnknownFields` | Multiple handler files | Add `.DisallowUnknownFields()` to all decoders |
