# Input Validation Depth Audit: metapi-go Admin Handlers

**Date:** 2026-07-05
**Scope:** `handler/admin/*.go` (28 files, 27 handler route registrars) + `handler/admin/payloads/*.go` (3 files)
**Methodology:** Full source read of all handler and payload files, tested against 7 audit dimensions: SQL injection, XSS, Unicode handling, max string lengths, negative numbers, null vs empty string distinction, JSON type coercion.

---

## Executive Summary

The codebase exhibits **defensive but incomplete** input validation. Parameterized SQL queries (`?` placeholders) are used consistently throughout, eliminating classical SQL injection. However, there are significant gaps: **no request body size limit is enforced** despite a config field existing for it, **JSON type coercion silently fails** (string `"123"` for int becomes 0 instead of error), **no upper bounds on string lengths** allow unbounded DB writes, and a **TOCTOU race condition** exists in the settings UPSERT path. Several payload structs use `any` typed fields (`apiToken`, `extraConfig`, `remainQuota`, `expiredTime`) that bypass Go's type system entirely.

---

## 1. SQL Injection

### 1.1 Parameterized Queries (Safe)

All user-supplied values are passed via `?` placeholders to `database/sql`-compatible drivers. No string concatenation of user input into SQL.

**Files verified:**
- `accounts.go` lines 133, 236, 291-296, 328, 341, 372-374, 378-381, 388, 494, 500, 616, 673, 682, 685-687, 786, 790, 862-866
- `account_tokens.go` lines 125, 162-166, 182, 246, 334, 630
- `sites.go` lines 148, 232, 370, 449, 458-468, 524, 564-568, 595-608
- `downstream_keys.go` lines 152, 193-198, 423-425, 446-461, 489, 507, 598-620, 1125-1126, 1164, 1199-1203, 1215-1217
- `search.go` lines 71-117
- `stats.go` lines 54-55, 73-76, 139-151, 302-310
- `events.go` lines 50-54, 85, 103, 110, 117
- `settings.go` lines 144, 150, 158, 169, 187, 194, 199, 203, 219, 225, 231, 237, 248, 256, 268, 272, 276, 284, 294, 308, 317, 327, 333, 340, 348, 354, 358, 361, 366, 378, 383, 387, 391, 396, 406, 426, 443, 460, 482, 506, 513, 630-631
- `token_routes.go` lines 62, 86-87, 124-130, 231-233, 246, 281-306, 338-341, 369, 476-478, 485-486, 523-524, 531-533, 560, 577, 609-615, 632
- `auth_settings.go` lines 83, 85-88, 94

**Verdict: PASS.** All user-supplied data flows through parameterized queries.

### 1.2 Dynamic Table/Column Names (Safe via Whitelist)

Three files construct table names dynamically in SQL strings, but all gate on a known whitelist:

- `settings_backup.go` line 179-185: `isKnownTable()` checks against `allTables` constant before `fmt.Sprintf("SELECT * FROM %s", table)`
- `settings_maintenance.go` line 104, 113, 135-136: `factoryReset` uses table names from `allTables` constant (never user-provided)
- `settings_backup.go` line 269: `importTableRows` uses whitelisted table name in `INSERT`

**Verdict: PASS.** Table name interpolation is protected by whitelist.

### 1.3 LIKE Wildcard Injection (Not Exploitable)

`search.go` line 68 and `stats.go` line 106 construct LIKE patterns with user input (`"%"+q+"%"`), but all values are parameterized. A user could inject `%` or `_` as LIKE wildcards to broaden search results, but this is not a security vulnerability -- it is a search feature.

**Verdict: INFORMATIONAL.** Search accepts `%` and `_` as literal LIKE wildcards. For an admin-facing search API this is acceptable.

---

## 2. XSS (Cross-Site Scripting)

### 2.1 JSON API Response

