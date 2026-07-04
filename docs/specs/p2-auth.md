# P2: Auth Middleware (admin + downstream proxy)

**S.U.P.E.R**: S (单一职责) · P (端口优先) | **依赖**: P1 | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\middleware\auth.ts` — 双中间件 (admin + proxy)
- `D:\Code\TokenDance\metapi\src\server\desktop.ts` — `isPublicApiRoute`
- `D:\Code\TokenDance\metapi\src\server\services\downstreamApiKeyService.ts` — managed key 授权核心
- `D:\Code\TokenDance\metapi\src\server\services\downstreamPolicyTypes.ts` — 下游策略类型
- `D:\Code\TokenDance\metapi\src\server\config.ts` — `adminIpAllowlist`, `authToken`, `proxyToken`

## Go 模块结构
```
auth/
  admin.go           # AdminAuth: Bearer token + IP CIDR allowlist
  proxy.go           # ProxyAuth: managed key + global proxy token
  downstream.go      # AuthorizeDownstreamToken: key lookup + policy resolution
  policy.go          # DownstreamRoutingPolicy struct
  context.go         # AuthContext 存到 context.Context
```

## 功能规格

### 1. AdminAuth Middleware
应用于所有 `/api/*` 路由 (除公开端点)。

**IP 提取与规范化** (在 IP 校验之前执行):
1. 取 `X-Forwarded-For` header (可能是 `string` 或 `[]string`):
   - 如果是数组: 找到第一个非空元素, 按逗号分割, 取第一段
   - 如果是字符串: 直接按逗号分割, 取第一段
   - 如果不存在或为空: 回退到 `RemoteAddr`
2. 将提取到的 IP 进行规范化 (`normalizeIp`):
   - IPv4-mapped IPv6 地址 (`::ffff:x.x.x.x`) → 去掉 `::ffff:` 前缀, 转为纯 IPv4
   - IPv6 loopback (`::1`) → `127.0.0.1`
   - 其他地址保持原样

**IP 白名单校验** (`isIpAllowed`):
- 如果 `adminIpAllowlist` 为空或长度为 0 → 跳过 IP 检查 (直接通过)
- 白名单条目解析 (`parseAllowlistEntry`):
  - 不含 `/` → 精确 IP 匹配 (exact): 存储 `normalizedIp` 字符串
  - 含 `/` → CIDR 匹配: 网络地址和子网掩码 (仅支持 IPv4, prefix 0-32)
  - 无效条目 → 跳过 (返回 null)
- 匹配逻辑:
  - exact 条目: 字符串比较 `normalizedClientIp === entry.normalizedIp`
  - CIDR 条目: 仅当客户端 IP 是 IPv4 时 (`parseIpv4Value` 返回非 null) 才进行位运算匹配 `(clientIpv4Value & mask) === network`
  - **重要**: 纯 IPv6 客户端 (非 `::ffff:` 映射) 的 `parseIpv4Value` 返回 null, 因此无法通过任何 CIDR 条目。纯 IPv6 只能通过精确匹配条目 (如 `2001:db8::1`) 通过白名单。

**Authorization 校验**:
1. 检查 `Authorization` header 是否存在 (falsy 检查: undefined/null/空字符串)
2. 提取 Bearer token: 使用**区分大小写**的简单字符串替换 `auth.replace('Bearer ', '')` (注意: admin 端使用 `replace` 而非正则, 只替换第一个 `"Bearer "` 字面量, 不处理多余空格)
3. 比较 token 与 `config.authToken`

**流程 (按顺序, 任一步失败即返回)**:
```
1. 提取客户端 IP (X-Forwarded-For → RemoteAddr 回退) + normalizeIp
2. IP 不在白名单 (allowlist 非空且不匹配) → 403 {"error": "IP not allowed"}
3. 缺少 Authorization header → 401 {"error": "Missing Authorization header"}
4. token 不匹配 config.authToken → 403 {"error": "Invalid token"}
```

**公开端点** (无需 admin auth):
- `GET /api/desktop/health`
- `GET /api/oauth/callback/*`

### 2. ProxyAuth Middleware
应用于所有 `/v1/*` 代理路由。

**Token 提取** (绝对优先级, 非简单回退链):

关键语义: **如果 `Authorization` header 存在且为 string 类型, 则独占提取, 即使 Bearer 后为空也不回退到其他来源。**

```
1. 检查 Authorization header:
   - 类型守卫: 必须是 string 类型 (typeof === 'string'), 非 string (undefined/数组) → 视为不存在
   - 如果 Authorization 存在且为 string:
     → 使用大小写不敏感正则 /^Bearer\s+/i 去除前缀
     → .trim() 得到最终 token
     → **结束提取, 不再检查其他来源** (即使结果为 "")
   - 如果 Authorization 不存在/非 string:
     → 按以下优先级回退:
       a. x-api-key header (trim 后取)
       b. x-goog-api-key header (trim 后取)
       c. ?key= query 参数 (trim 后取)
```

**极端情况**: `Authorization: Bearer ` (Bearer 后仅空格) + `x-api-key: valid-key`
→ Authorization 是 string `"Bearer "`, 正则替换后得到 `""` → 返回 401 Missing。
**不会**回退到 x-api-key。

**Token 为空**: 如果最终提取的 token 为空字符串 → 401 `{"error": "Missing Authorization, x-api-key, x-goog-api-key, or key query parameter"}`

**下游授权** (`authorizeDownstreamToken`):

```
2. 将 token 传给 authorizeDownstreamToken(token):
   a. 如果 token 为空 → 401, reason: "missing"
   b. 查 downstream_api_keys 表 WHERE key = token:
      - 找到 match:
        i.   enabled === false → 403 "API key is disabled", reason: "disabled"
        ii.  expiresAt 已过期 → 403 "API key is expired", reason: "expired"
        iii. maxCost !== null 且 usedCost >= maxCost → 403 "API key has exceeded max cost", reason: "over_cost"
        iv.  maxRequests !== null 且 usedRequests >= maxRequests → 403 "API key has exceeded max requests", reason: "over_requests"
        v.  全部通过 → 返回成功, source: "managed", key=DDRow, policy=toPolicyFromView(row)
      - 未找到 → 继续 c
   c. 检查是否等于 config.proxyToken (global proxy token):
      - 匹配 → 返回成功, source: "global", key=null, policy=getDefaultGlobalPolicy()
      - 不匹配 → 403 "Invalid API key", reason: "invalid"
```

**重要: 401 vs 403 区分**:
| 场景 | HTTP 状态码 | 语义 |
|------|------------|------|
| 无 Authorization header 且无其他 token 来源 (token 为空) | **401** | 未提供凭证 |
| Authorization header 存在但 token 为空 (无其他来源回退) | **401** | 未提供凭证 |
| managed key disabled | **403** | 已认证但被禁止 |
| managed key expired | **403** | 已认证但被禁止 |
| managed key over_cost | **403** | 已认证但被禁止 |
| managed key over_requests | **403** | 已认证但被禁止 |
| unknown token (既非 managed key 也非 global token) | **403** | 已认证但被禁止 |

**用量消费** (分两步, 不在 auth 流程内原子完成):

```
3. 如果 authResult.source === 'managed' 且 authResult.key 存在:
   → consumeManagedKeyRequest(keyId): 原子 SQL 递增 used_requests
     UPDATE downstream_api_keys SET
       used_requests = coalesce(used_requests, 0) + 1,
       last_used_at = now(),
       updated_at = now()
     WHERE id = keyId

4. 请求完成后 (在 proxy handler 中, 已知实际费用时):
   → recordManagedKeyCostUsage(keyId, estimatedCost): 原子 SQL 递增 used_cost
     UPDATE downstream_api_keys SET
       used_cost = coalesce(used_cost, 0) + cost,
       last_used_at = now(),
       updated_at = now()
     WHERE id = keyId
     仅当 estimatedCost > 0 时执行
```

**Auth 上下文存储**:
- 使用 request-scoped 存储 (Go 中用 `context.WithValue`)
- 结构:
  ```go
  type ProxyAuthContext struct {
      Token   string                  // 规范化后的 token
      Source  string                  // "managed" | "global"
      KeyID   *int64                  // managed key 的 ID, global 时为 nil
      KeyName string                  // managed key 的 name, global 时为 "global"
      Policy  DownstreamRoutingPolicy // 解析后的路由策略
  }
  ```

**ProxyResourceOwner** (用于下游用量归属):
```go
type ProxyResourceOwner struct {
    OwnerType string // "managed_key" | "global_proxy_token"
    OwnerID   string // managed_key: keyId 转 string (或 token 作为回退); global: "global"
}
```
- managed_key: 优先使用 `String(keyId)`, 如果 keyId 为 nil 则使用 token
- global_proxy_token: 固定为 `"global"`

### 3. DownstreamRoutingPolicy

```go
type CredentialRefKind string

const (
    CredentialRefAccountToken  CredentialRefKind = "account_token"
    CredentialRefDefaultApiKey CredentialRefKind = "default_api_key"
)

// account_token 变体: 排除特定账户的特定 token
// default_api_key 变体: 排除特定账户的默认 API key (无 tokenId)
type ExcludedCredentialRef struct {
    Kind      CredentialRefKind `json:"kind"`
    SiteID    int64             `json:"site_id"`
    AccountID int64             `json:"account_id"`
    TokenID   *int64            `json:"token_id,omitempty"` // 仅 account_token 时有值
}

type DownstreamRoutingPolicy struct {
    SupportedModels        []string                `json:"supported_models"`         // 模型匹配模式列表
    AllowedRouteIDs        []int64                 `json:"allowed_route_ids"`        // 允许的路由 ID 列表
    SiteWeightMultipliers  map[int64]float64       `json:"site_weight_multipliers"`  // site_id → 权重倍数
    ExcludedSiteIDs        []int64                 `json:"excluded_site_ids"`        // 排除的 site ID 列表
    ExcludedCredentialRefs []ExcludedCredentialRef `json:"excluded_credential_refs"` // 排除的凭证引用列表
    DenyAllWhenEmpty       bool                    `json:"deny_all_when_empty"`      // 当 supportedModels 和 allowedRouteIds 都为空时是否拒绝所有模型
}
```

**`DenyAllWhenEmpty` 字段说明 (关键)**:
- 当 `SupportedModels` 和 `AllowedRouteIDs` 都为空时, 此字段决定默认行为:
  - `true`: **拒绝所有模型请求** (managed key 的默认值, 由 `toPolicyFromView` 设置)
  - `false`: **允许所有模型请求** (global proxy token 的默认值, `EMPTY_DOWNSTREAM_ROUTING_POLICY` 不设置此字段, Go 中 bool 零值为 false)
- 此字段是 managed key 和 global token 的关键行为差异点

**Policy 来源和 DenyAllWhenEmpty 默认值**:
| 来源 | DenyAllWhenEmpty | 语义 |
|------|-----------------|------|
| managed key (`toPolicyFromView`) | `true` | 未配置模型的 key 默认拒绝所有模型 |
| global proxy token (`getDefaultGlobalPolicy`) | `false` | 全局 token 未配置模型时默认允许所有模型 |

### 4. 模型匹配逻辑 (`isModelAllowedByPolicyOrAllowedRoutes`)

判断给定模型是否被策略允许的完整逻辑:

```
输入: model (string), policy (DownstreamRoutingPolicy)

1. 规范化 supportedModels 和 allowedRouteIds
2. 计算:
   hasPatternRules = len(supportedModels) > 0
   hasRouteRules   = len(allowedRouteIds) > 0

3. 如果 !hasPatternRules && !hasRouteRules:
   → 返回 !policy.DenyAllWhenEmpty
     (DenyAllWhenEmpty=true → false; DenyAllWhenEmpty=false → true)

4. 如果 hasPatternRules:
   对每个 pattern 调用 matchesDownstreamModelPattern(model, pattern):
   - 精确匹配: pattern === model → true
   - 正则匹配: pattern 以 "re:" (大小写不敏感) 开头 → 解析正则并 test(model)
   - Glob 匹配: 使用 minimatch(model, pattern)
   - 任意 pattern 匹配 → 返回 true

5. 如果 !hasRouteRules → 返回 false

6. (hasRouteRules) 查询 token_routes 表:
   SELECT id, model_pattern, display_name FROM token_routes
   WHERE id IN (allowedRouteIds) AND enabled = true
   对每条 route: 取 displayName (空则取 modelPattern) 与 model 比较
   任意 route 匹配 → 返回 true, 否则 false
```

**注意**: 还有一个简化版 `isModelAllowedByPolicy` (仅检查 supportedModels 模式, 不考虑 allowedRouteIds 和 denyAllWhenEmpty), 用于非代理路径的策略检查。当 patterns 为空时返回 true (允许所有)。

### 5. Managed Key CRUD 相关类型

**DownstreamApiKeyPolicyView** (managed key 完整视图):
```go
type DownstreamApiKeyPolicyView struct {
    ID                    int64                   `json:"id"`
    Name                  string                  `json:"name"`
    Key                   string                  `json:"key"`
    KeyMasked             string                  `json:"key_masked"`     // 前4后4, 中间****
    Description           *string                 `json:"description"`
    GroupName             *string                 `json:"group_name"`     // max 64 chars
    Tags                  []string                `json:"tags"`           // max 20, 每个 max 32 chars, 大小写不敏感去重
    Enabled               bool                    `json:"enabled"`
    ExpiresAt             *string                 `json:"expires_at"`     // ISO 8601
    MaxCost               *float64                `json:"max_cost"`       // null = 无限
    UsedCost              float64                 `json:"used_cost"`
    MaxRequests           *int64                  `json:"max_requests"`   // null = 无限
    UsedRequests          int64                   `json:"used_requests"`
    SupportedModels        []string               `json:"supported_models"`
    AllowedRouteIDs        []int64                `json:"allowed_route_ids"`
    SiteWeightMultipliers  map[int64]float64      `json:"site_weight_multipliers"`
    ExcludedSiteIDs        []int64                `json:"excluded_site_ids"`
    ExcludedCredentialRefs  []ExcludedCredentialRef `json:"excluded_credential_refs"`
    LastUsedAt            *string                 `json:"last_used_at"`
    CreatedAt             *string                 `json:"created_at"`
    UpdatedAt             *string                 `json:"updated_at"`
}
```

**DownstreamTokenAuthResult** (授权结果):
```go
type DownstreamTokenAuthResult struct {
    OK         bool                         // 是否通过
    Source     string                       // "managed" | "global" (OK=true 时)
    Token      string                       // 规范化后的 token (OK=true 时)
    Key        *DownstreamApiKeyPolicyView  // managed key (OK=true, source="managed" 时)
    Policy     *DownstreamRoutingPolicy     // 解析后的路由策略 (OK=true 时)
    StatusCode int                          // HTTP 状态码 (OK=false 时)
    Error      string                       // 错误消息 (OK=false 时)
    Reason     string                       // 失败原因: "missing"|"invalid"|"disabled"|"expired"|"over_cost"|"over_requests"
}
```

### 6. 用量消费函数

**consumeManagedKeyRequest** (请求计数, 在 proxyAuthMiddleware 中调用):
```sql
UPDATE downstream_api_keys
SET used_requests = coalesce(used_requests, 0) + 1,
    last_used_at = ?,
    updated_at = ?
WHERE id = ?
```
- 使用 SQL `coalesce + 1` 实现原子递增, 避免多进程并发下的 lost update
- 同时更新 `last_used_at` 和 `updated_at`

**recordManagedKeyCostUsage** (费用计数, 在 proxy handler 中请求完成后调用):
```sql
UPDATE downstream_api_keys
SET used_cost = coalesce(used_cost, 0) + ?,
    last_used_at = ?,
    updated_at = ?
WHERE id = ?
```
- 仅当 `estimatedCost > 0` 时执行
- 使用 SQL 原子递增避免 lost update

## Acceptance Criteria
- [ ] AdminAuth: 空 allowlist → 只检查 Bearer token
- [ ] AdminAuth: CIDR allowlist → 白名单 IP 通过, 其他返回 403
- [ ] AdminAuth: 缺少 Authorization header → 401
- [ ] AdminAuth: 错误 token → 403
- [ ] AdminAuth: 公开端点不经过认证
- [ ] AdminAuth: IPv4-mapped IPv6 (`::ffff:x.x.x.x`) → 正确转为纯 IPv4 后匹配
- [ ] AdminAuth: IPv6 loopback (`::1`) → 转为 `127.0.0.1` 后匹配
- [ ] AdminAuth: 纯 IPv6 客户端 + 非空 allowlist (不含精确 IPv6 条目) → 被拒绝
- [ ] ProxyAuth: Authorization header 存在时独占提取 (即使 Bearer 为空也不回退)
- [ ] ProxyAuth: Bearer 前缀大小写不敏感匹配
- [ ] ProxyAuth: x-api-key / x-goog-api-key / ?key= 仅在 Authorization 不存在时生效
- [ ] ProxyAuth: managed key disabled → 403
- [ ] ProxyAuth: managed key expired → 403
- [ ] ProxyAuth: managed key over_cost → 403
- [ ] ProxyAuth: managed key over_requests → 403
- [ ] ProxyAuth: unknown token → 403
- [ ] ProxyAuth: token 为空 → 401
- [ ] ProxyAuth: global proxy token → 正确识别, source="global"
- [ ] ProxyAuth: managed key 通过后 used_requests 原子递增
- [ ] ProxyAuth: denyAllWhenEmpty=true (managed key 空策略) → 拒绝所有模型
- [ ] ProxyAuth: denyAllWhenEmpty=false (global token 空策略) → 允许所有模型
- [ ] ProxyAuth: max_cost=0 → 立即拒绝 (usedCost=0 >= maxCost=0)
- [ ] AuthContext 通过 context 传递到 handler
- [ ] ProxyResourceOwner 正确派生 (managed_key / global_proxy_token)

## Test Plan
| 文件 | 内容 |
|------|------|
| `auth/admin_test.go` | IP 匹配 (exact/CIDR/IPv6规范化), token 断言, 公开端点, IP拒绝=403, 缺header=401, 错token=403 |
| `auth/proxy_test.go` | token 提取 (4 种来源 + 独占语义), managed/global 区分, 各失败场景状态码 (401/403), denyAllWhenEmpty 行为 |
| `auth/downstream_test.go` | key CRUD + 配额检查 + 消费计数 (used_requests 原子递增) |
| `auth/policy_test.go` | ExcludedCredentialRef 序列化/反序列化 (两种 kind), denyAllWhenEmpty, 模型匹配 (exact/regex/glob) |

## Edge Cases
- X-Forwarded-For 含多个 IP (proxy chain) → 取逗号分割后的第一个
- X-Forwarded-For 为数组 (多 header) → 遍历找第一个非空元素
- CIDR `0.0.0.0/0` (prefix=0, mask=0) → 全放行任何 IPv4
- IPv4-mapped IPv6 (`::ffff:10.0.0.1`) → 规范化后按 IPv4 CIDR 匹配
- IPv6 loopback (`::1`) → 规范化为 `127.0.0.1` 后匹配
- 纯 IPv6 客户端 + 非空 allowlist → 只能通过精确 IPv6 条目匹配, CIDR 条目无效 (parseIpv4Value 返回 null)
- `Authorization: Bearer ` (Bearer 后仅空格) + `x-api-key: valid` → Authorization 存在, 提取后 token 为空 → 401, 不回退
- `Authorization` header 为数组 (多值) → typeof !== 'string' → 视为不存在, 回退到其他来源
- AdminAuth Bearer 提取: 简单 `replace("Bearer ", "")`, 区分大小写, 不处理多余空格
- ProxyAuth Bearer 提取: 正则 `/^Bearer\s+/i`, 大小写不敏感, 处理任意空白
- managed key `expires_at` 解析: `Date.parse()` 结果必须是有限数字, 且 `<= Date.now()` 才算过期
- managed key `max_cost = 0` → 立即被阻塞 (因为 usedCost (0) >= maxCost (0))
- managed key `max_requests = 0` → 立即被阻塞 (因为 usedRequests (0) >= maxRequests (0))
- managed key 配额检查与消费之间存在 race condition: check-then-increment 非原子, 并发下可能超出配额 1 次。SQL 原子递增仅防止 lost-update
- DB 不可用: SQL 查询异常向上传播 → 500 响应 (非 spec 管理范围, 但实现需注意)
- `downstream_api_keys` 表为空: 所有 token (除 global proxy token) 返回 403 "Invalid API key"
- `used_cost` 递增 (`recordManagedKeyCostUsage`) 不在 auth 中间件内执行, 由下游 proxy handler 在请求完成后单独调用
- `last_used_at` 在 `consumeManagedKeyRequest` 和 `recordManagedKeyCostUsage` 中均会更新
