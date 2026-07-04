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

```
流程:
1. 检查 X-Forwarded-For / RemoteAddr 是否在 adminIpAllowlist CIDR 集合中
   - 如果 allowlist 为空 → 跳过 IP 检查
   - 支持单个 IP 和 CIDR (如 "10.0.0.0/8")
2. 检查 Authorization header: Bearer <authToken>
3. 任一失败 → 401 JSON
```

**公开端点** (无需 admin auth):
- `GET /api/desktop/health`
- `GET /api/oauth/callback/*`

### 2. ProxyAuth Middleware
应用于所有 `/v1/*` 代理路由。

```
流程:
1. 从以下位置提取 token (按优先级):
   - Authorization: Bearer <token>
   - x-api-key: <token>
   - x-goog-api-key: <token>
   - ?key=<token> (query param)
2. 调用 AuthorizeDownstreamToken:
   a. 先查 downstream_api_keys 表 (managed keys)
      - 匹配 key → 返回 ProxyAuthContext{Source:"managed", KeyID, KeyName, Policy}
      - 检查 enabled + expires_at + max_cost/used_cost + max_requests/used_requests
      - 消费计数器 (used_cost++, used_requests++)
   b. 如果非 managed key → 检查是否等于 config.ProxyToken (global token)
      - 匹配 → 返回 ProxyAuthContext{Source:"global"}
   c. 都不匹配 → 401
3. 将 ProxyAuthContext 注入 context
```

### 3. DownstreamRoutingPolicy
```go
type DownstreamRoutingPolicy struct {
    AllowedRouteIDs       []int64             // json: allowed_route_ids
    SiteWeightMultipliers map[int64]float64   // json: site_weight_multipliers
    ExcludedSiteIDs       []int64             // json: excluded_site_ids
    ExcludedCredentialRefs []CredentialRef    // json: excluded_credential_refs
    SupportedModels       []string            // json: supported_models
}
```

## Acceptance Criteria
- [ ] AdminAuth: 无 allowlist → 只检查 Bearer token
- [ ] AdminAuth: CIDR allowlist → 白名单 IP 通过, 其他 401
- [ ] AdminAuth: 公开端点不经过认证
- [ ] ProxyAuth: Authorization header → 正确提取 token
- [ ] ProxyAuth: x-api-key header → 正确提取 token
- [ ] ProxyAuth: ?key= query param → 正确提取 token
- [ ] ProxyAuth: managed key → 校验 enabled/expires_at/配额
- [ ] ProxyAuth: global proxy token → 正确识别
- [ ] ProxyAuth: 未知 token → 401
- [ ] managed key 用量计数器正确递增
- [ ] AuthContext 通过 context 传递到 handler

## Test Plan
| 文件 | 内容 |
|------|------|
| `auth/admin_test.go` | IP 匹配 (exact/CIDR), token 断言, 公开端点 |
| `auth/proxy_test.go` | token 提取 (3 种), managed/global 区分, 401 |
| `auth/downstream_test.go` | key CRUD + 配额检查 + 消费计数 |
| `auth/policy_test.go` | 序列化/反序列化 |

## Edge Cases
- X-Forwarded-For 含多个 IP (proxy chain) → 取第一个
- CIDR `0.0.0.0/0` → 全放行
- managed key `expires_at` 已过期 → 401
- managed key `max_cost` 不为 null 且 used_cost ≥ max_cost → 401
- managed key `enabled=false` → 401
- token 为空字符串 → 401
