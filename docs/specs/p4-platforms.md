# P4: Platform Adapters (14 platforms)

**S.U.P.E.R**: S (单一职责) · U (单向流) · R (可替换) | **依赖**: P3 (site/account service) | **Size**: XL

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\services\platforms\index.ts` — 注册表
- `D:\Code\TokenDance\metapi\src\server\services\platforms\base.ts` — `BasePlatformAdapter` + `StandardApiProviderAdapterBase`
- `D:\Code\TokenDance\metapi\src\server\services\platforms\newApi.ts` — 最完整的 fork 实现
- `D:\Code\TokenDance\metapi\src\server\services\platforms\oneApi.ts` → oneHub.ts → doneHub.ts (继承链)
- `D:\Code\TokenDance\metapi\src\server\services\platforms\sub2api.ts` — 最复杂 (870 LOC, subscription summary)
- `D:\Code\TokenDance\metapi\src\server\services\platforms\openai.ts` / `claude.ts` / `codex.ts` / `gemini.ts` / `geminiCli.ts` / `antigravity.ts` / `cliproxyapi.ts` / `anyrouter.ts` / `veloera.ts`

## Go 模块结构
```
platform/
  adapter.go           # PlatformAdapter 接口定义
  base.go              # BaseAdapter: 默认 NewApi 式实现 (login/getUserInfo/verifyToken)
  standard.go          # StandardAPIProvider: OpenAI/Claude/Gemini 标准 API 提供商
  registry.go          # 注册表: map[string]PlatformAdapter + detect 逻辑
  newapi.go            # NewApiAdapter (最完整 fork)
  oneapi.go            # OneApiAdapter (base for one-hub/done-hub)
  onehub.go            # OneHubAdapter (title-first detect)
  donehub.go           # DoneHubAdapter (title-first detect)
  sub2api.go           # Sub2ApiAdapter (subscription summary + managed auth)
  veloera.go           # VeloeraAdapter (platformUserId checkin)
  openai.go            # OpenAiAdapter (API-key only, /v1/models)
  claude.go            # ClaudeAdapter (OAuth-driven, /v1/models)
  codex.go             # CodexAdapter (OAuth-driven, empty models)
  gemini.go            # GeminiAdapter (Gemini format models)
  gemini_cli.go        # GeminiCliAdapter (extends Gemini)
  antigravity.go       # AntigravityAdapter (OAuth-driven)
  cliproxyapi.go       # CliProxyApiAdapter
  anyrouter.go         # AnyRouterAdapter (extends NewApi, Cloudflare challenge)
  detect.go            # detectPlatform(url) — URL hint → title hint → sequential probe
  title_hint.go        # detectPlatformByTitle
  site_proxy.go        # HTTP client with SOCKS/HTTP proxy + DNS/TLS 控制
