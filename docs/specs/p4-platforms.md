# P4: Platform Adapters (14 platforms)

**S.U.P.E.R**: S (单一职责) · U (单向流) · R (可替换) | **依赖**: P3 (site/account service) | **Size**: XL

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\services\platforms\index.ts` — 注册表 + detectPlatform 流程
- `D:\Code\TokenDance\metapi\src\server\services\platforms\base.ts` — `PlatformAdapter` 接口 + `BasePlatformAdapter` + 全部类型定义
- `D:\Code\TokenDance\metapi\src\server\services\platforms\standardApiProvider.ts` — `StandardApiProviderAdapterBase` (OpenAI/Claude/Gemini 公共基类)
- `D:\Code\TokenDance\metapi\src\server\services\platforms\newApi.ts` — 最完整 fork 实现 (~1450 LOC, cookie fallback + shield challenge + user-ID probing)
- `D:\Code\TokenDance\metapi\src\server\services\platforms\oneApi.ts` → oneHub.ts → doneHub.ts (三层继承链)
- `D:\Code\TokenDance\metapi\src\server\services\platforms\sub2api.ts` — 最复杂非NewApi fork (~895 LOC, JWT auth + subscription summary)
- `D:\Code\TokenDance\metapi\src\server\services\platforms\openai.ts` / `claude.ts` / `codex.ts` / `gemini.ts` / `geminiCli.ts` / `antigravity.ts` / `cliproxyapi.ts` / `anyrouter.ts` / `veloera.ts`
- `D:\Code\TokenDance\metapi\src\server\services\platforms\titleHint.ts` — 基于页面 title 的平台识别

## Go 模块结构
```
platform/
  adapter.go           # PlatformAdapter 接口定义 + 所有共享类型
  base.go              # BaseAdapter: 默认实现 (login/getUserInfo/verifyToken)
  standard.go          # StandardAPIProvider: OpenAI/Claude/Gemini/CliProxyApi 公共基类
  registry.go          # 注册表: 适配器注册顺序 + detect 流程入口
  newapi.go            # NewApiAdapter (最完整 fork, 含 cookie fallback + shield challenge)
  oneapi.go            # OneApiAdapter (OneHub/DoneHub 的基类)
  onehub.go            # OneHubAdapter (title-first detect + /api/available_model fallback)
  donehub.go           # DoneHubAdapter (title-first detect + remaining-quota balance + /api/notice)
  sub2api.go           # Sub2ApiAdapter (JWT auth + subscription summary + managed auth)
  veloera.go           # VeloeraAdapter (1M divisor + Veloera-User header)
  openai.go            # OpenAiAdapter (API-key only, /v1/models)
  claude.go            # ClaudeAdapter (/anthropic suffix stripping + /v1/models fallback)
  codex.go             # CodexAdapter (OAuth-driven, all methods return empty/unsupported)
  gemini.go            # GeminiAdapter (3-path model discovery + openai compat)
  gemini_cli.go        # GeminiCliAdapter (extends Gemini, own detect for cloudcode-pa.googleapis.com)
  antigravity.go       # AntigravityAdapter (OAuth-driven, /v1internal:fetchAvailableModels)
  cliproxyapi.go       # CliProxyApiAdapter (port 8317 + x-cpa-* headers detect)
  anyrouter.go         # AnyRouterAdapter (extends NewApi, URL-keyword detect)
  detect.go            # detectPlatform(url) — 4-step pipeline
  title_hint.go        # detectPlatformByTitle — 页面 title 正则匹配 (7 种平台)
  site_proxy.go        # HTTP client with SOCKS/HTTP proxy + DNS/TLS 控制
```

## 继承链

```
BasePlatformAdapter (base.go)
├── StandardApiProviderAdapterBase (standardApiProvider.go)
│   ├── OpenAiAdapter       (openai.go)
│   ├── ClaudeAdapter       (claude.go)
│   ├── GeminiAdapter       (gemini.go)
│   │   └── GeminiCliAdapter (geminiCli.go)
│   └── CliProxyApiAdapter  (cliproxyapi.go)
├── CodexAdapter            (codex.go)
├── AntigravityAdapter      (antigravity.go)
├── OneApiAdapter           (oneApi.go)
│   └── OneHubAdapter       (oneHub.go)
│       └── DoneHubAdapter  (doneHub.go)
├── VeloeraAdapter          (veloera.go)
├── Sub2ApiAdapter          (sub2api.go)
└── NewApiAdapter           (newApi.go)
    └── AnyRouterAdapter    (anyrouter.go)
```

关键继承关系:
- **StandardApiProviderAdapterBase**: 提供统一的 `login=false`, `checkin=false`, `getBalance=0`, `getUserInfo=null` 默认值。子类只需实现 `detect()` 和 `getModels()`。
- **OneApi -> OneHub -> DoneHub**: 三层 NewApi fork 继承。OneHub 覆盖 `getModels`(加 /api/available_model fallback) 和 `getUserGroups`。DoneHub 覆盖 `checkin`(始终返回 unsupported)、`getBalance`(remaining-quota 语义)、`getSiteAnnouncements`(/api/notice)。
- **NewApi -> AnyRouter**: AnyRouter 继承 NewApi 全部 cookie/shield/user-ID 逻辑，仅覆盖 `detect()` 为纯 URL 关键词匹配。
- **Gemini -> GeminiCli**: GeminiCli 继承 Gemini 的模型发现逻辑，覆盖 `detect()` 为 `cloudcode-pa.googleapis.com` URL 匹配。
- **Veloera**: 直接继承 BasePlatformAdapter，不继承 NewApi。使用自己的 1,000,000 divisor 和 `Veloera-User` header。
- **Sub2Api**: 直接继承 BasePlatformAdapter。JWT auth，完全独立的 API 契约 (`{ code, message, data }` 信封)。

## PlatformAdapter 接口 (Go, 与 TS 对齐)

```go
// 类型定义
type CredentialMode int
const (
    CredentialAuto    CredentialMode = iota // 自动检测 (先 session 后 apikey)
    CredentialSession                       // 仅 session token
    CredentialAPIKey                        // 仅 API key
)

type CheckinResult struct {
    Success bool
    Message string
    Reward  string // 可选
}

type SubscriptionPlanSummary struct {
    ID               *int
    GroupID          *int
    GroupName        string
    Status           string
    ExpiresAt        string // ISO 8601
    DailyUsedUsd     *float64
    DailyLimitUsd    *float64
    WeeklyUsedUsd    *float64
    WeeklyLimitUsd   *float64
    MonthlyUsedUsd   *float64
    MonthlyLimitUsd  *float64
}

type SubscriptionSummary struct {
    ActiveCount   int
    TotalUsedUsd  float64
    Subscriptions []SubscriptionPlanSummary
}

