# P2 Cross-Reference Review: Go Spec vs TypeScript Source

**Reviewed**: 2026-07-04 | **Spec**: `docs/specs/p2-auth.md` | **TS sources**: `auth.ts`, `downstreamApiKeyService.ts`, `downstreamPolicyTypes.ts`, `desktop.ts`

---

## Accuracy Issues

### A1. AdminAuth failure status codes are wrong in spec (HIGH)

**Spec claims**: "任一失败 → 401 JSON"

**TS reality** (auth.ts lines 112, 118, 123):
| Failure condition | TS status code | Spec says |
|---|---|---|
| IP not in allowlist | **403** | 401 |
| Missing Authorization header | **401** | 401 |
| Invalid token (wrong authToken) | **403** | 401 |

Only the missing-header case is actually 401. IP rejection and bad-token are both 403. The spec collapses three distinct error modes into one wrong status code.

**Fix**: Update the flow to read:
```
2. IP 不在白名单 → 403
3. 缺少 Authorization header → 401
4. token 不匹配 → 403
```

---

### A2. Managed key failure status codes are all wrong (HIGH)

**Spec claims**: managed key `expires_at` 已过期 → 401, `max_cost` 超限 → 401, `enabled=false` → 401, unknown token → 401.

**TS reality** (downstreamApiKeyService.ts lines 430-493):
| Failure reason | TS status code | Spec says |
|---|---|---|
| `disabled` | **403** | 401 |
| `expired` | **403** | 401 |
| `over_cost` | **403** | 401 |
| `over_requests` | **403** | 401 |
| `invalid` (unknown token) | **403** | 401 |
| `missing` (empty token) | **401** | 401 |

Every managed-key rejection except empty/missing token returns 403, not 401. The 401-vs-403 distinction is semantically important: 401 = "you didn't authenticate", 403 = "you authenticated but are forbidden".

**Fix**: Change all managed-key failure status codes from 401 to 403. Only empty token stays 401.

---

### A3. Token extraction is NOT a simple priority fallthrough chain (MEDIUM)

**Spec claims**: "从以下位置提取 token (按优先级): Authorization → x-api-key → x-goog-api-key → ?key=", implying a fallthrough chain where each source is tried in order if the previous yields empty.

**TS reality** (auth.ts lines 129-147):
```typescript
const auth = typeof request.headers.authorization === 'string'
  ? request.headers.authorization : '';
const token = auth
  ? auth.replace(/^Bearer\s+/i, '').trim()
  : (apiKeyHeader.trim() || googApiKeyHeader.trim() || queryKey);
```

The actual logic: **Authorization header has absolute precedence**. If `Authorization` is a non-empty string, the token is extracted from it exclusively -- even if the Bearer value is empty/whitespace, resulting in an empty token string. It does NOT fall through to x-api-key. The fallthrough only happens when `Authorization` is undefined, an array, or an empty string.

**Example of divergence**: Request with `Authorization: Bearer ` (empty bearer) + `x-api-key: valid-key`:
- Spec interpretation: would try x-api-key and succeed
- TS behavior: sees Authorization header present, extracts empty string after Bearer, returns 401 "missing"

**Fix**: Rewrite step 1 to clarify:
```
1. 如果 Authorization header 存在且为 string 类型:
   - 提取 Bearer token（大小写不敏感正则 /^Bearer\s+/i）
   - 即使 Bearer 后为空也不回退到其他来源
   否则，按以下优先级尝试:
   - x-api-key header
   - x-goog-api-key header  
   - ?key= query 参数
```

---

### A4. Cost counter is NOT incremented in the auth flow (HIGH)

**Spec claims**: "消费计数器 (used_cost++, used_requests++)" happens inside step 2 (AuthorizeDownstreamToken).

**TS reality**: Only `usedRequests` is incremented during the proxy auth request lifecycle. The flow is:
1. `authorizeDownstreamToken(token)` -- checks quotas, does NOT mutate anything
2. `proxyAuthMiddleware` calls `consumeManagedKeyRequest(key.id)` -- increments only `usedRequests` (line 161)
3. `recordManagedKeyCostUsage(keyId, estimatedCost)` -- increments `usedCost`, but is a **separate function called later** (after the LLM response, when cost is known), not during auth

The spec conflates two separate consumption points. `usedCost` is incremented post-response, not pre-request.

**Fix**: Separate into:
```
a. 查询 key + 校验 enabled/expires_at/max_cost/used_cost/max_requests/used_requests
b. 通过后 → consumeManagedKeyRequest: used_requests++ (原子 SQL increment)
c. 请求完成后 → recordManagedKeyCostUsage: used_cost += estimatedCost (单独调用)
```

---

## Missing Details

### M1. `denyAllWhenEmpty` field completely absent from DownstreamRoutingPolicy (CRITICAL)

**Spec struct**:
```go
type DownstreamRoutingPolicy struct {
    AllowedRouteIDs       []int64
    SiteWeightMultipliers map[int64]float64
    ExcludedSiteIDs       []int64
    ExcludedCredentialRefs []CredentialRef
    SupportedModels       []string
}
```