All admin endpoints return `application/json`. JSON responses are not executable in a browser context when fetched via XHR/fetch (the browser does not parse JSON as HTML). There is **no reflected HTML** in any handler response.

The sole HTML response is `oauth_routes.go` line 151-161 (OAuth callback), which returns a hardcoded HTML page with no user input interpolation.

**Verdict: PASS.** No XSS vector in admin handlers. JSON Content-Type prevents script execution.

### 2.2 Stored XSS Concerns

String fields (site name, account username, token names, descriptions) are stored and later returned in JSON responses. A front-end rendering these values in HTML context must apply its own escaping. This is the front-end's responsibility for a JSON API.

**Verdict: ACCEPTABLE (front-end responsibility).**

---

## 3. Unicode Handling

### 3.1 Unicode Preservation

The edge-case test `TestEdge_UnicodeAndEmojiInFields` (`edge_cases_test.go` line 772-787) confirms that emoji and Unicode characters survive round-trip through JSON decode -> SQLite -> JSON encode.

### 3.2 Unicode Normalization

No Unicode normalization (NFKC, NFC, NFD, etc.) is applied to any string field. This means:
- Visually identical strings with different Unicode codepoints (e.g., composed vs decomposed characters) may be treated as different values, causing duplicate site/account creation to succeed
- Case-insensitive comparisons (`strings.ToLower`) work for ASCII but not for Unicode case folding (e.g., Turkish I)

**Affected locations:**
- `accounts.go` line 148, 319, 322, 429, 559: `strings.TrimSpace` (handles Unicode whitespace)
- `sites.go` line 127: `strings.ToLower` on platform name (ASCII-only for known platforms, so acceptable)
- `downstream_keys.go` line 847: `strings.ToLower` on tags (case-insensitive dedup, problematic for non-ASCII tags)

**Verdict: LOW RISK.** Platform names are ASCII. Tag deduplication would benefit from `strings.EqualFold` or a proper Unicode case-folding library if non-ASCII tags are expected.

---

## 4. Maximum String Length Enforcement

### 4.1 Fields With __No__ Upper Bound

These user-supplied string fields are stored directly with no length validation:

| File | Field | Handler | Max Length Check |
|---|---|---|---|
| `accounts.go` | `Username` | createAccount, updateAccount | **NONE** |
| `accounts.go` | `AccessToken` | createAccount, loginAccount | **NONE** |
| `accounts.go` | `Password` | loginAccount | **NONE** |
| `sites.go` | `Name` | createSite, updateSite | **NONE** |
| `sites.go` | `URL` | createSite, updateSite | **NONE** |
| `downstream_keys.go` | `Name` | createKey, updateKey | **NONE** |
| `downstream_keys.go` | `Description` | createKey, updateKey | **NONE** |
| `downstream_keys.go` | `SupportedModels` entries | createKey, updateKey | **NONE** |
| `auth_settings.go` | `NewToken` | changeToken | **NONE** (only min length 6) |
| `token_routes.go` | `DisplayName`, `DisplayIcon` | createRoute, updateRoute | **NONE** |

The edge-case test `TestEdge_VeryLongStringInField` (`edge_cases_test.go` line 831-847) confirms a 10KB site name is accepted without error.

### 4.2 Fields With Length Enforcement

| File | Field | Limit | Location |
|---|---|---|---|
| `downstream_keys.go` | `search` query param | 80 chars | line 42-43 |
| `downstream_keys.go` | `GroupName` | 64 chars | line 830-834 |
| `downstream_keys.go` | `Tags` (per tag) | 32 chars | line 844-847 |
| `downstream_keys.go` | `Tags` (total count) | 20 max | line 853 |
| `downstream_keys.go` | `AllowedRouteIds` | 500 max | line 889-893 |
| `downstream_keys.go` | `ExcludedSiteIds` | 500 max | line 962-968 |
| `downstream_keys.go` | `ExcludedCredentialRefs` | 1000 max | line 1016 |