type BalanceInfo struct {
    Balance              float64 // 当前余额 (USD)
    Used                 float64 // 已使用额度 (USD)
    Quota                float64 // 总额度 (USD)
    TodayIncome          *float64 // 今日收入 (USD)
    TodayQuotaConsumption *float64 // 今日消耗 (USD)
    SubscriptionSummary  *SubscriptionSummary // 仅 Sub2Api 填充
}

type LoginResult struct {
    Success     bool
    AccessToken string
    Username    string
    Message     string
}

type UserInfo struct {
    Username    string
    DisplayName string
    Email       string
    Role        *int
}

type TokenVerifyResult struct {
    TokenType string // "session", "apikey", "unknown"
    UserInfo  *UserInfo
    Balance   *BalanceInfo
    APIToken  string // 发现的首个 API key (可选)
    Models    []string
}

type ApiTokenInfo struct {
    Name       string
    Key        string
    Enabled    bool
    TokenGroup string // 可选
}

type SiteAnnouncement struct {
    SourceKey         string
    Title             string
    Content           string
    Level             string // "info", "warning", "error"
    SourceURL         string // 可选
    StartsAt          string // 可选, ISO 8601
    EndsAt            string // 可选, ISO 8601
    UpstreamCreatedAt string // 可选, ISO 8601
    UpstreamUpdatedAt string // 可选, ISO 8601
    RawPayload        json.RawMessage // 可选
}

type CreateAPITokenOptions struct {
    Name              string
    Group             string
    UnlimitedQuota    bool   // 默认 true
    RemainQuota       float64 // 默认 0
    ExpiredTime       int64  // Unix timestamp, -1 = 永不过期
    AllowIPs          string
    ModelLimitsEnabled bool
    ModelLimits       string
}