**TS interface** (downstreamPolicyTypes.ts lines 18-25):
```typescript
export interface DownstreamRoutingPolicy {
  supportedModels: string[];
  allowedRouteIds: number[];
  siteWeightMultipliers: Record<number, number>;
  excludedSiteIds: number[];
  excludedCredentialRefs: DownstreamExcludedCredentialRef[];
  denyAllWhenEmpty?: boolean;  // <-- MISSING FROM SPEC
}
```

This is a critical omission because it controls the fundamental behavior when both `supportedModels` and `allowedRouteIds` are empty:
- `denyAllWhenEmpty === true` (set by `toPolicyFromView` for all managed keys) → **deny all models**
- `denyAllWhenEmpty === undefined/false` (global token policy) → **allow all models**

The decision logic in `isModelAllowedByPolicyOrAllowedRoutes` (line 333):
```typescript
if (!hasPatternRules && !hasRouteRules) return policy.denyAllWhenEmpty === true ? false : true;
```

Without this field, the Go implementation cannot distinguish between "empty policy from a managed key" (deny all) and "empty policy from global token" (allow all).

**Fix**: Add `DenyAllWhenEmpty bool` to the struct. Managed key policies must default this to `true`. Global proxy token policy must default to `false`.

---

### M2. CredentialRef discriminated union not specified (MEDIUM)

**Spec**: just says `[]CredentialRef` with no structure.

**TS types** (downstreamPolicyTypes.ts lines 1-16):
```typescript
// Variant 1: excludes a specific account_token credential
type DownstreamAccountTokenCredentialRef = {
  kind: 'account_token';
  siteId: number;
  accountId: number;
  tokenId: number;
};

// Variant 2: excludes an account's default API key
type DownstreamDefaultApiKeyCredentialRef = {
  kind: 'default_api_key';
  siteId: number;
  accountId: number;
};

type DownstreamExcludedCredentialRef =
  | DownstreamAccountTokenCredentialRef
  | DownstreamDefaultApiKeyCredentialRef;
```

The two variants have different fields. An implementation that doesn't know about the `kind` discriminant will either miss `tokenId` (for account_token) or include an invalid `tokenId` (for default_api_key).

**Fix**: Define:
```go
type CredentialRefKind string
const (
    CredentialRefAccountToken  CredentialRefKind = "account_token"
    CredentialRefDefaultApiKey CredentialRefKind = "default_api_key"
)

type ExcludedCredentialRef struct {
    Kind      CredentialRefKind `json:"kind"`
    SiteID    int64             `json:"site_id"`
    AccountID int64             `json:"account_id"`
    TokenID   *int64            `json:"token_id,omitempty"` // only for account_token
}
```

---

### M3. IPv6 normalization logic not documented (MEDIUM)

**TS** (auth.ts lines 26-31):
```typescript
function normalizeIp(rawIp: string | null | undefined): string {
  const ip = (rawIp || '').trim();
  if (!ip) return '';
  if (ip.startsWith('::ffff:')) return ip.slice('::ffff:'.length).trim(); // v4-mapped → v4
  if (ip === '::1') return '127.0.0.1';  // IPv6 loopback → IPv4 loopback
  return ip;
}
```

This means IPv4-mapped IPv6 addresses (`::ffff:10.0.0.1`) and IPv6 loopback (`::1`) are converted to their IPv4 equivalents before CIDR matching. Without this normalization, a CIDR allowlist entry like `10.0.0.0/8` would not match a client connecting via IPv6-mapped `::ffff:10.0.0.5`.

Additionally, the `isIpAllowed` function converts the client IP to an IPv4 integer via `parseIpv4Value`. Pure IPv6 addresses (not ::ffff:-mapped) will yield `null` from `parseIpv4Value`, which means they are **never allowed** through the IP allowlist -- even if the allowlist is empty (empty allowlist skips IP check entirely via early return, so this only matters for non-empty allowlists).

**Fix**: Document the IPv6 normalization steps and the pure-IPv6 rejection behavior.

---

### M4. Bearer extraction is case-insensitive (LOW)

**TS** (auth.ts line 146): `auth.replace(/^Bearer\s+/i, '').trim()`

The `/i` flag means `bearer`, `BEARER`, `BeArEr` are all accepted. The spec says `Bearer <authToken>` which could be read as case-sensitive.

**Fix**: Note "case-insensitive Bearer prefix matching" in the spec.

---

### M5. `ProxyResourceOwner` concept absent from spec (LOW)

**TS** (auth.ts lines 15-18, 177-192):
```typescript
export interface ProxyResourceOwner {
  ownerType: 'managed_key' | 'global_proxy_token';
  ownerId: string;
}
```
Used by downstream code to attribute usage. The spec's `ProxyAuthContext` only has `Source` and does not cover the `ProxyResourceOwner` derivation logic. Managed keys use `String(keyId)` or token as fallback; global uses `'global'`.

**Fix**: Add `ProxyResourceOwner` to the spec or note as a future concern.

---

### M6. Authorization header type guard (LOW)