```

## PlatformAdapter 接口
```go
type PlatformAdapter interface {
    PlatformName() string

    // Detection
    Detect(ctx context.Context, url string) (bool, error)

    // Session management
    Login(ctx context.Context, url string, username string, password string, proxy *ProxyConfig) (*LoginResult, error)
    GetUserInfo(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) (*UserInfo, error)
    VerifyToken(ctx context.Context, url string, accessToken string, mode CredentialMode, proxy *ProxyConfig) (*VerifyTokenResult, error)

    // Daily operations
    Checkin(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) (*CheckinResult, error)
    GetBalance(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) (*BalanceResult, error)

    // Model discovery
    GetModels(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) ([]ModelInfo, error)

    // Token management (NewAPI-style platforms)
    GetAPIToken(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) (*APIToken, error)
    GetAPITokens(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) ([]APIToken, error)
    CreateAPIToken(ctx context.Context, url string, accessToken string, name string, proxy *ProxyConfig) (*APIToken, error)
    DeleteAPIToken(ctx context.Context, url string, accessToken string, tokenID string, proxy *ProxyConfig) error

    // Announcements
    GetSiteAnnouncements(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) ([]Announcement, error)
    GetUserGroups(ctx context.Context, url string, accessToken string, proxy *ProxyConfig) ([]UserGroup, error)
}
```

## 适配器能力矩阵 (14 平台 × 方法)

| Adapter | detect | login | checkin | balance | models | api-token CRUD | groups | announcements |
|---------|--------|-------|---------|---------|--------|----------------|--------|---------------|
| NewApi | ✅ URL | ✅ override | ✅ | ✅ | ✅ | ✅ create/delete | ✅ | ✅ |
| OneApi | ✅ URL | ✅ base | ✅ | ✅ | ✅ | ✅ create/delete/get | ✅ | ❌ |
| OneHub | ✅ title-first | ✅ inherited | ✅ | ✅ | ✅ | ✅ inherited | ✅ | ✅ inherited |
| DoneHub | ✅ title-first | ✅ inherited | ✅ | ✅ | ✅ | ✅ inherited | ✅ | ✅ inherited |
| Veloera | ✅ title-first | ✅ base | ✅ platformUserId | ✅ | ✅ | ❌ | ❌ | ❌ |
| Sub2Api | ✅ title-first | ✅ override | ❌ no-op | ✅ | ✅ | ✅ create/delete | ✅ | ✅ |
| AnyRouter | ✅ title-first | ✅ inherited | ✅ | ✅ | ✅ | ✅ inherited | ✅ | ✅ |
| OpenAI | ✅ | ❌ | ❌ | ❌ (0) | `/v1/models` | ❌ | ❌ | ❌ |
| Claude | ✅ | ❌ | ❌ | ❌ (0) | ✅ | ❌ | ❌ | ❌ |
| Codex | ✅ | ❌ | no-op | 0 | `[]` | ❌ | ❌ | ❌ |
| Gemini | ✅ | ❌ | ❌ | ❌ (0) | ✅ Gemini fmt | ❌ | ❌ | ❌ |
| GeminiCli | ✅ inherited | ❌ | ❌ | ❌ | ✅ inherited | ❌ | ❌ | ❌ |
| Antigravity | ✅ | ❌ | no-op | 0 | ✅ | ❌ | ❌ | ❌ |
| CliProxyApi | ✅ | ❌ | ❌ | ❌ (0) | ✅ | ❌ | ❌ | ❌ |

## Platform Detection 流程
```
detectPlatform(url):
  1. 对已知 URL pattern 直接匹配 (domain/路径特征)
  2. 对 title-first 集合 {anyrouter, done-hub, one-hub, veloera, sub2api} 爬页面 <title>
  3. 按注册顺序依次调用 adapter.Detect(url) (HTTP GET 探测特征)
  4. 如果都失败 → 回退到 title hint
```

## Site Proxy (出站 HTTP 统一层)
```go
type SiteProxy struct {
    SystemProxyURL string
    client         *http.Client
}

func (p *SiteProxy) Do(ctx context.Context, req *http.Request, proxyConfig *ProxyConfig) (*http.Response, error)
```
- 支持 SOCKS5/HTTP 代理
- 支持 useSystemProxy: true → 使用 config.SystemProxyURL
- 支持 customHeaders: 站点级自定义 header 注入
- TLS 配置: InsecureSkipVerify 选项
- DNS: 可自定义 DNS resolver

## Acceptance Criteria
- [ ] 14 个适配器全部实现 PlatformAdapter 接口
- [ ] `detectPlatform(url)` 正确识别每个平台
- [ ] 继承链正确 (OneHub extends OneApi extends Base)
- [ ] StandardApiProvider 适配器 getBalance 返回 0, checkin 返回 unsupported
- [ ] Sub2Api subscription summary 解析正确
- [ ] SiteProxy SOCKS5/HTTP 代理工作
- [ ] 每个平台至少一个 happy-path 集成测试

## Test Plan
| 文件 | 内容 |
|------|------|
| `platform/registry_test.go` | detect 逻辑, 别名映射 |
| `platform/newapi_test.go` | login, checkin, balance, models, token CRUD |
| `platform/oneapi_test.go` | 继承行为 |
| `platform/sub2api_test.go` | subscription summary, managed auth |
| `platform/standard_test.go` | OpenAI/Claude/Gemini /v1/models |
| `platform/site_proxy_test.go` | SOCKS5, HTTP proxy, custom headers |
| `platform/detect_test.go` | URL hint, title hint, sequential probe |

## Edge Cases
- 平台返回非标准 HTTP 状态码 → 正确解析错误消息
- 平台响应超时 → ctx 超时返回
- sub2api managed auth 过期 → 自动 refresh
- AnyRouter Cloudflare challenge → 使用 __fixtures__ 中的 challenge 处理逻辑
- 代理不可达 → 返回明确代理错误 (不要误判为平台故障)