// PlatformAdapter 接口
type PlatformAdapter interface {
    PlatformName() string

    // Detection: 返回 true 如果 url 可以被此适配器处理
    Detect(ctx context.Context, url string) (bool, error)

    // Session management
    // platformUserId: 用于 NewApi-fork 平台 (NewApi/OneApi/OneHub/DoneHub/Veloera/AnyRouter) 的
    //   cookie-based 认证回退。如果调用方已知用户 ID 则传入；否则适配器内部自动探测。
    Login(ctx context.Context, url, username, password string, proxy *ProxyConfig) (*LoginResult, error)
    GetUserInfo(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*UserInfo, error)
    VerifyToken(ctx context.Context, url, token string, platformUserId *int, proxy *ProxyConfig) (*TokenVerifyResult, error)

    // Daily operations
    Checkin(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*CheckinResult, error)
    GetBalance(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*BalanceInfo, error)

    // Model discovery: 返回模型 ID 字符串列表 (如 ["gpt-4", "claude-3-opus-20240229"])
    GetModels(ctx context.Context, url, token string, platformUserId *int, proxy *ProxyConfig) ([]string, error)

    // Token management (NewAPI-style platforms)
    GetAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) (*string, error)
    GetAPITokens(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]ApiTokenInfo, error)
    // CreateAPIToken: options 包含 name/group/unlimitedQuota/remainQuota/expiredTime/allowIps/modelLimits* 等全部字段
    CreateAPIToken(ctx context.Context, url, accessToken string, platformUserId *int, options *CreateAPITokenOptions, proxy *ProxyConfig) (bool, error)
    // DeleteAPIToken: tokenKey 是 API key 字符串 (如 "sk-xxx..."), 非数据库 ID
    //   适配器内部通过 list→find key→extract numeric ID→DELETE 流程完成删除
    DeleteAPIToken(ctx context.Context, url, accessToken, tokenKey string, platformUserId *int, proxy *ProxyConfig) error

    // Announcements & groups
    GetSiteAnnouncements(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]SiteAnnouncement, error)
    GetUserGroups(ctx context.Context, url, accessToken string, platformUserId *int, proxy *ProxyConfig) ([]string, error)
}
```

### 与原始 TS 接口的关键设计差异及理由

| 差异点 | TS 原始 | Go 设计 | 理由 |
|--------|---------|---------|------|
| `platformUserId` | 11/14 方法接受 `platformUserId?: number` | `*int`，所有方法接受 | 保持接口对称。NewApi-fork 平台在 cookie 回退时需要此 ID；非 fork 平台忽略 |
| `VerifyToken` 签名 | `verifyToken(baseUrl, token, platformUserId?)` | 同上，无 `credentialMode` | TS 无 `CredentialMode` 概念。Base 实现先试 session 再试 apikey (自动 fallback)。Go 保持相同逻辑 |
| `CreateAPIToken` 参数 | `options?: CreateApiTokenOptions` (7 字段) | `*CreateAPITokenOptions` (8 字段) | 保留全部字段。调用方 (createApiToken API) 可传入完整配置 |
| `DeleteAPIToken` 参数 | `tokenKey: string` (API key 字符串) | `tokenKey string` (API key 字符串) | 与 TS 一致。参数名从 `tokenID` 改为 `tokenKey` 反映实际语义 |
| `GetModels` 返回值 | `Promise<string[]>` (纯字符串) | `[]string` (纯字符串) | 与 TS 一致。不引入富类型 `ModelInfo` |
| `GetAPIToken` 返回值 | `Promise<string \| null>` | `(*string, error)` | Go 惯用方式。`nil` = 未找到 |

## 适配器能力矩阵 (14 平台 x 方法)

| Adapter | detect | detect 方式 | login | checkin | balance | models | api-token CRUD | groups | announcements |
|---------|--------|------------|-------|---------|---------|--------|----------------|--------|---------------|
| NewApi | YES | HTTP probe: GET /api/status, check `system_name` present | YES override | YES | YES (quota=remaining, total=quota+used, /500000) | YES multi-path | YES create/list/delete | YES | YES /api/notice |
| OneApi | YES | HTTP probe: GET /api/status, check `system_name` absent | YES base | YES | YES (quota=total, balance=quota-used, /500000) | YES /v1/models | YES create/list/delete + double-DELETE | YES | NO |
| OneHub | YES | title-first: URL keyword "onehub"/"one-hub" | YES inherited | YES inherited | YES inherited | YES override: /v1/models → /api/available_model fallback | YES inherited | YES override: /api/user_group_map | NO |
| DoneHub | YES | title-first: URL keyword "donehub"/"done-hub" | YES inherited | NO override: returns `success:false, "checkin endpoint not found"` | YES override: quota=remaining, total=quota+used, /500000 | YES inherited from OneHub | YES inherited | YES inherited | YES override: /api/notice |
| Veloera | YES | title-first: HTTP probe GET /api/status, check `system_name`/`version` includes "veloera" | YES base | YES (requires platformUserId, Veloera-User header) | YES override: quota=total, balance=quota-used, /1000000 | YES /v1/models | NO | NO | NO |
| Sub2Api | YES | title-first: URL keyword + /api/v1/auth/me probe + root title check | NO override: JWT-only, always `success:false` | NO override: always `success:false` | YES override: USD balance from /api/v1/auth/me x 500000 + subscription summary | YES override: 5 endpoint patterns + key discovery | YES create/list/delete (own API: /api/v1/keys) | YES override: 5 fallback + key inference | YES /api/v1/announcements |
| AnyRouter | YES | title-first: URL keyword "anyrouter" | YES inherited from NewApi | YES inherited | YES inherited | YES inherited | YES inherited | YES inherited | YES inherited |
| OpenAI | YES | URL keyword: "api.openai.com" | NO | NO | NO (0) | YES /v1/models | NO | NO | NO |
| Claude | YES | URL keyword: "api.anthropic.com" or "anthropic.com/v1" | NO | NO | NO (0) | YES: native Anthropic → /v1/models (strip /anthropic) | NO | NO | NO |
| Codex | YES | URL keyword: "chatgpt.com/backend-api/codex" | NO: OAuth only | NO override: "codex oauth connections do not support checkin" | NO (0) | NO (returns []) | NO | NO | NO |
| Gemini | YES | URL keyword: "generativelanguage.googleapis.com" / "googleapis.com/v1beta/openai" / "gemini.google.com" | NO | NO | NO (0) | YES override: 3-path (openai compat → native Gemini → openai compat) | NO | NO | NO |
| GeminiCli | YES | override: URL keyword "cloudcode-pa.googleapis.com" | NO inherited | NO inherited | NO inherited (0) | YES inherited | NO | NO | NO |
| Antigravity | YES | URL keyword: "antigravity" | NO: OAuth only | NO override: "checkin endpoint not supported" | NO (0) | YES /v1internal:fetchAvailableModels | NO | NO | NO |
| CliProxyApi | YES | HTTP probe: port 8317 OR "cliproxy" keyword OR GET /v0/management/openai-compatibility with x-cpa-* headers | NO | NO | NO (0) | YES /v0/management/openai-compatibility | NO | NO | NO |

**图例**:
- **YES override**: 适配器显式覆盖基类实现，提供自己的逻辑
- **YES inherited**: 适配器不覆盖，完全继承父类的实现
- **YES base**: 适配器不覆盖，继承 `BasePlatformAdapter` 的默认实现
- **NO**: 方法返回空/零值/unsupported (不抛异常)
- **NO override**: 适配器显式覆盖为 always-unsupported (如 Sub2Api login 返回 `success:false`)
- **HTTP probe**: 发送 HTTP 请求并根据响应判断 (区别于纯 URL 关键词匹配)
- **title-first**: 此平台在 detect 流程的第 2 步 (title hint + titleFirstPlatforms 集合) 即可短路匹配

### Balance 语义差异 (关键)

NewApi fork 家族存在三种不同的 balance 计算模式:

| 模式 | 平台 | quota 字段含义 | balance 计算 | divisor |
|------|------|---------------|-------------|---------|
| **A: quota=remaining** | NewApi, DoneHub | 剩余额度 | `balance = quota; total = quota + used` | 500,000 |
| **B: quota=total** | OneApi, OneHub | 总分配额度 | `balance = quota - used; total = quota` | 500,000 |
| **C: quota=total, 1M** | Veloera | 总分配额度 | `balance = quota - used; total = quota` | **1,000,000** |
| **D: USD balance** | Sub2Api | USD 余额 (不是 quota 字段) | `quota = balance_usd * 500000` | 500,000 (USD→quota) |
| **E: zero** | OpenAI, Claude, Codex, Gemini, GeminiCli, Antigravity, CliProxyApi | N/A | `balance=0, used=0, quota=0` | N/A |

**实现要求**: Go 的 `BaseAdapter.GetBalance` 不应假设统一公式。每个 fork 适配器必须实现自己的 `parseBalance` 逻辑。`BaseAdapter` 的默认实现返回 `{0, 0, 0}`。

## Platform Detection 流程 (4 步管道)

```
detectPlatform(url):
  1. URL Hint (直接匹配)
     调用 detectPlatformByUrlHint(url) — 基于已知 URL pattern 的快速匹配
     如果匹配 → 返回对应适配器 (通过 getAdapter 查找)

  2. Title Hint + titleFirstPlatforms 短路 (页面 title 匹配)
     调用 detectPlatformByTitle(url) — 爬取页面 <title> 标签
     可检测 7 种平台: anyrouter, done-hub, one-hub, veloera, sub2api, new-api, one-api
     但仅以下 5 种短路返回 (titleFirstPlatforms 集合):
       { anyrouter, done-hub, one-hub, veloera, sub2api }
     如果 title hint 匹配且在此集合中 → 返回对应适配器
     (new-api 和 one-api 不在短路集合中 — 它们需要第 3 步的 HTTP probe 来区分彼此)

  3. Sequential Probe (按注册顺序依次调用 adapter.Detect)
     for adapter in adapters (按注册顺序):
         if adapter.Detect(url) → return adapter
     Detect 方法可能是:
     - 纯 URL 关键词匹配 (OpenAI, Codex, AnyRouter, DoneHub, OneHub, Antigravity, GeminiCli, Gemini, Claude)
     - HTTP GET probe (NewApi, OneApi, Veloera, CliProxyApi, Sub2Api)

  4. Title Hint Fallback (兜底)
     如果第 2 步的 title hint 未短路但第 3 步全部失败:
       if titleHint → return getAdapter(titleHint)
      (处理 new-api/one-api 的 title 匹配但 probe 未命中的情况)

  5. 全部失败 → 返回 undefined (无法识别)
