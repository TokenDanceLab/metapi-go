# P3: Sites + Accounts + AccountTokens CRUD API

**S.U.P.E.R**: S (单一职责) · P (端口优先) | **依赖**: P1 + P2 | **Size**: L

## 原始 TS 参考
- `<metapi-ts>\src\server\routes\api\sites.ts` (含 api-endpoints + disabled-models + available-models + probe-now + probe-stream + detect 子路由)
- `<metapi-ts>\src\server\routes\api\accounts.ts` (含 login/verifyToken/rebind-session/balance/models/manual-models/health-refresh)
- `<metapi-ts>\src\server\routes\api\accountTokens.ts` (含 sync/groups/default/value/upstream-create)
- `<metapi-ts>\src\server\contracts\siteRoutePayloads.ts` — Zod schemas
- `<metapi-ts>\src\server\contracts\accountsRoutePayloads.ts` — Zod schemas
- `<metapi-ts>\src\server\contracts\accountTokensRoutePayloads.ts` — Zod schemas
- `<metapi-ts>\src\server\services\siteDetector.ts`
- `<metapi-ts>\src\server\services\siteApiEndpointService.ts`
- `<metapi-ts>\src\server\services\accountCredentialService.ts` — AES 加密 (password only)
- `<metapi-ts>\src\server\services\accountMutationWorkflow.ts`
- `<metapi-ts>\src\server\services\accountTokenService.ts`
- `<metapi-ts>\src\server\services\accountExtraConfig.ts`
- `<metapi-ts>\src\server\services\manualAccountCreationService.ts`
- `<metapi-ts>\src\server\services\accountsOverviewService.ts`
- `<metapi-ts>\src\server\services\accountUpdateWorkflow.ts`

## Go 模块结构
```
handler/admin/
  sites.go                  # Sites CRUD 端点
  sites_detect.go           # POST /api/sites/detect
  sites_disabled_models.go  # Site Disabled Models 子资源
  sites_available_models.go # GET /api/sites/:id/available-models
  sites_probe.go            # POST /api/sites/:id/probe-now
  sites_probe_stream.go     # GET /api/sites/:id/probe-stream (SSE)
  accounts.go               # Accounts CRUD 端点
  accounts_login.go         # 登录/验证/rebind
  accounts_balance.go       # 余额刷新
  accounts_models.go        # 模型查询/手动设置
  accounts_health.go        # POST /api/accounts/health/refresh
  account_tokens.go         # AccountTokens CRUD 端点
  account_tokens_sync.go    # 同步端点
  account_tokens_groups.go  # 分组端点
  payloads/
    sites.go                # Create/Update/Batch/Detect/DisabledModels payload structs + validation
    accounts.go             # Create/Update/Login/Rebind/VerifyToken/HealthRefresh/ManualModels payloads
    account_tokens.go       # Create/Update/Batch/SyncAll payloads
service/
  site_service.go           # 站点业务逻辑
  site_detect.go            # 平台检测逻辑
  site_endpoint_service.go  # API 端点管理 (URL规范化, 去重)
  account_service.go        # 账号业务逻辑
  account_credential.go     # AES-256-GCM 加密/解密 password (autoRelogin)
  account_mutation.go       # convergeAccountMutation 创建工作流
  account_extra_config.go   # extraConfig JSON 读写 (credentialMode, proxyUrl, platformUserId, autoRelogin, sub2apiAuth)
  account_token_service.go  # Token CRUD + sync + default repair + masked-pending
  model_service.go          # 模型刷新 (refreshModelsForAccount)
  route_refresh.go          # 路由重建 (rebuildRoutesOnly, rebuildRoutesBestEffort)
```

## API 端点清单

### Sites (11 端点)
| Method | Path | Payload | 说明 |
|--------|------|---------|------|
| GET | `/api/sites` | -- | 列表 (含 totalBalance, subscriptionSummary, apiEndpoints) |
| POST | `/api/sites` | `SiteCreatePayload` | 创建站点 (含 apiEndpoints 嵌入 + 平台自动检测) |
| PUT | `/api/sites/:id` | `SiteUpdatePayload` | 更新站点 (含 postRefreshProbe* 字段) |
| DELETE | `/api/sites/:id` | -- | 级联删除 (依赖 DB FOREIGN KEY ON DELETE CASCADE) |
| POST | `/api/sites/batch` | `SiteBatchPayload` | 批量操作 (enable/disable/delete/enableSystemProxy/disableSystemProxy) |
| POST | `/api/sites/detect` | `SiteDetectPayload{url}` | 检测平台类型 |
| GET | `/api/sites/:id/disabled-models` | -- | 获取禁用模型列表 |
| PUT | `/api/sites/:id/disabled-models` | `SiteDisabledModelsPayload` | 全量替换禁用模型列表 |
| GET | `/api/sites/:id/available-models` | -- | 获取全站可用模型 (合并 model_availability + token_model_availability) |
| POST | `/api/sites/:id/probe-now` | body: `{scope?, modelName?, latencyThresholdMs?}` | 立即探测模型 (JSON 响应) |
| GET | `/api/sites/:id/probe-stream` | query: `scope`, `modelName`, `latencyThresholdMs` | SSE 流式探测 (probe-start/probe-model-checked/probe-model-result/complete/error) |

