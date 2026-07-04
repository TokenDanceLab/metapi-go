# P6: OAuth Subsystem (4 Providers + Route Units)

**S.U.P.E.R**: S (单一职责) · P (端口优先) · R (可替换) | **依赖**: P3 + P4 | **Size**: L

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\services\oauth\providers.ts` — provider 注册表
- `D:\Code\TokenDance\metapi\src\server\services\oauth\service.ts` — 流程编排器
- `D:\Code\TokenDance\metapi\src\server\services\oauth\codexProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\claudeProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\geminiCliProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\antigravityProvider.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\sessionStore.ts` — 内存 session + PKCE
- `D:\Code\TokenDance\metapi\src\server\services\oauth\localCallbackServer.ts` — loopback HTTP
- `D:\Code\TokenDance\metapi\src\server\services\oauth\refreshSingleflight.ts` — lazy refresh
- `D:\Code\TokenDance\metapi\src\server\services\oauth\routeUnitService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\oauth\quota.ts`

## Go 模块结构
```
service/oauth/
  provider.go            # OAuthProviderDefinition 接口
  registry.go            # 4 个 provider 注册表
  codex.go               # Codex OAuth 实现
  claude.go              # Claude OAuth 实现
  gemini_cli.go          # Gemini CLI OAuth 实现
  antigravity.go         # Antigravity OAuth 实现
  flow.go                # StartFlow + HandleCallback 编排
  session.go             # 内存 session store + PKCE code verifier
  callback_server.go     # 本地 loopback HTTP 回调服务器
  refresh.go             # Lazy token refresh + singleflight 去重
  connection.go          # 连接管理 (list/delete/rebind/proxy)
  import.go              # 从 native JSON 批量导入 OAuth 连接
  quota.go               # Quota snapshot 刷新
  account.go             # OAuth 账号信息序列化到 extraConfig
  route_unit.go          # OAuth route unit (round_robin / stick_until_unavailable)
  headers.go             # buildProxyHeaders (代理路径用的 OAuth header)
```

## 功能规格

### Provider Interface
```go
type OAuthProviderDefinition struct {
    ID       string // "codex" | "claude" | "gemini-cli" | "antigravity"
    Metadata ProviderMetadata
    Site     ProviderSiteConfig
    Loopback LoopbackConfig

    BuildAuthorizationURL  func(state, codeVerifier string) (string, error)
    ExchangeAuthorizationCode func(ctx, code, codeVerifier string, proxy *ProxyConfig) (*TokenSet, error)
    RefreshAccessToken     func(ctx, refreshToken string, proxy *ProxyConfig) (*TokenSet, error)
    BuildProxyHeaders      func(ctx, accessToken string) (http.Header, error) // optional
}
```

### OAuth Flow
```
StartFlow(provider, requestOrigin):
  1. 验证 provider 已注册
  2. 确保 loopback callback server 已启动
  3. 生成 PKCE code_verifier + code_challenge (SHA256)
  4. 创建 session: {id, provider, codeVerifier, createdAt, ...}
  5. 调用 provider.BuildAuthorizationURL(state, codeChallenge)
  6. 返回 {state, authorizationUrl, instructions}
     - 如果 requestOrigin 不是 loopback → 含 SSH tunnel 提示

HandleCallback(provider, code, state):
  1. 验证 session 存在且未过期 (10 min)
  2. 验证 state 匹配
  3. 解析代理配置
  4. provider.ExchangeAuthorizationCode(code, codeVerifier, proxy)
  5. upsertOauthAccount: 按 provider+accountKey+projectId 查找或创建账号
     - 加密 tokens 写入 extraConfig.oauth
     - credentialMode = "session"
  6. refreshModelsForAccount → rebuildRoutesOnly
  7. 标记 session 已完成
  8. 失败时 full rollback (删除已创建的账号)
```

### Token Refresh (Lazy Singleflight)
```
RefreshAccessToken(accountID):
  1. 检查 singleflight map: 同一 accountID 的并发刷新合并为一次
  2. 从 extraConfig.oauth 解密 refreshToken
  3. provider.RefreshAccessToken(refreshToken, proxy)
  4. 更新 extraConfig.oauth (加密新 tokens)
  5. 如果刷新失败 → 标记账号 status=expired
```

### OAuth Route Units
```
RouteUnit:
  - 策略 round_robin: 轮询 member 列表
  - 策略 stick_until_unavailable: 固定第一个可用 member, 直到失败再切换
  - member 成功/失败计数, 延迟总计, 冷却管理
```

## Acceptance Criteria
- [ ] 4 个 provider OAuth 流程完整: start → callback → persist
- [ ] PKCE code_verifier/code_challenge SHA256 正确
- [ ] Loopback callback server 在配置端口上监听
- [ ] Session 10 分钟过期
- [ ] Token refresh singleflight 去重生效
- [ ] OAuth 账号按 provider+accountKey+projectId 唯一去重
- [ ] Route unit round_robin 轮询正确
- [ ] Route unit stick_until_unavailable 故障转移正确
- [ ] Quota snapshot 批量刷新 (concurrency=4)
- [ ] 从 native JSON 导入 OAuth 连接 (≤100 条, 拒绝 sub2api envelope)

## Test Plan
| 文件 | 内容 |
|------|------|
| `service/oauth/provider_test.go` | 接口合规, registry |
| `service/oauth/flow_test.go` | 完整 flow mock |
| `service/oauth/session_test.go` | PKCE 生成, 过期 |
| `service/oauth/refresh_test.go` | singleflight 去重 |
| `service/oauth/route_unit_test.go` | round_robin + stick |
| `service/oauth/*_test.go` | 各 provider 的 OAuth URL 格式 |

## Edge Cases
- Loopback port 被占用 → 启动失败, 提示用户
- OAuth 回调 code 已使用 → 拒绝重复使用
- Refresh token 已过期 → 标记账号 expired
- 并发刷新同一账号 → singleflight 合并为一次上游调用
- 导入 JSON 含 sub2api envelope → 拒绝, 不是静默跳过
- SSH tunnel 场景 → 回调 URL 提示正确