```

### 适配器注册顺序 (必须严格匹配)

```go
adapters := []PlatformAdapter{
    // "Specific forks before generic adapters for better auto-detection" (index.ts:20)
    NewOpenAiAdapter(),
    NewCodexAdapter(),
    NewClaudeAdapter(),
    NewGeminiAdapter(),
    NewGeminiCliAdapter(),
    NewAntigravityAdapter(),
    NewCliProxyApiAdapter(),
    NewAnyRouterAdapter(),
    NewDoneHubAdapter(),
    NewOneHubAdapter(),
    NewVeloeraAdapter(),
    NewNewApiAdapter(),
    NewSub2ApiAdapter(),
    NewOneApiAdapter(),
}
```

**顺序原理**:
- OpenAI/Codex/Claude/Gemini 等标准 API 提供商排在前面 (它们靠 URL 关键词快速返回 true/false, 不阻塞)
- AnyRouter/DoneHub/OneHub/Veloera 排在 NewApi 前面 (它们是 NewApi 的特定 fork, 需要优先匹配)
- NewApi 排在 Sub2Api/OneApi 前面 (其 HTTP probe `/api/status` 更具体, 需要 `system_name` 字段)
- OneApi 排在最后 (其 HTTP probe 匹配 `system_name` 不存在的响应 — 这是最宽泛的条件, 作为兜底)

### Title Hint 匹配规则 (titleHint.ts)

`detectPlatformByTitle` 检查 7 个平台, 12 条正则规则:

| 平台 | 正则 | 备注 |
|------|------|------|
| anyrouter | `/\bany\s*router\b/i` | |
| done-hub | `/\bdone[-_ ]?hub\b/i` | |
| one-hub | `/\bone[-_ ]?hub\b/i` | |
| veloera | `/\bveloera\b/i` | |
| sub2api | `/\bsub2api\b/i` | |
| new-api (5 rules) | `/\bnew[-_ ]?api\b/i`, `/\bvo[-_ ]?api\b/i`, `/\bsuper[-_ ]?api\b/i`, `/\brix[-_ ]?api\b/i`, `/\bneo[-_ ]?api\b/i`, `/wong\s*公益站/i` | 5 个 NewApi fork 变体 + wong 公益站 |
| one-api | `/\bone[-_ ]?api\b/i` | |

实现细节:
- 先规范化 URL 为 `protocol://host`, 然后 GET `/`
- 仅处理 `text/html` 或 `application/xhtml+xml` 响应
- 提取 `<title>` 标签内容, 消除多余空白
- 优先级: 按 TITLE_RULES 数组顺序 (anyrouter 最先, one-api 最后)
- 失败时重试一次 (50ms 延迟后), 处理并行测试下的竞态条件

一旦匹配到 new-api 或 one-api, title hint 不会在第 2 步短路。它们会进入第 3 步的 sequential probe, 通过 `/api/status` 响应区分 (NewApi: `system_name` 存在; OneApi: `system_name` 不存在)。

## 详细适配器规范

### NewApiAdapter (newApi.go, ~1450 LOC)

最完整的 NewApi fork 适配器, 也是 AnyRouter 的基类。

**Detect**: `GET /api/status`, 检查 `success===true && typeof data.system_name === 'string'`

**Login** (覆盖):
1. POST `/api/user/login`, body: `{ username, password }`, header: `X-Requested-With: XMLHttpRequest`
2. 使用 cookie 感知的 `fetchJsonRawWithCookie` (自动跟踪 Set-Cookie)
3. 如果 JSON 解析失败 (shield challenge), 尝试解决 `acw_sc__v2` 并重试 (最多 3 次)
4. 从响应中提取 token: `data`(直接字符串) | `token` | `accessToken` | `access_token` | `data.token` | `data.accessToken` | `data.access_token`
5. 如果上述都失败但 `res.success===true` 且 cookie 中包含可用的 session cookie → 返回 cookie header 作为 accessToken

**Checkin** (覆盖, ~90 LOC):
1. 如果未提供 `platformUserId`, 调用 `discoverUserId()` 自动探测
2. 尝试 POST `/api/user/checkin` (Bearer auth + userId headers)
3. 失败时如果错误消息指示需要 cookie (HTML 响应 / "New-Api-User" / "unexpected token" / 未登录 / 403): 进入 cookie fallback
4. Cookie fallback: POST `/api/user/sign_in` (Cookie header), 再 POST `/api/user/checkin` (Cookie + userId headers)
5. 尝试探测备选 userId (`probeAlternateUserIdByCookie`), 用备选 ID 重试 cookie checkin
6. 如果错误是 "missing checkin endpoint": 探测 cookie session 失败原因, 返回 session 错误消息

**GetBalance** (覆盖):
1. GET `/api/user/self` (Bearer auth + userId headers)
2. 失败时: GET `/api/user/self` cookie fallback (`fetchUserSelfByCookie`)
3. 再失败: `probeAlternateUserIdByCookie` → 用备选 userId 重试 cookie
4. Parse: `quota = data.quota / 500000` (剩余额度), `used = data.used_quota / 500000`, `total = quota + used`
5. 如果全部路径失败 → 抛出 error

**GetModels** (覆盖):
1. 先尝试 `/v1/models` (OpenAI-compat, shield cookie 用于 anyrouter 或 token 含 `=`)
2. 如果有 userId: GET `/api/user/models` (Bearer auth + userId headers)
3. Cookie fallback: `getSessionModelsByCookie`
4. 备选 userId fallback

**GetAPIToken / GetAPITokens** (覆盖):
- GET `/api/token/?p=0&size=100` (Bearer auth + userId headers)
- Cookie fallback: `getApiTokensByCookie`
- 返回首个 `enabled !== false` 的 token

**CreateAPIToken** (覆盖):
- `buildDefaultTokenPayload(options)`: name, unlimited_quota(true), expired_time(-1), remain_quota(0), allow_ips, model_limits_enabled(false), model_limits, group
- POST `/api/token/` (Bearer auth + userId headers)
- Cookie fallback: POST `/api/token/` (Cookie + userId headers)

**DeleteAPIToken** (覆盖):
- 参数 `tokenKey` 是 API key 字符串 (如 "sk-...")
- 1. GET `/api/token/?p=0&size=100` → 遍历 items 找到 key 匹配的 token → 提取 `id`
- 2. DELETE `/api/token/{id}`
- Cookie fallback: 用 cookie 列表 + 查找 ID + DELETE
- 如果上游已无该 key (`tokenId === null`) → 返回 true (本地删除也是安全的)

**GetUserGroups** (覆盖):
1. GET `/api/user/self/groups` (Bearer auth + userId)
2. GET `/api/user_group_map` (Bearer auth + userId)
3. Cookie fallback 对两个 endpoint
4. `parseGroupKeys`: 从 `data` 字段提取 keys (如果是对象) 或 `data` 数组提取值
5. 如果 `success===false` 且消息指示 expired → 抛出终端错误
6. 全部失败 → 返回 `["default"]`

**GetSiteAnnouncements** (覆盖):
- GET `/api/notice`
- 如果 `data` 是字符串 → 使用; 否则 `payload` 是字符串 → 使用
- 构建 notice → sourceKey 为 `notice:{sha1(content)}`

**VerifyToken** (覆盖, ~65 LOC):
1. 先试 `/v1/models` (apikey 路径)
2. GET `/api/user/self` (Bearer auth) → session 路径
3. 如果响应包含 "New-Api-User" → 用 `probeUserId` + authHeaders 重试
4. Cookie fallback: `fetchUserSelfByCookie`
5. 备选 userId cookie fallback
6. 返回 `tokenType: 'session'` 或 `'apikey'` 或 `'unknown'`

#### NewApi User-ID 探测系统

NewApi fork 平台在 cookie 认证时需要指定用户 ID (用于 `New-API-User` / `Veloera-User` header)。