### Accounts (12 端点)
| Method | Path | Payload | 说明 |
|--------|------|---------|------|
| GET | `/api/accounts` | query: `?refresh` | 列表 (快照缓存; 响应含 generatedAt, accounts, sites) |
| POST | `/api/accounts` | `AccountCreatePayload` | 创建账号 (支持批量 API key import) |
| POST | `/api/accounts/login` | `AccountLoginPayload` | 登录获取 session (含 reusedAccount 检测) |
| POST | `/api/accounts/verify-token` | `AccountVerifyTokenPayload` | 验证 token 有效性 (含 shield-blocked/needs-user-id/invalid-user-id 诊断) |
| POST | `/api/accounts/:id/rebind-session` | `AccountRebindSessionPayload` | 重新绑定 session token |
| PUT | `/api/accounts/:id` | `AccountUpdatePayload` | 更新账号 (含 sub2api managed auth + expired API key 恢复) |
| DELETE | `/api/accounts/:id` | -- | 删除账号 (DB CASCADE + rebuildRoutesBestEffort) |
| POST | `/api/accounts/batch` | `AccountBatchPayload` | 批量操作 (enable/disable/delete/refreshBalance) |
| POST | `/api/accounts/health/refresh` | `AccountHealthRefreshPayload` | 健康检查 (wait=true 同步 / wait=false 后台任务) |
| POST | `/api/accounts/:id/balance` | -- | 刷新余额 |
| GET | `/api/accounts/:id/models` | -- | 获取模型列表 (含 site-level disabled 标记) |
| POST | `/api/accounts/:id/models/manual` | `AccountManualModelsPayload` | 手动设置模型 (upsert + rebuildRoutesBestEffort) |

### AccountTokens (11 端点)
| Method | Path | Payload | 说明 |
|--------|------|---------|------|
| GET | `/api/account-tokens` | query: `?accountId` | 列表 (含 relations) |
| POST | `/api/account-tokens` | `AccountTokenCreatePayload` | 创建 token (双路径: local vs upstream) |
| POST | `/api/account-tokens/batch` | `AccountTokenBatchPayload` | 批量操作 (enable/disable/delete) |
| PUT | `/api/account-tokens/:id` | `AccountTokenUpdatePayload` | 更新 token |
| POST | `/api/account-tokens/:id/default` | -- | 设为默认 token |
| GET | `/api/account-tokens/:id/value` | -- | 获取 token 明文 (masked -> 409, apikey connection -> 400) |
| DELETE | `/api/account-tokens/:id` | -- | 删除 token (upstream-first 策略) |
| POST | `/api/account-tokens/sync/:accountId` | -- | 同步单个账号 token |
| POST | `/api/account-tokens/sync-all` | `AccountTokenSyncAllPayload` | 全量同步 (wait=true 同步 / wait=false 后台任务, batch=3) |
| GET | `/api/account-tokens/groups/:accountId` | -- | 按分组查看 |
| GET | `/api/account-tokens/account/:id/default` | -- | 获取默认 token (返回 tokenMasked, apikey connection -> null) |

## Payload Definitions (Complete)

### SiteCreatePayload
```go
type SiteCreatePayload struct {
    Name                   string  `json:"name" validate:"required,min=1"`             // non-empty trimmed string
    URL                    string  `json:"url" validate:"required,min=1"`              // non-empty trimmed string; canonicalized via analyzePrimarySiteUrl
    Platform               *string `json:"platform,omitempty"`                         // optional trimmed lowercase; if empty, auto-detected
    InitializationPresetID *string `json:"initializationPresetId,omitempty"`           // string or null; if provided, must match detected platform
    ProxyURL               *string `json:"proxyUrl,omitempty"`                         // validated http(s)/socks proxy URL or null
    UseSystemProxy         *bool   `json:"useSystemProxy,omitempty"`                   // boolean, default false
    CustomHeaders          *string `json:"customHeaders,omitempty"`                    // validated JSON or null
    ExternalCheckinURL     *string `json:"externalCheckinUrl,omitempty"`               // validated http(s) URL, null, or undefined
    Status                 *string `json:"status,omitempty"`                           // "active" or "disabled", default "active"
    IsPinned               *bool   `json:"isPinned,omitempty"`                         // boolean, default false
    SortOrder              *int    `json:"sortOrder,omitempty"`                        // non-negative integer, default max+1
    GlobalWeight           *float64 `json:"globalWeight,omitempty"`                    // positive number 0.01..100, default 1
    ApiEndpoints           []SiteApiEndpointInput `json:"apiEndpoints,omitempty"`      // embedded sub-resource, not a separate REST endpoint
}

type SiteApiEndpointInput struct {
    URL       string `json:"url" validate:"required"`       // validated http(s) URL, normalized, deduplicated
    Enabled   bool   `json:"enabled"`                       // default true
    SortOrder int    `json:"sortOrder"`                     // default index in array
}
```

### SiteUpdatePayload
```go
type SiteUpdatePayload struct {
    Name                         *string  `json:"name,omitempty"`
    URL                          *string  `json:"url,omitempty"`              // canonicalized
    Platform                     *string  `json:"platform,omitempty"`         // non-empty trimmed string
    ProxyURL                     *string  `json:"proxyUrl,omitempty"`
    UseSystemProxy               *bool    `json:"useSystemProxy,omitempty"`
    CustomHeaders                *string  `json:"customHeaders,omitempty"`
    ExternalCheckinURL           *string  `json:"externalCheckinUrl,omitempty"`
    Status                       *string  `json:"status,omitempty"`
    IsPinned                     *bool    `json:"isPinned,omitempty"`
    SortOrder                    *int     `json:"sortOrder,omitempty"`
    GlobalWeight                 *float64 `json:"globalWeight,omitempty"`
    ApiEndpoints                 []SiteApiEndpointInput `json:"apiEndpoints,omitempty"`
    PostRefreshProbeEnabled      *bool    `json:"postRefreshProbeEnabled,omitempty"`      // boolean
    PostRefreshProbeModel        *string  `json:"postRefreshProbeModel,omitempty"`        // trimmed string
    PostRefreshProbeScope        *string  `json:"postRefreshProbeScope,omitempty"`        // "all" or "single"
    PostRefreshProbeLatencyThresholdMs *int `json:"postRefreshProbeLatencyThresholdMs,omitempty"` // non-negative integer ms
}
```