**Verdict: HIGH SEVERITY.** No length limit on core entity fields (site name, account username, access token, password). A malicious or buggy client could write arbitrarily large strings to the database, causing storage exhaustion or query performance degradation.

---

## 5. Negative Numbers / Invalid Numeric Values

### 5.1 Validated

| File | Field | Validation | Location |
|---|---|---|---|
| `accounts.go` | `SiteID` | `<= 0` rejected | line 126, 314 |
| `accounts.go` | `AccountID` | `<= 0` rejected | line 458, 625, 767, 822 |
| `account_tokens.go` | `AccountID` | `<= 0` rejected | line 73 |
| `sites.go` | `ID` | `<= 0` rejected | line 405, 638, 670 |
| `sites.go` | `SortOrder` | `NormalizeSortOrder` returns nil for negative | line 99-103, 305-310 |
| `downstream_keys.go` | `MaxCost` | `< 0`, NaN, Inf -> null | line 1066-1074 |
| `settings.go` | `checkinIntervalHours` | `< 1` or `> 24` rejected | line 175-176 |
| `settings.go` | `logCleanupRetentionDays` | `< 1` rejected | line 208 |
| `settings.go` | `proxySessionChannelConcurrencyLimit` | `< 0` rejected | line 244 |
| `settings.go` | `proxyDebugRetentionHours` | `< 1` rejected | line 279-280 |
| `settings.go` | `proxyDebugMaxBodyBytes` | `< 1024` rejected | line 289 |
| `settings.go` | `routingFallbackUnitCost` | `<= 0` rejected | line 300 |
| `settings.go` | `tokenRouterFailureCooldownMaxSec` | `<= 0` rejected | line 321 |
| `accounts.go` | `SortOrder` (account) | `NormalizeSortOrder` returns nil for negative | line 585-589 |

### 5.2 NOT Validated / Silently Clamped

| File | Field | Behavior | Location |
|---|---|---|---|
| `sites.go` | `PostRefreshProbeLatencyThresholdMs` | Negative silently clamped to 0 | line 334-338 |
| `downstream_keys.go` | `MaxRequests` | `< 0` silently converted to null | line 1076-1084 |
| `settings.go` | `smtpPort` | Not validated (any int accepted) | line 376-379 |
| `settings.go` | `proxyFirstByteTimeoutSec` | `< 0` rejected, but 0 is accepted (which may disable timeout) | line 311-317 |
| `token_routes.go` | `Priority`, `Weight` on channels | Any int64 accepted (no non-negative check) | line 465-469 |

### 5.3 Silently Ignored

| File | Field | Behavior |
|---|---|---|
| `settings.go` | `toFloat64` on non-numeric | Returns 0 (silent failure) |
| `settings.go` | `toBool` on unrecognized input | Returns false (silent failure) |

**Verdict: MEDIUM SEVERITY.** Core entity IDs are well-validated. Settings numeric fields have good coverage. But `ProxyFirstByteTimeoutSec=0` and `SmtpPort` lack validation, and the silent clamping of `PostRefreshProbeLatencyThresholdMs` hides configuration errors.

---

## 6. Null vs Empty String Distinction

### 6.1 Well-Handled Cases

Payload structs use `*string` (pointer to string) for optional fields, correctly representing the tri-state: absent (nil), empty string (pointer to ""), or value (pointer to "value").

**Examples:**
- `payloads/accounts.go`: `Username *string`, `AccessToken *string`, `APIToken *string`
- `payloads/sites.go`: `Platform *string`, `ProxyURL *string`, `Status *string`
- `payloads/account_tokens.go`: `Name *string`, `Token *string`, `Group *string`

The `downstream_keys.go` `updateKey` handler (lines 262-322) goes further: it parses the request body into a raw `map[string]any` to detect field presence before applying values, correctly distinguishing "not provided" (keep existing) from "set to null" (clear) from "set to new value".