**discoverUserId** (4 步):
1. JWT 解码: 从 accessToken 的 JWT payload 提取 `id` 或 `sub` 字段
2. Bearer 直接: GET `/api/user/self` 无 userId, 从响应 `data.id` 提取
3. Cookie 直接: `fetchUserSelfByCookie` 无 userId, 从响应 `data.id` 提取
4. Cookie 探测: `probeUserIdByCookie` — 遍历 candidate IDs × cookie candidates

**buildUserIdProbeCandidates**:
1. `tryDecodeUserId(token)` — JWT payload.id 或 payload.sub
2. `extractLikelyUserIds(token)`:
   - base64 解码 session cookie 的 payload
   - regex: `_(\d{4,8})` (下划线前缀的 4-8 位数字)
   - regex: `(user(?:name)?|uid|id)...(\d{4,8})` (user/id 关键词附近的数字)
   - **Gob 二进制解码**: 在 cookie payload 中搜索 `字段名 + 0x03 + "int" + 0x04` marker, 然后解码 Gob signed int (支持多字节编码)
3. 硬编码探针列表: `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20, 50, 100, 8899, 11494]` — 常见的 NewApi 部署默认管理员/root 用户 ID

**userIdHeaders**: 同时设置 7 个 header:
- `New-API-User` — 标准 NewApi
- `Veloera-User` — Veloera fork
- `voapi-user` — VoAPI fork
- `User-id` — 通用小写变体
- `X-User-Id` — 通用 X- 前缀变体
- `Rix-Api-User` — RixAPI fork
- `neo-api-user` — NeoAPI fork

**Gob 解码细节** (`decodeGobSignedInt` + `extractGobFieldInts`):
- 在二进制 session cookie payload 中搜索 Gob 编码的 `id` 字段
- Marker: `字段名(UTF-8) + 0x03 + "int"(UTF-8) + 0x04`
- 解析后 1 字节长度 + 0x00 分隔符 + 值字节
- Gob signed int: 如果最高位 < 0x80 → 直接读取; 否则宽度 = 0x100 - 首字节, 读取后续 width 个字节
- zigzag 解码: `(unsigned & 1) === 0 ? unsigned >> 1 : -((unsigned >> 1) + 1)`
- Go 实现可选择: (a) 使用 `encoding/gob` stdlib 复制此逻辑, 或 (b) 使 probe 列表可配置并跳过 Gob 解码

#### NewApi Shield Challenge (acw_sc__v2)

NewApi 部署可能有阿里云 WAF / CDN 的 `acw_sc__v2` cookie challenge。

**`solveAcwScV2`** (3 步):
1. `parseArg1`: regex `/var\s+arg1\s*=\s*['"]([0-9a-fA-F]+)['"]/` — 提取十六进制字符串
2. `parseMapping`: regex `/for\(var m=\[([^\]]+)\],p=L\(0x115\)/` — 提取重排映射数组 (十进制或 0x 十六进制)
3. `parseXorSeed`: 在 HTML 中提取并执行 JS 辅助函数 `a0i()` → 调用 `a0j(0x115)` 获取 XOR seed

求解: 按 mapping 重排 arg1 字符 → 每 2 个十六进制字符与 XOR seed 异或 → 得到 cookie 值

**Cookie 重试循环** (`fetchJsonRawWithCookie`):
- 最多 3 次尝试
- 每次自动跟踪 Set-Cookie 响应头
- 检测 HTML 响应中的 `var arg1=` / `acw_sc__v2` / `cdn_sec_tc` / `<script` → 触发 shield solver
- 将解决的 `acw_sc__v2` cookie 注入后续请求

### OneApiAdapter (oneApi.go, ~245 LOC)

NewApi fork 家族中 OneHub/DoneHub 的基类。

**Detect**: `GET /api/status`, 检查 `success===true && !data.system_name` (system_name 不存在 — 区别于 NewApi)

**Checkin**: POST `/api/user/checkin` (Bearer auth)

**GetBalance**: GET `/api/user/self`, `quota = data.quota / 500000`, `used = data.used_quota / 500000`, `balance = quota - used` (quota 是总额度, 不是剩余额度)

**GetModels**: GET `/v1/models` (Bearer auth)

**CreateAPIToken**: POST `/api/token/` (Bearer auth), `buildDefaultTokenPayload(options)` 同 NewApi

**DeleteAPIToken**: 双 DELETE 策略 (OneApi 特有):
1. GET `/api/token/?p=0&size=100` → 按 key 查找 → 提取 `id`
2. DELETE `/api/token/{id}`
3. 如果失败: DELETE `/api/token/{id}/` (尾部斜杠 fallback) — 某些 OneApi 版本需要

**GetUserGroups**:
1. GET `/api/user_group_map` (Bearer auth)
2. 失败: GET `/api/user/self/groups` (Bearer auth)
3. `extractGroupKeys`: 从 `data` 对象提取 keys (过滤 success/message/code/data/error)
4. 如果 `success===false` 且消息指示 expired → 抛出终端错误
5. 全部失败 → 返回 `["default"]`

### OneHubAdapter (oneHub.go, ~55 LOC)

继承 OneApiAdapter。

**Detect**: URL 关键词匹配 "onehub" 或 "one-hub"

**GetModels** (覆盖):
1. 调用 `super.getModels()` (OneApi 的 `/v1/models`)
2. 如果空: GET `/api/available_model` → `data` 对象的 keys (模型名) — OneHub 特有 endpoint

**GetUserGroups** (覆盖):
- GET `/api/user_group_map` (Bearer auth)
- `data` 对象的 keys (group name)
- 如果空: 调用 `super.getUserGroups()` (OneApi 的 user_group_map + user/self/groups)

**Announcements**: 不覆盖 — 继承自 BasePlatformAdapter, 返回 `[]`

### DoneHubAdapter (doneHub.go, ~58 LOC)

继承 OneHubAdapter。

**Detect**: URL 关键词匹配 "donehub" 或 "done-hub"

**Checkin** (覆盖): 永远返回 `{ success: false, message: 'checkin endpoint not found' }`

**GetBalance** (覆盖): `quota = data.quota / 500000` (剩余额度), `used = data.used_quota / 500000`, `total = quota + used` — 与 NewApi 相同 (quota 是剩余额度)

**GetSiteAnnouncements** (覆盖): GET `/api/notice`, 同 NewApi 逻辑

### VeloeraAdapter (veloera.go, ~62 LOC)

直接继承 BasePlatformAdapter (不继承 NewApi)。使用自己的 header 策略和 1M divisor。

**Detect**: `GET /api/status`, 检查 `success===true && (system_name.includes('veloera') || version.includes('veloera'))`

**veloeraHeaders**: 设置 `Authorization`, `Veloera-User`, `New-API-User`, `User-id` (3 个 header, 不是 7 个)

**Checkin**: POST `/api/user/checkin` (veloeraHeaders), 接受 `platformUserId`