### SiteBatchPayload
```go
type SiteBatchPayload struct {
    IDs    []int  `json:"ids"`
    Action string `json:"action" validate:"oneof=enable disable delete enableSystemProxy disableSystemProxy"`
}
```

### SiteDetectPayload
```go
type SiteDetectPayload struct {
    URL string `json:"url" validate:"required,min=1"`
}
```

### SiteDisabledModelsPayload
```go
type SiteDisabledModelsPayload struct {
    Models []string `json:"models"` // array of model name strings; full-replace semantics
}
```

### AccountCreatePayload
```go
type AccountCreatePayload struct {
    SiteID           int      `json:"siteId" validate:"required,gt=0"`
    Username         *string  `json:"username,omitempty"`        // optional string
    AccessToken      *string  `json:"accessToken,omitempty"`     // OPTIONAL — stored as PLAINTEXT in DB
    AccessTokens     []string `json:"accessTokens,omitempty"`    // batch API key import; when non-empty forces credentialMode=apikey
    APIToken         *string  `json:"apiToken,omitempty"`
    PlatformUserID   *int     `json:"platformUserId,omitempty"`  // positive integer, for NewAPI/OneAPI
    CheckinEnabled   *bool    `json:"checkinEnabled,omitempty"`
    CredentialMode   *string  `json:"credentialMode,omitempty" validate:"omitempty,oneof=auto session apikey"`
    RefreshToken     *string  `json:"refreshToken,omitempty"`    // sub2api managed OAuth
    TokenExpiresAt   *int64   `json:"tokenExpiresAt,omitempty"`  // Unix epoch seconds (accepts number or numeric string)
    SkipModelFetch   *bool    `json:"skipModelFetch,omitempty"`  // skip auto model fetch on creation
}
```

### AccountUpdatePayload
```go
type AccountUpdatePayload struct {
    Username         *string  `json:"username,omitempty"`
    AccessToken      *string  `json:"accessToken,omitempty"`      // stored as PLAINTEXT
    APIToken          any     `json:"apiToken,omitempty"`          // string or null
    Status           *string  `json:"status,omitempty"`
    CheckinEnabled   *bool    `json:"checkinEnabled,omitempty"`
    UnitCost         *float64 `json:"unitCost,omitempty"`         // number or null
    ExtraConfig       any     `json:"extraConfig,omitempty"`      // JSON string, object, or null
    RefreshToken     *string  `json:"refreshToken,omitempty"`     // string or null; sub2api managed OAuth
    TokenExpiresAt   *int64   `json:"tokenExpiresAt,omitempty"`   // epoch seconds, numeric string, or null
    IsPinned         *bool    `json:"isPinned,omitempty"`
    SortOrder        *int     `json:"sortOrder,omitempty" validate:"omitempty,gte=0"`
    ProxyURL         *string  `json:"proxyUrl,omitempty"`         // string or null; account-level proxy override; stored in extraConfig.proxyUrl
}
```

### AccountBatchPayload
```go
type AccountBatchPayload struct {
    IDs    []int  `json:"ids"`
    Action string `json:"action" validate:"oneof=enable disable delete refreshBalance"`
}
```

### AccountLoginPayload
```go
type AccountLoginPayload struct {
    SiteID   int    `json:"siteId" validate:"required,gt=0"`
    Username string `json:"username" validate:"required"`
    Password string `json:"password" validate:"required"`
}
```

### AccountVerifyTokenPayload
```go
type AccountVerifyTokenPayload struct {
    SiteID         int     `json:"siteId" validate:"required,gt=0"`
    AccessToken    *string `json:"accessToken,omitempty"`
    PlatformUserID *int    `json:"platformUserId,omitempty"` // positive integer
    CredentialMode *string `json:"credentialMode,omitempty" validate:"omitempty,oneof=auto session apikey"`
}
```

### AccountRebindSessionPayload
```go
type AccountRebindSessionPayload struct {
    AccessToken    *string `json:"accessToken,omitempty"`
    PlatformUserID *int    `json:"platformUserId,omitempty"`
    RefreshToken   *string `json:"refreshToken,omitempty"`    // sub2api managed
    TokenExpiresAt *int64  `json:"tokenExpiresAt,omitempty"`  // epoch seconds
}
```

### AccountHealthRefreshPayload
```go
type AccountHealthRefreshPayload struct {
    AccountID *int  `json:"accountId,omitempty"` // optional; if omitted, refresh all
    Wait      *bool `json:"wait,omitempty"`      // true=同步返回, false/omit=后台任务 202
}
```

### AccountManualModelsPayload
```go
type AccountManualModelsPayload struct {
    Models []string `json:"models"` // array of model name strings
}
```