### 6.2 Problematic Cases

| File | Field | Problem |
|---|---|---|
| `payloads/accounts.go` | `SiteID` (int, not *int) | JSON decode sets missing field to `0`. Cannot distinguish "not provided" from "explicitly 0" |
| `payloads/accounts.go` | `AccountLoginPayload.Username`, `.Password` (string, not *string) | Cannot distinguish missing field from empty string; both produce `""` |
| `payloads/sites.go` | `SiteCreatePayload.Name` (string, not *string) | JSON decode sets missing to `""`; handler then rejects "" as invalid, which is correct for a required field |
| `token_routes.go` | `createRoute` body struct: `ModelPattern` (string) | Same issue: required field uses non-pointer, but validation catches it |

### 6.3 Database NULL Handling

`edge_cases_test.go` line 459-506 (`TestEdge_NULLFieldsInJSONResponse`) confirms that NULL database columns serialize correctly as JSON `null`. The `nullIfEmpty` helper in `account_tokens.go` line 631-636 converts empty strings to nil for storage.

### 6.4 Endpoint-Specific Analysis

- **createAccount**: `Username` is `*string`, `omitempty`. An explicit `"username": ""` in JSON results in a Go `*string` pointing to `""`, which is passed to SQL as empty string (not NULL). Acceptable.
- **loginAccount**: `Username` and `Password` are `string` (non-pointer). Missing fields become `""`. The handler rejects empty strings on lines 318-325. Acceptable.
- **createSite**: `Name` and `URL` are `string`. Missing fields become `""`. The handler rejects empty strings on lines 65-72. Acceptable.

**Verdict: ACCEPTABLE.** Required fields correctly reject empty/missing values via `strings.TrimSpace` checks. Optional fields use `*string` pointers.

---

## 7. JSON Type Coercion

### 7.1 Go JSON Decoder Behavior

The standard `encoding/json` decoder is **strict** about types:
- `{"siteId": "123"}` into `int` field -> decode error (caught by `json.NewDecoder().Decode()`)
- `{"siteId": 123}` into `int` field -> correct
- `{"enabled": "true"}` into `bool` field -> decode error
- `{"enabled": true}` into `bool` field -> correct

This strictness provides a good baseline defense.

### 7.2 Where Coercion Fails

#### `settings.go` `updateRuntime` (CRITICAL)

The handler decodes into `map[string]any`, bypassing struct type checking. It then uses `toFloat64()` (line 584-600) which handles `float64`, `json.Number`, `int`, `int64` but **silently returns 0 for strings**:

```go
func toFloat64(v any) float64 {
    switch val := v.(type) {
    case float64:  return val
    case float32:  return float64(val)
    case int:      return float64(val)
    case int64:    return float64(val)
    case json.Number: n, _ := val.Float64(); return n
    default:       return 0  // <-- String "123" lands here, returns 0
    }
}
```

**Impact:** If a client sends `{"checkinIntervalHours": "6"}` (string instead of number), the handler silently sets the interval to 0, which then fails the `hours < 1` check and returns an error message "签到间隔必须是 1 到 24 的整数小时" -- **misleading because the real problem is that "6" is a string, not that it's out of range.**

Similarly, `toBool()` (line 602-616) is overly lenient, accepting strings `"1"`, `"true"`, `"yes"`, `"on"` in addition to Go's native `bool`. This is lenient but functional.

#### `token_routes.go` `updateRoute` and `updateChannel`

These also decode into `map[string]any` and use `toBool()` and `toFloat64()`, inheriting the same silent-string-to-zero problem.

#### `payloads/accounts.go` `AccountUpdatePayload`

`APIToken` and `ExtraConfig` are typed as `any`, bypassing all struct-level type checking.

#### `payloads/account_tokens.go` `AccountTokenCreatePayload`

`RemainQuota` and `ExpiredTime` are typed as `any`, bypassing all struct-level type checking.