**GetBalance**: GET `/api/user/self` (veloeraHeaders), **divisor = 1,000,000** (不是 500,000!), `balance = quota - used` (quota = total)

**GetModels**: GET `/v1/models` (Bearer auth)

**不支持**: login 使用 base (BasePlatformAdapter.login), getApiToken/getApiTokens/createApiToken/deleteApiToken/getUserGroups/getSiteAnnouncements 全部 base 默认 (返回空/null)

### Sub2ApiAdapter (sub2api.go, ~895 LOC)

最复杂且最独立的适配器。使用 `{ code: 0, message, data }` 信封格式的 JWT auth。

**Detect** (多路径):
1. URL 小写包含 "sub2api" → true
2. GET `/api/v1/auth/me` → 检查响应是否为 Sub2API 错误信封 (`code === 'UNAUTHORIZED'` 或 message 包含 "authorization header is required")
3. GET `/v1/models` → 同上
4. GET `/` → 检查 HTML title 是否匹配 `/\bsub2api\b/i`

**Login**: 覆盖为 `{ success: false, message: 'Sub2API uses JWT authentication; login is not supported' }`

**GetUserInfo**: GET `/api/v1/auth/me` → 解析 `parseSub2ApiEnvelope` (检查 `code === 0`)

**Checkin**: 覆盖为 `{ success: false, message: 'Check-in is not supported by Sub2API' }`

**GetBalance**: GET `/api/v1/auth/me` + GET `/api/v1/subscriptions/summary` (并行), `quota = balance_usd * 500000 / 500000 = balance_usd`, `used = 0` (无使用量明细)

**GetModels**:
1. `resolveModelEndpoints()` 生成 4 种 URL: `/v1/models`, `/api/v1/models`, `/v1beta/models`, `/antigravity/v1beta/models`
2. 依次尝试每个 URL (Bearer auth)
3. 如果全部失败: 调用 `getApiToken()` 发现用户 key, 用 key 重新尝试
4. 模型名规范化: 去除 `models/` 前缀

**GetAPITokens**: GET `/api/v1/keys?page=1&page_size=100` + GET `/api/v1/api-keys?page=1&page_size=100`

**CreateAPIToken**:
- POST `/api/v1/keys` + POST `/api/v1/api-keys`
- `group` → `group_id` (int), `expiredTime` → `expires_in_days` (计算), `remainQuota` → `quota`

**DeleteAPIToken**:
- 先 `listApiKeys()` 查找匹配 key → 提取 ID
- DELETE `/api/v1/keys/{id}` + DELETE `/api/v1/api-keys/{id}`
- 如果 key 不存在 → 返回 true (本地删除安全)

**GetUserGroups**:
1. `listGroups()`: 5 个 endpoint: `/api/v1/groups/available`, `/api/v1/groups?page=1&page_size=100`, `/api/v1/groups`, `/api/v1/group?page=1&page_size=100`, `/api/v1/group`
2. 如果空: `inferGroupsFromKeys()`: 从 `/api/v1/keys` 或 `/api/v1/api-keys` 的 token payload 提取 `group_id`
3. 如果仍然空: `["default"]`

**GetSiteAnnouncements**: GET `/api/v1/announcements?page=1&page_size=100` → 解析 `data` 数组或 `data.items` 数组

#### Sub2Api URL Resolution (resolveManagementBaseUrl)

从 base URL 中剥离 API 路径后缀以获取管理 API 根路径:

1. 规范化 (去尾部斜杠)
2. 循环剥离: `/models`, `/antigravity`, `/antigravity/v1beta`, `/antigravity/v1`, `/api/v1`, `/v1beta`, `/v1`
3. `https://sub2api.example.com/v1/models` → `https://sub2api.example.com`

#### Sub2Api Model Endpoint Resolution (resolveModelEndpoints)

从 base URL 生成模型端点:
- 如果 URL 以 `/models` 结尾 → 直接使用
- 如果 URL 以 `/v{N}` 或 `/v{N}beta` 结尾 → 追加 `/models`
- 如果 URL 以 `/antigravity` 结尾 → 生成 `/antigravity/v1/models` 和 `/antigravity/v1beta/models`
- 默认: 4 个候选: `/v1/models`, `/api/v1/models`, `/v1beta/models`, `/antigravity/v1beta/models`

#### Sub2Api parseSub2ApiEnvelope

所有 Sub2API 响应都包装在 `{ code: number, message?: string, data?: T }` 中:
- `code !== 0` → 抛出 Error(message || `Error code {code} from {endpoint}`)
- `data === undefined` → 抛出 Error
- 成功: 返回 `data as T`

### AnyRouterAdapter (anyrouter.go, ~11 LOC)

继承 NewApiAdapter。最简实现。

**Detect**: URL 小写包含 "anyrouter"

所有其他方法: 完全继承 NewApiAdapter (包括 cookie fallback, shield challenge, user-ID probing, 7-header injection)

### OpenAiAdapter (openai.go, ~17 LOC)

继承 StandardApiProviderAdapterBase。

**Detect**: URL 小写包含 "api.openai.com"

**GetModels**: GET `/v1/models` (Bearer auth), 标准 OpenAI `/v1/models` 端点

### ClaudeAdapter (claude.go, ~42 LOC)

继承 StandardApiProviderAdapterBase。

**Detect**: URL 小写包含 "api.anthropic.com" 或 "anthropic.com/v1"

**GetModels**:
1. 先尝试原生 Anthropic endpoint: `{baseUrl}/v1/models` with headers `x-api-key` + `anthropic-version: 2023-06-01`
2. 如果失败: 从 base URL 中剥离 `/anthropic` 后缀 → 用剥离后的 URL 尝试 OpenAI-compat `/v1/models` (Bearer auth)
   - 例如: `https://gateway.example.com/anthropic` → 尝试 `https://gateway.example.com/v1/models`
   - 这使通过 OpenAI-compat 网关代理的 Claude 端点能够进行模型发现

### CodexAdapter (codex.go, ~38 LOC)

继承 BasePlatformAdapter (不是 StandardApiProviderAdapterBase)。

**Detect**: URL 小写包含 "chatgpt.com/backend-api/codex"

**Login**: 覆盖为 `{ success: false, message: 'codex oauth login is managed via OAuth flow' }`

**GetUserInfo**: 覆盖为 `null`

**Checkin**: 覆盖为 `{ success: false, message: 'codex oauth connections do not support checkin' }`

**GetBalance**: 返回 `{ balance: 0, used: 0, quota: 0 }`

**GetModels**: 返回 `[]`

### GeminiAdapter (gemini.go, ~83 LOC)

继承 StandardApiProviderAdapterBase。

**Detect**: URL 小写包含 "generativelanguage.googleapis.com" / "googleapis.com/v1beta/openai" / "gemini.google.com"