### AccountTokenCreatePayload
```go
type AccountTokenCreatePayload struct {
    AccountID          int     `json:"accountId" validate:"required,gt=0"`
    Name               *string `json:"name,omitempty"`
    Token              *string `json:"token,omitempty"`              // when provided: local path; when absent: upstream create path
    Enabled            *bool   `json:"enabled,omitempty"`
    IsDefault          *bool   `json:"isDefault,omitempty"`
    Source             *string `json:"source,omitempty"`             // e.g. "manual"
    Group              *string `json:"group,omitempty"`              // tokenGroup
    UnlimitedQuota     *bool   `json:"unlimitedQuota,omitempty"`     // upstream: unlimited quota flag
    RemainQuota        any     `json:"remainQuota,omitempty"`        // number or string; required when unlimitedQuota=false
    ExpiredTime        any     `json:"expiredTime,omitempty"`        // number (epoch seconds) or ISO date string
    AllowIPs           *string `json:"allowIps,omitempty"`           // IP allowlist
    ModelLimitsEnabled *bool   `json:"modelLimitsEnabled,omitempty"` // model-specific limits enabled
    ModelLimits        *string `json:"modelLimits,omitempty"`        // model limits config string
}
```

### AccountTokenUpdatePayload
```go
type AccountTokenUpdatePayload struct {
    Name      *string `json:"name,omitempty"`
    Token     *string `json:"token,omitempty"`     // updating to masked value -> valueStatus becomes masked-pending
    Group     *string `json:"group,omitempty"`
    Enabled   *bool   `json:"enabled,omitempty"`
    IsDefault *bool   `json:"isDefault,omitempty"`
    Source    *string `json:"source,omitempty"`
}
```

### AccountTokenBatchPayload
```go
type AccountTokenBatchPayload struct {
    IDs    []int  `json:"ids"`
    Action string `json:"action" validate:"oneof=enable disable delete"`
}
```

### AccountTokenSyncAllPayload
```go
type AccountTokenSyncAllPayload struct {
    Wait *bool `json:"wait,omitempty"` // true=同步返回, false/omit=后台任务 202
}
```

### Response Shapes (Key Examples)

**GET /api/sites response**: array of site objects, each containing:
```json
{
  "id": 1,
  "name": "...",
  "url": "...",
  "platform": "openai",
  "status": "active",
  "apiEndpoints": [{ "id": 1, "url": "...", "enabled": true, "sortOrder": 0 }],
  "totalBalance": 12.50,
  "subscriptionSummary": { "activeCount": 3, "totalUsedUsd": 1.23, ... } | null
}
```

**GET /api/accounts response**:
```json
{
  "generatedAt": "2026-07-04T00:00:00.000Z",
  "accounts": [...],
  "sites": [...]
}
```
Response header: `x-accounts-snapshot-cache: hit` or `miss`.

**POST /api/accounts/login response**:
```json
{
  "success": true,
  "account": { ... },
  "apiTokenFound": true,
  "tokenCount": 3,
  "reusedAccount": false
}
```

**GET /api/account-tokens/:id/value response**:
```json
{
  "success": true,
  "id": 1,
  "name": "default",
  "token": "sk-abc123...",
  "tokenMasked": "sk-abc***123"
}
```
Masked token: HTTP 409 `{ "success": false, "message": "当前仅保存了脱敏令牌..." }`.
ApiKey connection: HTTP 400 `{ "success": false, "message": "API Key 连接不支持管理账号令牌" }`.

## 关键业务逻辑

### AES 加密 (Password Only — NOT accessToken)

- **加密对象**: `autoRelogin.password` (存储在 `extraConfig.autoRelogin.passwordCipher`), **不是** `accessToken`
- **`accessToken` 列**: 以 **明文** 存储在数据库中
- **算法**: `aes-256-gcm` (NOT CBC)
- **密钥**: SHA-256(`config.AccountCredentialSecret`), 32 bytes (TS: `config.accountCredentialSecret`, camelCase)
- **IV**: 随机 12 bytes (GCM nonce)
- **认证标签 (Auth Tag)**: 16 bytes
- **密文格式**: `v1:base64url(IV):base64url(authTag):base64url(ciphertext)`
  - 冒号分隔, 4 段
  - 版本前缀 `v1`
  - 所有二进制段使用 base64url 编码 (非标准 base64)
- **函数**: `EncryptAccountPassword(password string) string` / `DecryptAccountPassword(cipherText string) (string, error)`
- **解密失败**: 静默返回 null/error (不抛异常)

### credentialMode 解析逻辑

**存储态 resolution** (`resolveStoredCredentialMode`):
1. 从 `extraConfig.credentialMode` 读取显式值
2. 如果显式值存在且不是 `"auto"`, 返回该值
3. 如果 `accessToken` 非空字符串, 返回 `"session"`
4. 否则返回 `"apikey"`

**请求态 resolution** (`resolveRequestedCredentialMode`):
1. 从请求体读取 `credentialMode`, 规范化 (trim + lowercase)
2. 如果有效 (`auto`/`session`/`apikey`) 则返回; 否则返回 `"auto"`

**create 时的特殊规则**: 当 `accessTokens` (批量 API key) 非空时, `credentialMode` 强制为 `"apikey"`, 忽略请求体中的值

### 账号创建完整流程

**单 token 创建** (`createManualAccount`):
1. `adapter.verifyToken(url, accessToken, platformUserId)` -- 返回 `userInfo`, `balance`, `apiToken`, `tokenType`
2. 检查 `requestedTokens.length > 0`; 如果为 0, 返回 "请填写 Token" 错误
3. 在事务中写入 `accounts` 行 + `model_availability` (含 balance, extraConfig)
4. 调用 `convergeAccountMutation` 后处理 (continueOnError=true):
   - `ensureDefaultToken` (如果 apiToken 存在)
   - `syncTokensFromUpstream` (如果 upstream tokens 返回)
   - `refreshBalance`
   - `refreshModels` (skipModelFetch=false 时)
   - `rebuildRoutes`