### 7.3 Endpoint-Specific Analysis

**Settings updateRuntime**: Uses `map[string]any` + manual type switching. Beneficial for partial updates (only present fields are applied), but the `default: return 0` pattern in `toFloat64` is dangerous.

**Token route updateRoute**: Same pattern as settings. Acceptable within admin context but fragile.

**Downstream key updateKey**: Double-parses the body (raw map + typed struct) to detect field presence. This is the most robust approach in the codebase.

**All other endpoints**: Use typed structs with `json.NewDecoder().Decode()`. Standard Go strict typing applies.

**Verdict: MEDIUM SEVERITY.** The `toFloat64` silent-zero problem can cause subtle misconfiguration. The `updateRuntime` handler should at minimum log or warn when a non-numeric value is received for a numeric field.

---

## 8. Additional Findings

### 8.1 CRITICAL: No Request Body Size Limit Enforced

**File:** `config/config.go` (config definition) + absence in middleware chain

The config struct has `RequestBodyLimit` (default 20MB per `edge_cases_test.go` line 47), but **no middleware reads or enforces this value**. The test `TestEdge_MaxBodyLimitNotEnforced` (`edge_cases_test.go` line 165-187) explicitly documents: "RequestBodyLimit is configured as %d bytes but NOT enforced by middleware."

**Impact:** An attacker can send arbitrarily large JSON bodies, consuming server memory until OOM.

### 8.2 HIGH: TOCTOU Race in `upsertSettingDB`

**File:** `settings.go` lines 625-633

```go
func upsertSettingDB(db *sqlx.DB, key string, value any) {
    var count int
    db.Get(&count, "SELECT COUNT(*) FROM settings WHERE key = ?", key)
    if count > 0 {
        db.Exec("UPDATE settings SET value = ? WHERE key = ?", string(jsonValue), key)
    } else {
        db.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", key, string(jsonValue))
    }
}
```

**Race:** Two concurrent requests for the same key can both see `count == 0`, then both attempt INSERT. The second INSERT fails with a UNIQUE constraint violation. The test `TestEdge_UpsertSettingDB_RaceCondition` (`edge_cases_test.go` line 605-654) reproduces this.

**Fix:** Use `INSERT ... ON CONFLICT(key) DO UPDATE SET value = ...`. The codebase already has `SettingsStore.Set` which does this correctly.

### 8.3 HIGH: Unsanitized Username in Token Construction

**File:** `accounts.go` line 336

```go
loginAccessToken := "session_" + strconv.Itoa(int(body.SiteID)) + "_" + body.Username
```

While not a direct injection vector (the result is stored, not parsed), embedding unsanitized user input into a structured token is bad hygiene. A username containing `_` or control characters could confuse downstream parsers.

### 8.4 MEDIUM: No Duplicate AccessToken Prevention

**File:** `accounts.go` `createAccount`

The accounts table has no UNIQUE constraint on `access_token`. The test `TestEdge_DuplicateAccountCreation` (`edge_cases_test.go` line 430-453) confirms that duplicate accounts with the same `accessToken` are accepted without error. This can cause double-counting in routing and balance tracking.

### 8.5 MEDIUM: Stub Endpoints Accept All Input Silently

**Files:** `oauth_routes.go`, `test.go`, `checkin_routes.go`, `tasks.go`

Multiple endpoints are stubs that return hardcoded success responses without validating or even reading the request body:

- `oauth_routes.go`: 11 endpoints accept any body and return `{"success": true}`
- `test.go`: All 9 endpoints are stubs
- `checkin_routes.go`: `triggerAll`, `triggerOne`, `updateSchedule` are stubs
- Various other stubs in `stats.go`, `sites.go`, `token_routes.go`

This is acceptable during development but creates a trap: if these stubs reach production, users may believe operations succeeded when nothing happened.