**GetModels** (3 路径):
1. **OpenAI-compat 路径**: 如果 base URL 包含 `/openai/` → GET `{baseUrl}/v1beta/openai/models` (或 `/models` 如果 URL 以 `/models` 结尾)
2. **原生 Gemini 路径**: GET `{baseUrl}/v1beta/models?key={apiToken}`
3. **OpenAI-compat 回退**: 如果 URL 不含 `/openai/` 且原生返回空 → GET `{baseUrl}/v1beta/openai/models`

模型名规范化: 全部去除 `models/` 前缀 (`stripModelPrefix`)

### GeminiCliAdapter (geminiCli.go, ~10 LOC)

继承 GeminiAdapter。

**Detect** (覆盖): URL 小写包含 "cloudcode-pa.googleapis.com" — 自己的 URL pattern, 不继承 GeminiAdapter 的 detect

所有其他方法: 继承 GeminiAdapter

### AntigravityAdapter (antigravity.go, ~80 LOC)

继承 BasePlatformAdapter (不是 StandardApiProviderAdapterBase)。

**Detect**: URL 小写包含 "antigravity"

**Login**: 覆盖为 `{ success: false, message: 'login endpoint not supported' }`

**GetUserInfo**: 覆盖为 `null`

**Checkin**: 覆盖为 `{ success: false, message: 'checkin endpoint not supported' }`

**GetBalance**: 返回 `{ balance: 0, used: 0, quota: 0 }`

**GetModels**: POST `/v1internal:fetchAvailableModels` (body: `{}`), 携带特殊 headers:
- `User-Agent` (ANTIGRAVITY_USER_AGENT)
- `X-Goog-Api-Client` (ANTIGRAVITY_GOOGLE_API_CLIENT)
- `Client-Metadata` (ANTIGRAVITY_CLIENT_METADATA)
- 从响应 `models` 对象提取 keys 或数组 `id`/`name` 字段
- 默认 base URL 为 `ANTIGRAVITY_UPSTREAM_BASE_URL`

### CliProxyApiAdapter (cliproxyapi.go, ~60 LOC)

继承 StandardApiProviderAdapterBase。

**Detect** (3 条件, 任一满足):
1. URL 匹配 `/:8317/` (默认端口)
2. URL 小写包含 "cliproxy"
3. HTTP probe: GET `/v0/management/openai-compatibility` → 检查 `x-cpa-version` / `x-cpa-commit` / `x-cpa-build-date` headers (status 200/401/403 均可)

**GetModels**: GET `{baseUrl}/v1/models` (如果 URL 以 `/v{N}` 结尾追加 `/models`, 否则追加 `/v1/models`)

## BasePlatformAdapter 默认实现 (base.go)

Go 的 `BaseAdapter` 应实现以下默认行为:

| 方法 | 默认实现 |
|------|---------|
| `Login` | POST `/api/user/login`, body `{ username, password }`, 从 `res.data`(string) / `res.data.token` / `res.data.access_token` 提取 token |
| `GetUserInfo` | GET `/api/user/self` (Bearer auth), 解析 `res.data.{username, display_name, email, role}` |
| `VerifyToken` | 1. 试 `GetUserInfo` (session path) → if ok, 再 `GetBalance` + `GetApiToken`; 2. 试 `GetModels` (apikey path) |
| `Checkin` | 抛出 "not implemented" 或返回 unsupported |
| `GetBalance` | 返回 `{ balance: 0, used: 0, quota: 0 }` |
| `GetModels` | 抛出 "not implemented" 或返回 `[]` |
| `GetAPIToken` | 返回 `null` |
| `GetAPITokens` | 调用 `GetApiToken`, 如果非空返回 `[{ name: "default", key, enabled: true }]` |
| `CreateAPIToken` | 返回 `false` |
| `DeleteAPIToken` | 返回 `false` |
| `GetSiteAnnouncements` | 返回 `[]` |
| `GetUserGroups` | 返回 `["default"]` |

## StandardApiProviderAdapterBase 默认实现 (standardApiProvider.go)

继承 BasePlatformAdapter, 覆盖以下方法:

| 方法 | 默认实现 |
|------|---------|
| `Login` | `{ success: false, message: configurable }` (默认 "login endpoint not supported") |
| `GetUserInfo` | `null` |
| `Checkin` | `{ success: false, message: configurable }` (默认 "checkin endpoint not supported") |
| `GetBalance` | `{ balance: 0, used: 0, quota: 0 }` |

`fetchModelsFromStandardEndpoint` 辅助方法:
1. 规范化 base URL
2. 解析 URL: 如果 URL 以 `/v{N}` 结尾 → 追加 `/models`; 否则追追加 `/v1/models`
3. GET → 检查 `data` 数组 → 提取每个 item 的 `id` 字段

## Site Proxy (出站 HTTP 统一层)

```go
type SiteProxy struct {
    SystemProxyURL string
    client         *http.Client
}

func (p *SiteProxy) Do(ctx context.Context, req *http.Request, proxyConfig *ProxyConfig) (*http.Response, error)
```

- 支持 SOCKS5/HTTP 代理
- 支持 `useSystemProxy: true` → 使用 `config.SystemProxyURL`
- 支持 `customHeaders`: 站点级自定义 header 注入
- TLS 配置: `InsecureSkipVerify` 选项
- DNS: 可自定义 DNS resolver

在 TS 中, site proxy 通过 `withSiteProxyRequestInit(url, requestOptions)` 注入到每个请求。Go 的实现应提供类似的请求级代理注入。

## Edge Cases (完整列表)

### 通用
- 平台返回非标准 HTTP 状态码 → 正确解析错误消息
- 平台响应超时 → ctx 超时返回
- 代理不可达 → 返回明确代理错误 (不要误判为平台故障)
- HTML 错误页面 (非 JSON) → 提取 `<title>` 文本作为错误消息
- HTTP 错误消息格式: `HTTP {status}: {body_message}` → 解析 body 中的 JSON message/error.message/msg 字段

### NewApi / AnyRouter
- **Cookie 认证回退**: 当 Bearer auth 失败时, 使用 session cookie 重试
- **user-ID 多 header 注入**: 同时设置 7 个 header 适配不同 fork 变体
- **Gob 二进制 user-ID 提取**: 从 Go `encoding/gob` 编码的 session payload 中解码用户 ID
- **硬编码 user-ID 探针列表**: `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20, 50, 100, 8899, 11494]`
- **Shield challenge (acw_sc__v2)**: 解析阿里云 WAF challenge → 求解 cookie → 最多 3 次重试
- **checkin 缺失处理**: 如果 checkin endpoint 缺失, 探测 cookie session 失败原因并返回 session 错误
- **token 列表响应格式兼容**: 支持 `data[]`, `data.items[]`, `data.data[]`, `items[]`, `list[]`, `data.list[]` 6 种格式
- **login 时 session cookie 回退**: 如果无 access token 但有可用 session cookie → 返回 cookie 作为 accessToken
- **AnyRouter shield cookie 模型发现**: 使用 `fetchJsonWithShieldCookieRetry` 处理可能被 shield 保护的 `/v1/models`