**批量 API key 创建**: 当 `accessTokens` 非空且长度 > 1 时:
- 遍历每个 token, 每次调用 `createManualAccount`
- 返回 `{ batch: true, totalCount, createdCount, failedCount, items[] }`

### Account Login 流程

1. `adapter.login(site.url, username, password)` -- 获取 session token
2. 尝试 `adapter.getApiToken` 和 `adapter.getApiTokens` 自动获取 API key
3. 检测是否存在同 (siteId, username) 的已有账号
   - **存在**: UPDATE `accessToken`, `apiToken`, `checkinEnabled`, `status`, `extraConfig`
   - **不存在**: INSERT 新行
4. 构造 `extraConfig`: `{ credentialMode: "session", autoRelogin: { username, passwordCipher, updatedAt }, platformUserId? }`
5. `encryptAccountPassword(password)` 加密密码存入 `passwordCipher`
6. 调用 `convergeAccountMutation` (continueOnError=true)
7. 响应包含 `reusedAccount: true/false`

### convergeAccountMutation 详细步骤

执行顺序 (`accountMutationWorkflow.ts`):
1. **Ensure default token** (如果 `ensurePreferredTokenBeforeSync` 且 `preferredApiToken` 存在): 确保 preferredApiToken 有一个对应的 accountTokens 行并设为默认
2. **Sync tokens from upstream** (如果 `upstreamTokens` 非空): 调用 `syncTokensFromUpstream`
3. **Ensure default token** (如果未提前执行): 在 sync 之后确保 preferredApiToken
4. **Refresh balance** (如果 `refreshBalance`): 调用 `refreshBalance(accountId)`
5. **Refresh models** (如果 `refreshModels`): 调用 `refreshModelsForAccount`, 支持 `allowInactive: true`
6. **Rebuild routes** (如果 `rebuildRoutes`): 调用 `rebuildRoutesOnly()`

所有步骤在 `continueOnError=true` 时独立 try/catch, 单步失败不影响后续步骤. 返回聚合结果:
```go
type ConvergeAccountMutationResult struct {
    DefaultTokenID      *int64
    TokenSync           *TokenSyncResult
    RefreshedBalance    bool
    RefreshedModels     bool
    RebuiltRoutes       bool
    BalanceResult       *BalanceResult
    ModelRefreshResult  *ModelRefreshResult
    RebuildResult       *RebuildRoutesResult
}
```

### Site 删除: DB 级联删除

- **应用层**: 仅执行 `DELETE FROM sites WHERE id = ?`
- **级联**: 依赖数据库 FOREIGN KEY `ON DELETE CASCADE` 约束 (P1 中定义):
  - `accounts` -> `siteId` (CASCADE)
  - `siteApiEndpoints` -> `siteId` (CASCADE)
  - `siteDisabledModels` -> `siteId` (CASCADE)
  - `channels` -> `siteId` (CASCADE)
  - `modelAvailability` -> `accountId` (CASCADE from accounts)
  - `accountTokens` -> `accountId` (CASCADE from accounts)
- **后处理**: `invalidateSiteProxyCache()` + `invalidateTokenRouterCache()`

### Account 删除: DB 级联 + Route Rebuild

- **应用层**: `DELETE FROM accounts WHERE id = ?`
- **级联**: 依赖 DB CASCADE (accountTokens, modelAvailability 等)
- **后处理**: `rebuildRoutesBestEffort()` -- 必须调用, 失败静默忽略

### Site Status 变更副作用

当 site status 通过 update 或 batch 变更时 (`applySiteStatusSideEffects`):

**Disable**:
- 该 site 所有 accounts 的 status 设为 `"disabled"`
- 创建 events 行: `type="status"`, `title="站点已禁用"`, `level="warning"`, `relatedType="site"`

**Enable**:
- 仅将之前被禁用的 accounts (status="disabled") 恢复为 `"active"`; 不影响本身就是 active 的
- 创建 events 行: `type="status"`, `title="站点已启用"`, `level="info"`, `relatedType="site"`

### Token Value Status System

| Status | 含义 | 行为 |
|--------|------|------|
| `ready` | 完整明文 token 可用 | 可启用, 可设为默认, 可显示明文 |
| `masked-pending` | 上游返回脱敏 token (如 sk-***123) | 强制 `enabled=false`, `isDefault=false`; 不可设为默认; 不可 enable; GET value 返回 409 |

**解析**: `isMaskedTokenValue(token)` 检查 token 是否包含 `***` 模式
**修复**: 用户通过 update token 提供完整明文后, `valueStatus` 自动切换为 `ready`, 此时可以 enable 和设为默认

### Token 删除: Upstream-First 策略

1. 跳过上游删除的条件 (任一满足):
   - Token 是 `masked-pending` 状态
   - Site status 为 `disabled`
   - Account `accessToken` 为空
   - 平台 adapter 不存在
2. 调用 `adapter.deleteApiToken(url, accessToken, tokenValue, platformUserId)`
3. 如果上游删除失败: **中止本地删除**, 返回错误
4. 如果上游删除成功: 本地删除 + `repairDefaultToken` (如果被删的是默认 token)

### Token 创建: 双路径