### 8.6 LOW: Silent Error Suppression

Several handlers silently ignore errors:

- `sites.go` line 641: `json.NewDecoder(r.Body).Decode(&body)` -- return value ignored in `probeNow`
- `account_tokens.go` line 521: `json.NewDecoder(r.Body).Decode(&body)` -- return value ignored in `syncAll`
- `sites.go` line 111: `globalAccountsCache.set(respBytes)` -- Marshal error ignored on line 110
- `settings_backup.go` line 315: `json.NewDecoder(r.Body).Decode(&body)` -- return value ignored

### 8.7 LOW: Monitor Session Cookie Exposes Auth Token

**File:** `monitor.go` lines 60-65

The `createSession` endpoint sets the full `AuthToken` as an HttpOnly cookie. While HttpOnly prevents JavaScript access, the token is still transmitted on every request to origins matching the cookie path, increasing exposure surface.

### 8.8 INFORMATIONAL: Response Includes Raw AccessToken

**File:** `accounts.go` lines 239-252, 389-403, 501-515

Account creation, login, and rebind responses include the raw `accessToken` value. This is intentional (the admin UI needs it) but means the token traverses the network in every response.

---

## 9. Summary by Endpoint

| Endpoint | SQLi | XSS | Unicode | Length | Negative | Null/Empty | Coercion | Overall |
|---|---|---|---|---|---|---|---|---|
| POST /api/accounts | PASS | PASS | PASS | **FAIL** | PASS | PASS | PASS | NEEDS LENGTH LIMITS |
| PUT /api/accounts/:id | PASS | PASS | PASS | **FAIL** | PASS | PASS | **WARN** (any) | NEEDS LENGTH LIMITS + any fix |
| POST /api/accounts/login | PASS | PASS | PASS | **FAIL** | PASS | PASS | PASS | NEEDS LENGTH LIMITS |
| POST /api/accounts/verify-token | PASS | PASS | PASS | **FAIL** | PASS | PASS | PASS | NEEDS LENGTH LIMITS |
| POST /api/sites | PASS | PASS | PASS | **FAIL** | PASS | PASS | PASS | NEEDS LENGTH LIMITS |
| PUT /api/sites/:id | PASS | PASS | PASS | **FAIL** | PASS | PASS | PASS | NEEDS LENGTH LIMITS |
| PUT /api/settings/runtime | PASS | PASS | PASS | PASS | PASS | PASS | **FAIL** (toFloat64) | FIX toFloat64 |
| POST /api/downstream-keys | PASS | PASS | PASS | **PARTIAL** | PASS | PASS | PASS | OK |
| PUT /api/downstream-keys/:id | PASS | PASS | PASS | **PARTIAL** | PASS | **PASS** | PASS | OK |
| POST /api/routes | PASS | PASS | PASS | **FAIL** | PASS | PASS | PASS | NEEDS LENGTH LIMITS |
| PUT /api/routes/:id | PASS | PASS | PASS | **FAIL** | PASS | PASS | **WARN** (toFloat64) | OK for admin |
| POST /api/search | PASS | PASS | PASS | PASS | PASS | PASS | PASS | OK |
| POST /api/settings/maintenance/factory-reset | PASS | PASS | PASS | PASS | PASS | PASS | PASS | OK |
| *All batch endpoints* | PASS | PASS | PASS | PASS | PASS | PASS | PASS | OK |
| *All GET endpoints* | PASS | PASS | PASS | PASS | PASS | PASS | PASS | OK |

---

## 10. Recommendations (Priority Order)

### P0 (Critical)
1. **Wire `RequestBodyLimit` to middleware.** Add `http.MaxBytesReader` or a `chi` middleware that enforces `cfg.RequestBodyLimit` on all POST/PUT routes.