### OneApi / OneHub / DoneHub
- **OneApi 双 DELETE**: DELETE `/api/token/{id}` → 失败则 DELETE `/api/token/{id}/`
- **OneHub /api/available_model fallback**: 如果 `/v1/models` 返回空, 尝试 `/api/available_model`
- **DoneHub balance remaining-quota 语义**: `quota` 是剩余值, `total = quota + used`
- **OneApi detect 与 NewApi 的区别**: 两者都 probe `/api/status`, OneApi 要求 `system_name` 不存在

### Veloera
- **1,000,000 divisor**: 与所有其他 NewApi fork 的 500,000 不同 — 相差 2 倍

### Sub2Api
- **JWT auth, 无 login**: login 总是返回 `success: false`
- **checkin 不支持**: 总是返回 `success: false`
- **`{ code, message, data }` 信封**: 所有响应都需要 `code === 0` 检查
- **5 个 group endpoint fallback**: `/api/v1/groups/available` → `?page` → bare → `/api/v1/group?page` → `/api/v1/group`
- **Key-based group inference**: 如果 group 列表空, 从 API key payload 的 `group_id` 推断
- **4 种 model endpoint 候选**: `/v1/models`, `/api/v1/models`, `/v1beta/models`, `/antigravity/v1beta/models`
- **URL resolution**: 剥离 `/models`, `/antigravity*`, `/api/v1`, `/v1*` 后缀
- **expiredTime 转换**: unix timestamp → `expires_in_days` (天数, 1-3650)
- **managed auth 过期 → 自动 refresh** (在 P8 ProxyCore 中处理)

### Claude
- **`/anthropic` 后缀剥离**: 用剥离后的 URL 尝试 OpenAI-compat `/v1/models`
- **双认证方式**: 原生 `x-api-key` + `anthropic-version` vs OpenAI-compat `Bearer`

### Gemini / GeminiCli
- **3 路径模型发现**: OpenAI-compat (如果在 `/openai/` 路径) → 原生 `?key=` → OpenAI-compat 回退
- **模型名 `models/` 前缀剥离**: `stripModelPrefix`
- **GeminiCli 独立 detect**: 覆盖为 `cloudcode-pa.googleapis.com`

### Detection Pipeline
- **title-first 集合限制**: 标题可检测 7 种平台, 但只有 5 种短路 (new-api/one-api 需要 HTTP probe 区分)
- **重试**: title hint 失败时重试一次 (50ms 延迟)
- **HTML 内容类型检查**: title hint 只处理 `text/html` 或 `application/xhtml+xml`

## Unsupported 方法的契约

当一个方法不受支持时, 适配器**不得抛出异常**。必须返回结构化的 "not supported" 结果:

- **Login**: `LoginResult{Success: false, Message: "reason"}`
- **Checkin**: `CheckinResult{Success: false, Message: "reason"}`
- **GetBalance**: `BalanceInfo{Balance: 0, Used: 0, Quota: 0}` (无 error)
- **GetModels**: `[]string{}` (空切片, 无 error)
- **GetAPIToken**: `nil` (无 error)
- **GetAPITokens**: `[]ApiTokenInfo{}` (空切片, 无 error)
- **CreateAPIToken**: `false` (无 error)
- **DeleteAPIToken**: `nil` (无 error — 视为幂等操作)
- **GetSiteAnnouncements**: `[]SiteAnnouncement{}` (空切片, 无 error)
- **GetUserGroups**: `["default"]` (无 error)
- **GetUserInfo**: `nil` (无 error)
- **VerifyToken**: `TokenVerifyResult{TokenType: "unknown"}` (无 error)

调用方通过检查 `result.Success` 或返回值的空/非空来区分 "不支持" 和 "失败"。

## Acceptance Criteria
- [ ] 14 个适配器全部实现 PlatformAdapter 接口
- [ ] `detectPlatform(url)` 正确识别每个平台 (4 步管道, 注册顺序正确)
- [ ] 继承链正确: OneApi->OneHub->DoneHub, NewApi->AnyRouter, Gemini->GeminiCli, StandardApiProviderBase->OpenAI/Claude/Gemini/CliProxyApi
- [ ] StandardApiProvider 适配器 getBalance 返回 0, checkin/login 返回 unsupported
- [ ] NewApi 适配器实现完整的 cookie fallback + user-ID probing + shield challenge
- [ ] Sub2Api subscription summary 解析正确 + 信封检查 + URL resolution
- [ ] Veloera 使用 1,000,000 divisor (不是 500,000)
- [ ] DoneHub balance 使用 quota+used 公式 (quota=剩余额度)
- [ ] OneApi deleteApiToken 实现双 DELETE (尾部斜杠 fallback)
- [ ] SiteProxy SOCKS5/HTTP 代理工作
- [ ] 每个平台至少一个 happy-path 集成测试
- [ ] platformUserId 在所有方法中作为可选参数传递

## Test Plan
| 文件 | 内容 |
|------|------|
| `platform/registry_test.go` | detect 逻辑, 别名映射, 适配器注册顺序 |
| `platform/newapi_test.go` | login(cookie回退), checkin(cookie回退+多userId), balance(quota+used), models(multi-path), token CRUD, shield challenge |
| `platform/oneapi_test.go` | 继承行为, 双 DELETE, balance(quota-used) |
| `platform/onehub_test.go` | available_model fallback, user_group_map |
| `platform/donehub_test.go` | balance(quota+used), checkin unsupported, announcements /api/notice |
| `platform/veloera_test.go` | 1M divisor, Veloera-User header, checkin with userId |
| `platform/sub2api_test.go` | subscription summary, parseSub2ApiEnvelope, URL resolution, group inference, model endpoints |
| `platform/anyrouter_test.go` | 继承 NewApi, detect URL keyword |
| `platform/standard_test.go` | OpenAI/Claude/Gemini /v1/models, Claude /anthropic stripping |
| `platform/claude_test.go` | /anthropic suffix stripping, x-api-key vs Bearer |
| `platform/gemini_test.go` | 3-path model discovery, stripModelPrefix |
| `platform/gemini_cli_test.go` | detect override cloudcode-pa |
| `platform/antigravity_test.go` | /v1internal:fetchAvailableModels |
| `platform/cliproxyapi_test.go` | port 8317 detect, x-cpa-* headers |
| `platform/site_proxy_test.go` | SOCKS5, HTTP proxy, custom headers |
| `platform/detect_test.go` | URL hint, title hint (7 platforms, 5 short-circuit), sequential probe, registration order |