**Local 路径** (请求体含 `token` 值):
1. 验证 token 值, 判断 valueStatus (masked-pending vs ready)
2. 自动生成 name: 第一个 token 为 "default", 后续为 "token-{N}"
3. INSERT 行 (masked-pending 强制 enabled=false, isDefault=false)
4. 如果 ready && (isDefault || 第一个 enabled token || 没有默认 token): `setDefaultToken`
5. `refreshCoverageForAccounts`

**Upstream 路径** (请求体不含 `token` 值):
1. 验证 upstream 参数 (unlimitedQuota, remainQuota, expiredTime, allowIps, modelLimitsEnabled, modelLimits)
2. 如果 unlimitedQuota=false 且 remainQuota 未提供: reject
3. `adapter.createApiToken(url, accessToken, platformUserId, params)` -- 在目标站点创建 token
4. `executeAccountTokenSync(row)` -- 同步所有上游 token
5. `refreshCoverageForAccounts`

### Account GET: 快照缓存架构

`GET /api/accounts` 使用 `getAccountsSnapshot`:
- 维护内存缓存 (TTL-based)
- `?refresh=true` 或 `?refresh=1` 强制刷新缓存
- 响应 header: `x-accounts-snapshot-cache: hit` 或 `miss`
- 响应体包含 `generatedAt` (ISO timestamp), `accounts`, `sites`
- `sites` 字段随 accounts 一起返回, 供前端做名称解析

### API Endpoints 嵌入管理

- `apiEndpoints` 是 site 的**嵌入字段**, 不是独立 REST 资源
- 在 POST/PUT site 时通过 `apiEndpoints` 数组传入
- Create: 与 site 在同一事务中 INSERT
- Update: full-replace 语义 -- 先 DELETE 所有旧行, 再 INSERT 新行 (在同一事务中)
- URL 通过 `normalizeSiteApiEndpointBaseUrl` 规范化
- 数组中不允许重复 URL
- 加载 site 时通过 `attachSiteApiEndpoints` 自动附加 (按 sortOrder, id 排序)

### Sub2API Managed Auth (extraConfig.sub2apiAuth)

用于 sub2api 平台的 OAuth token 管理, 存储在 `extraConfig.sub2apiAuth`:
```json
{
  "sub2apiAuth": {
    "refreshToken": "rt_xxx",
    "tokenExpiresAt": 1712345678
  }
}
```
- rebind-session 和 account update 时检查平台是否为 `sub2api`
- 合并逻辑: 如果请求体中提供了新值则用新值, 否则保留旧值
- `normalizeManagedRefreshToken`: 非空 string, trimmed
- `normalizeManagedTokenExpiresAt`: 正数 epoch 秒 (number or numeric string)

### Batch 操作汇总

| 资源 | 支持的操作 |
|------|-----------|
| Sites | `enable`, `disable`, `delete`, `enableSystemProxy`, `disableSystemProxy` |
| Accounts | `enable`, `disable`, `delete`, `refreshBalance` |
| AccountTokens | `enable`, `disable`, `delete` |

批量操作通用模式:
- 遍历 ids, 逐个处理
- 返回 `{ success: true, successIds: [], failedItems: [{ id, message }] }`
- Site batch 在循环内对 enable/disable 调用 `applySiteStatusSideEffects`
- Account batch 在 delete 后调用 `rebuildRoutesBestEffort` (整个 batch 只调用一次)
- AccountToken batch 对 masked-pending token 的 enable/disable 返回特定错误

### Site Probe (模型探测)

**POST /api/sites/:id/probe-now** (JSON 响应):
- Body: `{ scope?: "all"|"single", modelName?: string, latencyThresholdMs?: number }`
- `scope` 默认 undefined (探测所有)
- 调用 `probeSiteModels(id, { scope, modelName, latencyThresholdMs })`
- 失败返回 422

**GET /api/sites/:id/probe-stream** (SSE 流式):
- Query params: `scope`, `modelName`, `latencyThresholdMs`
- 设置 SSE headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
- SSE 事件类型:
  - `probe-start`: `{ totalModels, startedAt }`
  - `probe-model-checked`: `{ modelName, checkedAt }`
  - `probe-model-result`: `{ modelName, available, latencyMs, error? }`
  - `complete`: 最终结果 summary
  - `error`: `{ message }`
- 客户端断开: 通过 `AbortController` 传播给 `probeSiteModels` worker pool
- 使用 `reply.hijack()` + `reply.raw` 直接写入

### Account Health Refresh

**POST /api/accounts/health/refresh**:
- `wait=true`: 同步执行, 返回结果
- `wait=false` (或未设置): 后台任务, 返回 202 `{ queued: true, jobId, status }`
- 后台任务有 dedupe key: `refresh-account-runtime-health-{id}` 或 `refresh-all-account-runtime-health`
- `accountId` 可选: 指定则刷新单个, 不指定则刷新全部

**Runtime Health 状态**:
- `healthy`, `unhealthy`, `degraded`, `disabled`, `unknown`
- Disabled 账号或 disabled site 下的账号: 状态 `disabled`, status `skipped`
- proxyOnly (OAuth) 账号: status `skipped` (不做健康检查)
- 健康检查通过调用 `refreshBalance` 实现
- 超时 10s

### Account Expired API Key Recovery

当 update 一个状态为 `expired` 的 API key 账号时:
- 条件: `account.status === "expired" && nextCredentialMode === "apikey" && nextStatus !== "disabled" && needsModelRefresh`
- 行为: 调用 `applyAccountUpdateWorkflow` 时传入:
  - `preserveExpiredStatus: true`
  - `allowInactiveModelRefresh: true`
  - `reactivateAfterSuccessfulModelRefresh: true`