**TS** (auth.ts lines 129-131): Explicitly checks `typeof request.headers.authorization === 'string'` before using it. Fastify (and Go's `http.Request`) can receive headers as multi-valued. The spec assumes a simple string.

**Fix**: Note that the Go implementation must handle the case where `Authorization` header appears multiple times (take the first value).

---

## Edge Cases Not Covered

### E1. Authorization header present with empty Bearer token (MEDIUM)

**Scenario**: `Authorization: Bearer ` (whitespace after Bearer) + `x-api-key: sk-valid`

**TS behavior**: Authorization header is a non-empty string (`"Bearer "`), so `auth` is truthy. Token becomes `"Bearer ".replace(/^Bearer\s+/i, '').trim()` = `""`. Returns 401 "Missing". Does NOT fall through to x-api-key.

**Spec implication**: The spec's "按优先级" description would lead an implementer to try x-api-key next. This must be explicitly clarified.

---

### E2. X-Forwarded-For as string array (LOW)

**TS** (auth.ts lines 82-92): `extractClientIp` handles both `string` and `string[]` for `xForwardedFor`:
- Array: iterates to find first non-empty element, then splits on comma
- String: splits on comma directly
- Falls back to `remoteIp`

**Spec** only mentions "X-Forwarded-For 含多个 IP (proxy chain) → 取第一个" which covers the comma-splitting case but not the multi-header case.

---

### E3. Pure IPv6 client with non-empty allowlist is always blocked (MEDIUM)

**TS**: `parseIpv4Value` returns `null` for IPv6 addresses that are not IPv4-mapped. In `isIpAllowed`, when `clientIpv4Value === null`, CIDR entries return false and exact-match entries are checked by string equality. So a pure IPv6 address can only pass if it exactly matches an allowlist entry string (e.g., `2001:db8::1`). This means IPv6-capable deployments need IPv6 entries in the allowlist.

**Spec**: The Edge Cases section mentions CIDR `0.0.0.0/0` (IPv4) but says nothing about IPv6 allowlist entries.

**Fix**: Add edge case: "纯 IPv6 客户端需要 allowlist 中包含精确 IPv6 地址才能通过（CIDR 匹配仅支持 IPv4）"

---

### E4. Race condition between auth check and consumption (MEDIUM)

**TS flow**: `authorizeDownstreamToken` checks `usedRequests >= maxRequests`, returns success, then `consumeManagedKeyRequest` increments. Between these two calls, another concurrent request could also pass the check, causing both to exceed the quota by 1.

**TS mitigation**: The SQL increment uses `coalesce(usedRequests, 0) + 1` which is atomic per-row, but the check-then-increment gap is not atomic. No row-level lock or transaction is used.

**Spec**: Mentions "消费计数器 (used_cost++, used_requests++)" as if it happens atomically within the auth flow. Does not note this race condition.

**Fix**: Add edge case note about the check-increment race and that the SQL atomic increment mitigates lost-update but not over-quota-by-one.

---

### E5. `max_cost = 0` immediately blocks the key (LOW)

**TS** (downstreamApiKeyService.ts line 452):
```typescript
if (managed.maxCost !== null && managed.usedCost >= managed.maxCost)
```
Since `usedCost` starts at 0, a key with `maxCost = 0` is permanently blocked. This is logical but worth documenting as it may surprise operators.

---

### E6. Empty/managed DB lookup failure modes (LOW)

**TS**: If `downstream_api_keys` table is empty, all tokens (except global proxy token) get 403 "Invalid API key". No explicit handling for DB errors -- if the query throws, it propagates as a 500.

**Spec**: Does not discuss what happens when the DB is unreachable during key lookup.

---

## Incorrect Details

### I1. Status code summary (already detailed in A1, A2 above)

The spec uses 401 for nearly all failure modes. The TS source uses a meaningful 401/403 split:
- **401**: Missing credentials (no Authorization header, empty token)
- **403**: Valid credentials presented but not authorized (wrong admin token, IP not allowed, managed key disabled/expired/over-quota, unknown downstream token)

### I2. Both counters claimed in auth flow (already detailed in A4)

Only `usedRequests` is incremented pre-request. `usedCost` is a separate post-response call.

### I3. Token extraction "优先级" as fallthrough chain (already detailed in A3)

---

## Summary

| Category | Count |
|---|---|
| Accuracy Issues | 4 |
| Missing Details | 6 |
| Edge Cases Not Covered | 6 |
| Incorrect Details | 3 |

### Verdict: **NEEDS_REVISION**

The spec is structurally sound and covers the major flows, but has three blocking issues that will produce incorrect Go code if implemented as written:

1. **All managed-key failure status codes are wrong** (403 not 401) -- this affects every error response the proxy returns.
2. **`denyAllWhenEmpty` is completely missing** from the policy struct -- this controls allow-all vs deny-all for empty policies and the Go implementation cannot work correctly without it.
3. **Token extraction priority semantics** are described as fallthrough when they are actually an Authorization-header-takes-all precedence model -- this changes which token is used when multiple sources are present.

The `usedCost` counter claim is also incorrect but is less likely to cause bugs (it changes where the increment happens, not whether it happens).

**Recommended fix priority**: M1, A2, A3, A1, A4, then remaining items.