### P1 (High)
2. **Fix `upsertSettingDB` TOCTOU race.** Replace SELECT+INSERT/UPDATE with `INSERT ... ON CONFLICT(key) DO UPDATE`.
3. **Fix `toFloat64` to not silently return 0.** Return an error or at minimum log a warning when a string is received where a number was expected. Consider using `json.Number` throughout or adding strict type assertions.
4. **Add max length validation to all string fields.** Suggested limits:
   - Site name: 255 chars
   - Site URL: 2048 chars
   - Account username: 255 chars
   - Account access_token: 4096 chars
   - Login password: 1024 chars
   - Token route displayName: 255 chars
   - Downstream key name: 255 chars
   - Downstream key description: 1024 chars
5. **Add UNIQUE constraint on `accounts(access_token)` or `accounts(site_id, access_token)`.** Prevents duplicate credential import.

### P2 (Medium)
6. **Sanitize username before embedding in token string** (`accounts.go` line 336).
7. **Add negative value checks for `Priority`, `Weight`, `SmtpPort`.**
8. **Add max count limits to array fields** (`AccessTokens`, batch operation `IDs`, `Tags`, `SupportedModels`).
9. **Standardize pagination** across all list endpoints. Currently `sites` and `accounts` return unlimited results.

### P3 (Low)
10. **Check and log `Decode` errors** in `probeNow`, `syncAll`, and `saveWebdavConfig`.
11. **Consider scoping the monitor session cookie** to `/monitor-proxy` path instead of `/`.
12. **Add Unicode normalization** (or document that raw bytes are preserved) for site name and username deduplication.

---

## Appendix A: Files Audited

### Handler files (27):
1. `handler/admin/accounts.go` (898 lines)
2. `handler/admin/account_tokens.go` (679 lines)
3. `handler/admin/sites.go` (800 lines)
4. `handler/admin/settings.go` (641 lines)
5. `handler/admin/settings_backup.go` (338 lines)
6. `handler/admin/settings_database.go` (159 lines)
7. `handler/admin/settings_maintenance.go` (155 lines)
8. `handler/admin/settings_notify.go` (22 lines)
9. `handler/admin/auth_settings.go` (111 lines)
10. `handler/admin/checkin_routes.go` (113 lines)
11. `handler/admin/downstream_keys.go` (1234 lines)
12. `handler/admin/events.go` (120 lines)
13. `handler/admin/monitor.go` (104 lines)
14. `handler/admin/oauth_routes.go` (163 lines)
15. `handler/admin/search.go` (203 lines)
16. `handler/admin/site_announcements.go` (222 lines)
17. `handler/admin/stats.go` (402 lines)
18. `handler/admin/tasks.go` (52 lines)
19. `handler/admin/test.go` (113 lines)
20. `handler/admin/token_routes.go` (708 lines)
21. `handler/admin/update_center.go` (160 lines)
22. `handler/admin/doc.go`
23. `handler/admin/edge_cases_test.go` (907 lines)

### Payload files (3):
24. `handler/admin/payloads/accounts.go` (73 lines)
25. `handler/admin/payloads/account_tokens.go` (40 lines)
26. `handler/admin/payloads/sites.go` (69 lines)

### Test files (also reviewed for patterns):
27. `handler/admin/accounts_test.go`
28. `handler/admin/account_tokens_test.go`
29. `handler/admin/sites_test.go`

## Appendix B: Methodology Notes

Each handler function was manually inspected for:
- How request body is decoded (`json.Decoder`, raw `map[string]any`, or typed struct)
- How URL path parameters are parsed and validated
- How query string parameters are parsed and validated
- How values are passed to SQL queries (parameterized vs string concatenation)
- Whether string fields have length bounds before storage
- Whether numeric fields reject negative/invalid values
- How `null`, empty string, and absent fields are distinguished
- Whether Go type assertions are error-checked or use `default:` catch-alls
- Whether stubs silently accept input without processing

The `edge_cases_test.go` file was cross-referenced to confirm documented gaps against actual code behavior.