- 模型刷新成功后自动将 status 从 `expired` 恢复为 `active`

### API Key Connection 限制

当 `isApiKeyConnection(account)` 返回 true 时 (credentialMode=apikey 且 accessToken 为空):
- 不可创建上游 accountToken (POST without token value)
- 不可管理/同步 accountTokens (batch, sync, groups, value retrieval)
- 运行时健康检查中标记为 `proxyOnly: true` (跳过)

### Account Capabilities

```go
type AccountCapabilities struct {
    CanCheckin        bool // session capable 才能签到
    CanRefreshBalance bool // session capable 才能刷新余额
    ProxyOnly         bool // OAuth 或纯 apikey 只做代理
}
```

基于 `credentialMode` 和 `hasSessionToken`:
- `session` mode: `canCheckin = hasSessionToken`, `canRefreshBalance = hasSessionToken`
- `apikey` mode: 两项均为 false
- `auto` mode: 等效于 session (如果有 session token) 或 apikey
- OAuth provider 账号: `proxyOnly=true`, 两项均为 false

### SSE Probe-Stream Endpoint 详细规范

```
GET /api/sites/:id/probe-stream?scope=all&modelName=gpt-4&latencyThresholdMs=5000
```

**请求参数** (全部可选, query string):
| 参数 | 类型 | 说明 |
|------|------|------|
| `scope` | string | `"all"` 或 `"single"`, 其他值忽略 |
| `modelName` | string | 单个模型名 (trimmed), scope=single 时生效 |
| `latencyThresholdMs` | string | 正整数毫秒, 解析为 int |

**SSE 流事件**:

1. `probe-start`:
```json
{"type":"probe-start","totalModels":15,"startedAt":"2026-07-04T00:00:00.000Z"}
```

2. `probe-model-checked` (每个模型探测前):
```json
{"type":"probe-model-checked","modelName":"gpt-4","checkedAt":"2026-07-04T00:00:01.000Z"}
```

3. `probe-model-result` (每个模型探测后):
```json
{"type":"probe-model-result","modelName":"gpt-4","available":true,"latencyMs":320}
```

4. `complete` (所有探测完成):
```json
{"type":"complete","totalModels":15,"available":12,"unavailable":3,"...":"..."}
```

5. `error` (验证失败或异常):
```json
{"type":"error","message":"Invalid site id"}
```

**客户端断开处理**: 监听 `reply.raw` 的 `close` 事件, 通过 `AbortController.abort()` 传播给 `probeSiteModels`.

### Token Sync 流程 (sync/sync-all)

**单个账号同步** (`executeAccountTokenSync`):
1. 检查 site 是否 disabled -> skip (reason: site_disabled)
2. 检查是否为 apikey connection -> skip (reason: apikey_connection)
3. 如果没有 accessToken 但有 apiToken -> 尝试 convergeAccountMutation 恢复 legacy default token
4. 调用 `adapter.getApiTokens(url, accessToken, platformUserId)`, 超时 15s
5. 如果返回空: 尝试 `adapter.getApiToken` 作为 fallback
6. 调用 `convergeAccountMutation` 同步 tokens
7. 创建 events 行记录同步结果

**全量同步** (`executeSyncAllAccountTokens`):
- 仅同步 `status='active'` 的账号
- 批量处理: batch size = 3
- 同步完成后调用 `refreshCoverageForAccounts` 刷新已同步账号的 coverage
- 如果没有提供 `wait=true`, 作为后台任务运行 (dedupe key: `sync-all-account-tokens`)

### Model Availability 覆盖刷新

Token 创建/同步后自动调用 `refreshCoverageForAccounts`:
- 对每个账号调用 `refreshModelsForAccount`
- 批量大小 = 3
- 刷新完成后调用 `rebuildRoutesOnly` 重建路由
- 单个账号失败不影响整体 (Promise.allSettled)

### 平台检测 (POST /api/sites/detect)

1. Payload: `{ url: "https://api.openai.com" }` (required, non-empty trimmed string)
2. 调用 `detectSite(url)` -- 返回 `{ platform, initializationPresetId }` 或 null
3. 返回检测结果或 `{ error: "Could not detect platform" }`

### 可用模型查询 (GET /api/sites/:id/available-models)

合并两个来源:
1. `model_availability` (account-level): `available=true` 的模型, 通过 accounts.siteId 过滤
2. `token_model_availability` (token-level): `available=true` 的模型, 通过 accountTokens.accountId -> accounts.siteId 过滤
3. 去重 + 排序 (localeCompare, case-insensitive)

### 账号模型查询 (GET /api/accounts/:id/models)

1. 查询该账号的 `model_availability` 行 (含 latencyMs, isManual)
2. 查询该 site 的 `siteDisabledModels` 行
3. 仅返回 `available=true` 的模型
4. 每个模型标记 `disabled` (是否在 siteDisabledModels 中) 和 `isManual`
5. 响应: `{ siteId, siteName, models: [{ name, latencyMs, disabled, isManual }], totalCount, disabledCount }`

### Payload Validation 错误消息

- **Zod 层 (结构校验)**: 英文 -- e.g. "Invalid siteId. Expected positive number.", "Invalid name. Expected non-empty string."
- **Handler 层 (业务校验)**: 中文 -- e.g. "账号 ID 无效", "令牌不存在", "站点已禁用，无法创建令牌"
- **Go 实现**: `validate` tags 的错误消息应为英文 (对应 Zod 层), handler 层业务校验用中文

