# P3: Sites + Accounts + AccountTokens CRUD API

**S.U.P.E.R**: S (单一职责) · P (端口优先) | **依赖**: P1 + P2 | **Size**: L

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\routes\api\sites.ts` (含 api-endpoints + disabled-models 子路由)
- `D:\Code\TokenDance\metapi\src\server\routes\api\accounts.ts` (含 login/verifyToken/rebind-session/balance/models)
- `D:\Code\TokenDance\metapi\src\server\routes\api\accountTokens.ts` (含 sync/groups/default)
- `D:\Code\TokenDance\metapi\src\server\contracts\siteRoutePayloads.ts` — Zod schemas
- `D:\Code\TokenDance\metapi\src\server\contracts\accountsRoutePayloads.ts` — Zod schemas
- `D:\Code\TokenDance\metapi\src\server\contracts\accountTokensRoutePayloads.ts` — Zod schemas
- `D:\Code\TokenDance\metapi\src\server\services\siteDetector.ts`
- `D:\Code\TokenDance\metapi\src\server\services\siteApiEndpointService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\accountCredentialService.ts` — AES 加密
- `D:\Code\TokenDance\metapi\src\server\services\accountMutationWorkflow.ts`
- `D:\Code\TokenDance\metapi\src\server\services\accountTokenService.ts`

## Go 模块结构
```
handler/admin/
  sites.go                 # Sites CRUD 端点
  sites_endpoints.go       # Site API Endpoints 子资源
  sites_disabled_models.go # Site Disabled Models 子资源
  sites_detect.go          # POST /api/sites/detect
  accounts.go              # Accounts CRUD 端点
  accounts_login.go        # 登录/验证/rebind
  accounts_balance.go      # 余额刷新
  accounts_models.go       # 模型查询/手动设置
  account_tokens.go        # AccountTokens CRUD 端点
  account_tokens_sync.go   # 同步端点
  account_tokens_groups.go # 分组端点
  payloads/
    sites.go               # Create/Update/Batch/Detect payload structs + validation
    accounts.go            # Create/Update/Login/Rebind/VerifyToken payloads
    account_tokens.go      # Create/Update/Batch/SyncAll payloads
service/
  site_service.go          # 站点业务逻辑
  site_detect.go           # 平台检测逻辑
  site_endpoint_service.go # API 端点管理
  account_service.go       # 账号业务逻辑
  account_credential.go    # AES 加密/解密 accessToken
  account_mutation.go      # 账号创建工作流
  account_token_service.go # Token CRUD + 同步