### Config Key 映射

| TS (camelCase) | Go (PascalCase) | 用途 |
|----------------|-----------------|------|
| `config.accountCredentialSecret` | `config.AccountCredentialSecret` | AES-256-GCM 密钥 |
| `config.authToken` | `config.AuthToken` | fallback 密钥 (当 accountCredentialSecret 为空时) |

## Acceptance Criteria
- [ ] 全部 34 端点实现并返回与 TS 版一致的 JSON 格式
- [ ] `accessToken` 列以**明文**存储; `autoRelogin.password` 使用 **AES-256-GCM** 加密 (`v1:iv:tag:ciphertext` base64url 格式)
- [ ] Payload 结构校验错误消息为英文; 业务校验错误消息为中文 (与 TS 一致)
- [ ] `POST /api/sites/detect` 正确识别平台类型
- [ ] 创建账号后自动调用 `convergeAccountMutation` (ensureDefaultToken + syncTokens + refreshBalance + refreshModels + rebuildRoutes, continueOnError=true)
- [ ] 级联删除: DB FOREIGN KEY ON DELETE CASCADE (site -> accounts -> accountTokens/modelAvailability)
- [ ] Site delete 后 invalidate caches; Account delete 后 rebuildRoutesBestEffort
- [ ] `batch` 端点支持完整操作集: Sites (5), Accounts (4), AccountTokens (3)
- [ ] `apiEndpoints` 作为 site 嵌入字段管理 (full-replace on update), 非独立 REST 资源
- [ ] SSE probe-stream 端点正确处理 client disconnect (AbortController)
- [ ] AccountTokens value status: masked-pending -> 强制 disabled + 不可默认; ready -> 正常
- [ ] Token delete: upstream-first (upstream 失败则中止本地删除)
- [ ] Account login: 自动检测 reusedAccount; `password` 加密为 autoRelogin.passwordCipher
- [ ] Account GET: 快照缓存 (`?refresh` 参数, `x-accounts-snapshot-cache` header, `generatedAt`)
- [ ] Account health refresh: wait=true 同步 / wait=false 后台任务 202; 支持单账号和全量
- [ ] Sub2API managed auth: `extraConfig.sub2apiAuth` 通过 rebind-session 和 account update flow
- [ ] Expired API key account recovery: allowInactive model refresh + reactivate

## Test Plan
| 文件 | 内容 |
|------|------|
| `handler/admin/sites_test.go` | Sites CRUD + detect + disabled-models + available-models + probe-now + probe-stream + batch(含 enableSystemProxy/disableSystemProxy) + apiEndpoints 嵌入 |
| `handler/admin/accounts_test.go` | Accounts CRUD + login + verify-token (含 shield-blocked/needs-user-id/invalid-user-id) + rebind-session + batch(含 refreshBalance) + health-refresh(wait/background) + models/manual |
| `handler/admin/account_tokens_test.go` | AccountTokens CRUD + batch + sync/sync-all(wait/background) + groups + default + value + 双路径(upstream/local) |
| `service/site_service_test.go` | 平台检测逻辑, api endpoint URL 规范化 |
| `service/account_credential_test.go` | AES-256-GCM 加解密 roundtrip; 密钥推导; 格式兼容性; 解密失败静默 |
| `service/account_mutation_test.go` | convergeAccountMutation 全流程; ensureDefaultToken; syncTokensFromUpstream; continueOnError 行为 |

## Edge Cases
- `credentialMode=apikey` 的账号不能签到 (`canCheckin=false`); OAuth provider 账号 `proxyOnly=true`
- `accessToken` 为空但 `accessTokens` 非空 -> 有效, 批量 API key import
- `requestedTokens.length === 0` (accessToken 空且 accessTokens 空) -> reject "请填写 Token"
- 重复 (url, platform) 的 site -> 409 `"A {platform} site with URL {url} already exists."`
- 删除 site 后重建同名 site -> 新 id, 旧 channels 不受影响
- `skipModelFetch=true` -> 创建账号时不自动获取模型, 但仍执行 `convergeAccountMutation` (除 refreshModels 外的其他步骤)
- Site status 变更: disable -> 所有 accounts 置 disabled; enable -> 仅恢复之前被 disable 的 accounts
- API key connection: 不可创建上游 token, 不可管理/同步 tokens, 健康检查跳过
- Token masked-pending: 强制 enabled=false + isDefault=false; batch enable/disable 跳过并报错; 不可设为默认; GET value 返回 409
- Token update 提供新 masked 值: valueStatus 重新设为 masked-pending; 提供明文值: valueStatus 恢复为 ready
- Default token 被 disable 或删除: 自动调用 `repairDefaultToken` 寻找替代
- SSE probe-stream: 客户端断开 (close 事件) -> AbortController 传播停止
- Account login: 同 (siteId, username) 已存在 -> UPDATE 而非 INSERT; 返回 `reusedAccount: true`
- `expiredTime` 接受 Unix 时间戳 (number) 或 ISO 日期字符串; 统一解析为 epoch 秒
- `remainQuota`: 当 unlimitedQuota=false 时必须提供 (正整数); 否则 reject
- Batch 操作: 部分成功/部分失败, 返回 `successIds` + `failedItems`
- Account token sync: disabled site -> skip; apikey connection -> skip; 无 upstream tokens -> skip; getApiToken fallback
- Health refresh: disabled account/site -> state=disabled status=skipped; proxyOnly -> status=skipped