```

## API 端点清单

### Sites (9 端点)
| Method | Path | Payload | 说明 |
|--------|------|---------|------|
| GET | `/api/sites` | — | 列表, 支持 `?refresh` |
| POST | `/api/sites` | `SiteCreatePayload` | 创建站点 |
| PUT | `/api/sites/:id` | `SiteUpdatePayload` | 更新站点 |
| DELETE | `/api/sites/:id` | — | 级联删除 |
| POST | `/api/sites/batch` | `SiteBatchPayload` | 批量操作 |
| POST | `/api/sites/detect` | `SiteDetectPayload{url}` | 检测平台类型 |
| GET | `/api/sites/:id/disabled-models` | — | 获取禁用模型列表 |
| PUT | `/api/sites/:id/disabled-models` | `SiteDisabledModelsPayload` | 设置禁用模型 |
| GET | `/api/sites/:id/available-models` | — | 获取可用模型 |
| POST | `/api/sites/:id/probe-now` | — | 立即探测模型 |

### Accounts (11 端点)
| Method | Path | Payload | 说明 |
|--------|------|---------|------|
| GET | `/api/accounts` | — | 列表, `?refresh` |
| POST | `/api/accounts` | `AccountCreatePayload` | 创建账号 |
| POST | `/api/accounts/login` | `AccountLoginPayload` | 登录获取 session |
| POST | `/api/accounts/verify-token` | `AccountVerifyTokenPayload` | 验证 token 有效性 |
| POST | `/api/accounts/:id/rebind-session` | `AccountRebindSessionPayload` | 重新绑定 session |
| PUT | `/api/accounts/:id` | `AccountUpdatePayload` | 更新账号 |
| DELETE | `/api/accounts/:id` | — | 删除账号 |
| POST | `/api/accounts/batch` | `AccountBatchPayload` | 批量操作 |
| POST | `/api/accounts/health/refresh` | `AccountHealthRefreshPayload` | 健康检查 |
| POST | `/api/accounts/:id/balance` | — | 刷新余额 |
| GET | `/api/accounts/:id/models` | — | 获取模型列表 |
| POST | `/api/accounts/:id/models/manual` | `AccountManualModelsPayload` | 手动设置模型 |

### AccountTokens (11 端点)
| Method | Path | Payload | 说明 |
|--------|------|---------|------|
| GET | `/api/account-tokens` | — | 列表, `?accountId` |
| POST | `/api/account-tokens` | `AccountTokenCreatePayload` | 创建 token |
| POST | `/api/account-tokens/batch` | `AccountTokenBatchPayload` | 批量操作 |
| PUT | `/api/account-tokens/:id` | `AccountTokenUpdatePayload` | 更新 token |
| POST | `/api/account-tokens/:id/default` | — | 设为默认 |
| GET | `/api/account-tokens/:id/value` | — | 获取 token 值 (解密) |
| DELETE | `/api/account-tokens/:id` | — | 删除 token |
| POST | `/api/account-tokens/sync/:accountId` | — | 同步单个账号 token |
| POST | `/api/account-tokens/sync-all` | `AccountTokenSyncAllPayload` | 全量同步 |
| GET | `/api/account-tokens/groups/:accountId` | — | 按分组查看 |
| GET | `/api/account-tokens/account/:id/default` | — | 获取默认 token |

### Payload Validation
Go struct 使用 `validate` tags (go-playground/validator), 等效 Zod:
```go
type AccountCreatePayload struct {
    SiteID           int64  `json:"siteId" validate:"required"`
    Username         string `json:"username"`
    AccessToken      string `json:"accessToken" validate:"required"`
    APIToken         string `json:"apiToken"`
    CredentialMode   string `json:"credentialMode" validate:"omitempty,oneof=auto session apikey"`
    CheckinEnabled   *bool  `json:"checkinEnabled"`
    SkipModelFetch   bool   `json:"skipModelFetch"`
    RefreshToken     string `json:"refreshToken"`
    TokenExpiresAt   string `json:"tokenExpiresAt"`
}
```

### 关键业务逻辑
- **Credential 加密**: `accessToken` 使用 AES-256-CBC 加密存储, 密钥 = `config.AccountCredentialSecret`
- **Site detect**: POST 前先调 `/api/sites/detect` 获取 `platform`, 禁止用户手动指定
- **Account 创建**: 先调用平台适配器 `verifyToken` → 获取 `getBalance` → `getModels` → 写入 accounts + model_availability + 触发路由重建
- **Site 删除**: 级联删除 accounts/endpoints/disabledModels/channels
- **Credential mode**: `auto` (自动判断) / `session` (session cookie) / `apikey` (纯 API key)

## Acceptance Criteria
- [ ] 全部 31 端点实现并返回与 TS 版一致的 JSON 格式
- [ ] Payload validation 错误消息包含中文 (与 Zod 一致)
- [ ] `accessToken` AES 加密/解密正确
- [ ] `POST /api/sites/detect` 正确识别平台类型
- [ ] 创建账号后自动刷新模型列表 + 重建路由
- [ ] 级联删除: 删 site → 关联数据一起删
- [ ] `batch` 端点支持批量启用/禁用/删除
- [ ] CRUD 端点返回符合现有前端期望的数据结构

## Test Plan
| 文件 | 内容 |
|------|------|
| `handler/admin/sites_test.go` | CRUD + detect + batch + disabled-models |
| `handler/admin/accounts_test.go` | CRUD + login + verify + rebind + batch |
| `handler/admin/account_tokens_test.go` | CRUD + batch + sync + groups |
| `service/site_service_test.go` | 检测逻辑 |
| `service/account_credential_test.go` | AES 加解密 roundtrip |
| `service/account_mutation_test.go` | 创建工作流 |

## Edge Cases
- `credentialMode=apikey` 的账号不能签到
- `accessToken` 为空字符串 → 拒绝 (不可以空)
- 重复 URL+platform 的 site → 409 或 UNIQUE constraint error
- 删除 site 后重建同名 site → 新 id, 旧 channels 不受影响
- `SkipModelFetch=true` → 创建账号时不自动获取模型
