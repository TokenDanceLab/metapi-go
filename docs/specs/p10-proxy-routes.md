# P10: Proxy Routes (/v1/*)

**S.U.P.E.R**: S (单一职责) · U (单向流) | **依赖**: P7 + P8 + P9 | **Size**: M

## 原始 TS 参考
- `<metapi-ts>\src\server\routes\proxy\router.ts` — 代理路由注册
- `<metapi-ts>\src\server\routes\proxy\chat.ts` — chat + messages
- `<metapi-ts>\src\server\routes\proxy\completions.ts` — completions
- `<metapi-ts>\src\server\routes\proxy\responses.ts` — responses HTTP endpoints
- `<metapi-ts>\src\server\routes\proxy\responsesWebsocket.ts` — responses WebSocket transport
- `<metapi-ts>\src\server\routes\proxy\embeddings.ts` — embeddings
- `<metapi-ts>\src\server\routes\proxy\images.ts` — images (generations + edits + variations)
- `<metapi-ts>\src\server\routes\proxy\models.ts` — models list
- `<metapi-ts>\src\server\routes\proxy\files.ts` — files (re-exports from filesSurface)
- `<metapi-ts>\src\server\routes\proxy\videos.ts` — videos
- `<metapi-ts>\src\server\routes\proxy\gemini.ts` — Gemini native surface (re-exports from geminiSurface)
- `<metapi-ts>\src\server\routes\proxy\search.ts` — search
- `<metapi-ts>\src\server\routes\proxy\multipart.ts` — multipart/form-data helpers
- `<metapi-ts>\src\server\routes\proxy\upstreamEndpoint.ts` — upstream endpoint request builder
- `<metapi-ts>\src\server\routes\proxy\upstreamError.ts`
- `<metapi-ts>\src\server\routes\proxy\downstreamPolicy.ts` — downstream API key policy enforcement
- `<metapi-ts>\src\server\routes\proxy\proxyBilling.ts` — billing cost estimation
- `<metapi-ts>\src\server\proxy-core\downstreamClientContext.ts` — client detection (287 lines)
- `<metapi-ts>\src\server\proxy-core\cliProfiles\types.ts` — CLI profile type definitions
- `<metapi-ts>\src\server\proxy-core\cliProfiles\registry.ts` — CLI profile registry

## Go 模块结构
```
handler/proxy/
  router.go            # RegisterProxyRoutes(chi.Router) — registers all 13 surfaces
  chat.go              # POST /v1/chat/completions + /chat/completions (surface: "openai")
  messages.go          # POST /v1/messages (surface: "claude") + /v1/messages/count_tokens
  completions.go       # POST /v1/completions
  responses.go         # POST /v1/responses + /v1/responses/compact + GET 426 + alias rejection
  responses_ws.go      # WebSocket: Codex responses via WS upgrade + HTTP fallback
  embeddings.go        # POST /v1/embeddings
  images.go            # POST /v1/images/generations + edits (multipart) + variations (400)
  models.go            # GET /v1/models (dual OpenAI/Claude format)
  files.go             # Files surface (proxy-core delegated)
  input_files.go       # 文件输入解析 (inputFiles)
  videos.go            # POST /v1/videos + GET/DELETE /v1/videos/:id
  gemini.go            # Gemini native surface: GET /v1beta/models, POST /v1beta/models/*, etc.
  search.go            # POST /v1/search
  multipart.go         # multipart/form-data 解析 + FormData helpers
  client_detect.go     # detectDownstreamClientContext — full CLI profile + app fingerprinting
```

## 端点清单 (13 surfaces, 29 route registrations)

### 表 1: OpenAI Chat 表面
| Method | Path | Handler | Surface Identifier |
|--------|------|---------|--------------------|
| POST | `/v1/chat/completions` | handleChatSurfaceRequest(request, reply, `"openai"`) | `"openai"` |
| POST | `/chat/completions` | handleChatSurfaceRequest(request, reply, `"openai"`) | `"openai"` |

### 表 2: Claude Messages 表面
| Method | Path | Handler | Surface Identifier |
|--------|------|---------|--------------------|
| POST | `/v1/messages` | handleChatSurfaceRequest(request, reply, `"claude"`) | `"claude"` |
| POST | `/v1/messages/count_tokens` | handleClaudeCountTokensSurfaceRequest(request, reply) | -- |

### 表 3: Completions 表面
| Method | Path | 说明 |
|--------|------|------|
| POST | `/v1/completions` | 传统 completions (non-chat) |

### 表 4: Responses 表面 (HTTP)
| Method | Path | 说明 |
|--------|------|------|
| POST | `/v1/responses` | OpenAI responses — 调用 `handleOpenAiResponsesSurfaceRequest(request, reply, "/v1/responses")` |
| GET | `/v1/responses` | **返回 426** `{"error":{"message":"WebSocket upgrade required for GET /v1/responses","type":"invalid_request_error"}}` |
| POST | `/v1/responses/compact` | Compact 优化 — 调用 `handleOpenAiResponsesSurfaceRequest(request, reply, "/v1/responses/compact")` |

### 表 5: Responses 别名 (Codex 原生路径, 无 /v1 前缀)
| Method | Path | 说明 |
|--------|------|------|
| POST | `/responses` | 映射到 `/v1/responses` |
| POST | `/responses/*` | 别名解析: `/responses` -> `/v1/responses`, `*/compact` -> `/v1/responses/compact`. **未知子路径返回 404** `{"error":{"message":"Unknown /responses alias path","type":"invalid_request_error"}}` |
| GET | `/responses` | **返回 426** `{"error":{"message":"WebSocket upgrade required for GET /v1/responses","type":"invalid_request_error"}}` |
| GET | `/responses/*` | 识别 /responses 和 */compact 后 **仍然返回 426** `{"error":{"message":"WebSocket upgrade required for GET <downstreamPath>","type":"invalid_request_error"}}`; 未知路径返回 404 |

### 表 6: Models 表面
| Method | Path | 说明 |
|--------|------|------|
| GET | `/v1/models` | 模型列表。检测 `anthropic-version` 或 `x-api-key` header 决定返回格式: 有则 `responseFormat: "claude"`, 无则 `responseFormat: "openai"`. 集成 `tokenRouter`, `refreshModelsAndRebuildRoutes`, `isModelAllowedByPolicyOrAllowedRoutes`. |

### 表 7: Embeddings 表面
| Method | Path | 说明 |
|--------|------|------|
| POST | `/v1/embeddings` | 向量嵌入. body 中 `model` 字段必填, 缺失返回 400. |

### 表 8: Images 表面
| Method | Path | 说明 |
|--------|------|------|
| POST | `/v1/images/generations` | 图片生成 (JSON body, model 默认 `"gpt-image-1"`) |
| POST | `/v1/images/edits` | 图片编辑. **支持 multipart/form-data 或 JSON body**. model 默认 `"gpt-image-1"`. 从 multipart 读取 `model` 字段, 回退到 JSON body. |
| POST | `/v1/images/variations` | **始终返回 400** `{"error":{"message":"Image variations are not supported","type":"invalid_request_error"}}` |

### 表 9: Videos 表面
| Method | Path | 说明 |
|--------|------|------|
| POST | `/v1/videos` | 视频生成任务. **支持 multipart/form-data 或 JSON body**. model 必填, 缺失返回 400. 上游返回后保存 `proxyVideoTask` mapping (publicId -> upstreamVideoId), 响应中 `id` 改写为 publicId. |
| GET | `/v1/videos/:id` | 按 publicId 查询视频任务状态, 代理到上游 GET. 刷新 snapshot. |
| DELETE | `/v1/videos/:id` | 按 publicId 删除视频任务, 代理到上游 DELETE. 成功则删除本地 mapping. |

### 表 10: Search 表面
| Method | Path | 说明 |
|--------|------|------|
| POST | `/v1/search` | 搜索. `query` 必填 (trim 后为空返回 400). `stream=true` 返回 400 (不支持 streaming). `max_results` 默认 10, 最大 20, 非整数或越界返回 400. `model` 默认 `"__search"`. |

### 表 11: Files 表面
| Method | Path | 说明 |
|--------|------|------|
| (由 proxy-core surfaces/filesSurface.ts 代理) | | 文件上传/下载, 由 filesSurface 模块实现, router 仅 re-export. |

### 表 12: Gemini 表面 (7 个路由)
| Method | Path | 说明 |
|--------|------|------|
| GET | `/v1beta/models` | Gemini 模型列表 (proxy API version 由 transformer 解析) |
| GET | `/gemini/:geminiApiVersion/models` | Gemini 模型列表 (动态 API version) |
| POST | `/v1beta/models/*` | Gemini generateContent 原生请求. 路径解析 `{apiVersion}/models/{model}:{action}`, 支持 stream (SSE) 和非 stream. |
| POST | `/gemini/:geminiApiVersion/models/*` | 同上, 动态 API version |
| POST | `/v1internal::generateContent` | **Gemini CLI 内部协议**. `downstreamProtocol: "gemini-cli"`, action `"generateContent"`. 解析 body 中的 `model` 字段, 去掉后代理. |
| POST | `/v1internal::streamGenerateContent` | Gemini CLI stream. `downstreamProtocol: "gemini-cli"`, action `"streamGenerateContent"`. |
| POST | `/v1internal::countTokens` | Gemini CLI countTokens. `downstreamProtocol: "gemini-cli"`, action `"countTokens"`. |

Gemini 表面的三种处理路径:
1. **Direct Gemini family** (platform 为 `gemini`/`gemini-cli`/`antigravity`): 直接代理到上游 Gemini endpoint. 对 gemini-cli 平台使用 `wrapGeminiCliRequest` 包装请求, 对 antigravity 使用 `resolveAntigravityProviderAction` 解析 action. 支持 401 OAuth token refresh.
2. **Gemini compatibility** (非 Gemini 平台): 通过 `geminiGenerateContentTransformer.compatibility.buildOpenAiBodyFromGeminiRequest` 转换为 OpenAI body, 然后走 endpoint flow (chat/completions/responses). 响应通过 `serializeNormalizedFinalToGemini` 转回 Gemini 格式.
3. **countTokens on non-Gemini**: 返回 501 (未实现).

### 表 13: Route Aliases 总结
| Alias Path | Resolved To | Surface Format Parameter |
|---|---|---|
| `/chat/completions` | `/v1/chat/completions` | `"openai"` |
| `/responses` | `/v1/responses` | `"/v1/responses"` (full path) |
| `/responses/compact` | `/v1/responses/compact` | `"/v1/responses/compact"` (full path) |

## Handler 模式 (proxy handler)

每个代理 handler 遵循统一的 channel selection + retry loop 模式:

```go
func HandleProxySurface(w http.ResponseWriter, r *http.Request) {
    // 1. 从 context 取 ProxyAuthContext
    auth := GetProxyAuth(r.Context())

    // 2. 解析请求体, 提取 requestedModel
    var reqBody map[string]interface{}
    json.NewDecoder(r.Body).Decode(&reqBody)
    requestedModel, _ := reqBody["model"].(string)

    // 3. 验证: model 必填 (chat/completions/embeddings/videos/search)
    if requestedModel == "" {
        writeJSON(w, 400, errorResponse("model is required", "invalid_request_error"))
        return
    }

    // 4. Downstream policy: 检查模型是否允许
    if !ensureModelAllowedForDownstreamKey(r, requestedModel) {
        return // 已写入 403
    }

    // 5. Client detection: 返回结构化 context
    clientCtx := detectDownstreamClientContext(DetectInput{
        DownstreamPath: r.URL.Path,
        Headers:        r.Header,
        Body:           reqBody,
    })
    // clientCtx.ClientKind    -> "generic" | "codex" | "claude_code" | "gemini_cli"
    // clientCtx.SessionID     -> string | ""
    // clientCtx.TraceHint     -> string | ""
    // clientCtx.ClientAppID   -> string | ""
    // clientCtx.ClientAppName -> string | ""
    // clientCtx.ClientConfidence -> "exact" | "heuristic" | ""

    // 6. Channel selection + retry loop
    excludeChannelIDs := []int64{}
    for retry := 0; retry <= maxRetries; retry++ {
        selected, err := selectProxyChannelForAttempt(SelectInput{
            RequestedModel:    requestedModel,
            DownstreamPolicy:  auth.Policy,
            ExcludeChannelIDs: excludeChannelIDs,
            RetryCount:        retry,
            ForcedChannelID:   getTesterForcedChannelID(r),
        })
        if selected == nil {
            writeJSON(w, 503, errorResponse("No available channels", "server_error"))
            return
        }
        excludeChannelIDs = append(excludeChannelIDs, selected.Channel.ID)

        // 7. 构建上游请求
        upstreamModel := selected.ActualModel
        forwardBody := cloneAndSetModel(reqBody, upstreamModel)
        upstreamURL := buildUpstreamURL(selected.SiteBaseURL, downstreamPath)

        // 8. 发送上游请求 (带 first-byte timeout)
        startTime := time.Now()
        upstreamResp, err := fetchWithFirstByteTimeout(upstreamURL, forwardBody, selected.TokenValue)
        if err != nil {
            recordFailure(selected, err)
            if shouldRetry(err, retry) { continue }
            writeJSON(w, 502, errorResponse(err.Error(), "upstream_error"))
            return
        }

        // 9. 处理响应 (streaming vs non-streaming)
        if isStream {
            handleStreamResponse(w, upstreamResp, selected, requestedModel, startTime, clientCtx)
        } else {
            handleNonStreamResponse(w, upstreamResp, selected, requestedModel, startTime, clientCtx)
        }

        // 10. 写 proxy_log + billing
        insertProxyLog(...)
        return
    }
}
```

### Streaming 响应处理
```go
func handleStreamResponse(w http.ResponseWriter, upstreamResp *http.Response, ...) {
    // SSE headers — MUST include Connection: keep-alive
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(200)
    flusher := w.(http.Flusher)

    // 逐 chunk 透传, 同时解析 SSE data events 累积 usage
    reader := bufio.NewReader(upstreamResp.Body)
    sseBuffer := ""
    for {
        line, err := reader.ReadString('\n')
        if err != nil { break }
        w.Write([]byte(line))
        flusher.Flush()
        // 累积解析 usage...
    }
}
```

### Non-streaming 响应处理
```go
func handleNonStreamResponse(w http.ResponseWriter, upstreamResp *http.Response, ...) {
    bodyBytes, _ := io.ReadAll(upstreamResp.Body)
    // 检测 proxy failure (空响应 + 零 usage)
    failure := detectProxyFailure(bodyBytes)
    if failure != nil {
        if shouldRetry(failure, retry) { continue }
        writeJSON(w, failure.Status, errorResponse(failure.Reason, "upstream_error"))
        return
    }
    // 解析 usage, resolve billing, record success
    w.WriteHeader(upstreamResp.StatusCode)
    w.Write(bodyBytes)
}
```

### 请求体验证规则
| 端点 | 验证 |
|------|------|
| `/v1/chat/completions` | `model` 必填 (在 chatSurface 层面处理) |
| `/v1/completions` | `model` 必填, 缺失返回 400 |
| `/v1/embeddings` | `model` 必填, 缺失返回 400 |
| `/v1/search` | `query` 必填 (trim 后非空). `stream` 不能为 true. `max_results` 范围 [1, 20], 默认 10. |
| `/v1/videos` | `model` 必填, 缺失返回 400 |
| `/v1/images/edits` | model 默认 `"gpt-image-1"` |

## Client Detection 详细规格

### 概述
`detectDownstreamClientContext` 返回结构化的 `DownstreamClientContext`:
```go
type DownstreamClientContext struct {
    ClientKind       string // "generic" | "codex" | "claude_code" | "gemini_cli"
    SessionID        string // 会话 ID (用于 proxy log 和 WS 会话管理)
    TraceHint        string // 调试追踪标记
    ClientAppID      string // 客户端应用 ID (如 "cherry_studio", "opencode", "openclaw")
    ClientAppName    string // 客户端应用显示名 (如 "Cherry Studio", "OpenCode", "OpenClaw")
    ClientConfidence string // "exact" | "heuristic" | ""
}
```

**注意**: TS 中不存在 `"openai"` client kind. OpenAI-protocol 请求不匹配任何特定 CLI profile 时 fall through 到 `"generic"`.

### CLI Profile Registry (按检测优先级排序)
```
1. claude_code  — 检测 Claude Code (AxonHub)
2. codex        — 检测 Codex CLI / OpenAI Codex
3. gemini_cli   — 检测 Gemini CLI
4. generic      — 兜底 (所有不匹配的请求)
```

每个 profile 定义:
- `id`: CliProfileId (上述之一)
- `capabilities`: 能力标志 (见下表)
- `detect(input)`: 检测函数, 返回 `{ id, sessionId?, traceHint?, clientAppId?, clientAppName?, clientConfidence? } | null`

### CLI Profile Capabilities
| Profile | supportsResponsesCompact | supportsResponsesWebsocketIncremental | preservesContinuation | supportsCountTokens | echoesTurnState |
|---------|--------------------------|---------------------------------------|----------------------|--------------------|----------------|
| claude_code | true | false | true | false | false |
| codex | true | true | true | false | true |
| gemini_cli | false | false | false | true | false |
| generic | false | false | false | false | false |

### 检测输入
```go
type DetectInput struct {
    DownstreamPath string              // 请求路径 (如 "/v1/chat/completions")
    Headers        map[string][]string // 规范化后的 headers (key 小写)
    Body           interface{}         // 请求体 (用于 system prompt 检查和 metadata.user_id 提取)
}
```

### Session ID 提取
- **Claude Code** (claude_code profile): 从 `body.metadata.user_id` 提取 (UUID 格式, AxonHub 格式)
- **Codex** (codex profile): 从 `Session_id` 或 `conversation_id` header 提取
- **Gemini CLI** (gemini_cli profile): 无 session ID

### 检测路径 (按优先级)
1. **CLI Profile 检测** (orderedProfiles 顺序: claude_code > codex > gemini_cli):
   - `claude_code` profile 检测: 检查 User-Agent / headers 特征 + body.metadata.user_id
   - `codex` profile 检测: 检查 headers 特征
   - `gemini_cli` profile 检测: **检查 downstreamPath 是否匹配 `/v1internal:` 模式** (路径检测, 非仅 headers)
   - 都不匹配 -> `generic`

2. **显式自报** (Explicit Self-Report):
   - 解析 `x-openai-client-user-agent` header (JSON): 提取 `client`/`name`/`app` 字段
   - 检查 `User-Agent: OpenClaw/...` 模式 -> `clientAppId: "openclaw"`, `clientAppName: "OpenClaw"`
   - 优先级低于 app fingerprinting

3. **App Fingerprinting** (应用指纹识别, 按 priority 降序):
   - **OpenCode** (priority=110): 检测 x-title/referer/user-agent 含 "opencode" -> `"exact"`; 如果 body system prompt 包含 "you are opencode" 或 "file called opencode.md" -> `"heuristic"`
   - **Cherry Studio** (priority=100): 检测 x-title + referer 同时匹配 -> `"exact"`; 弱信号 (user-agent/x-title/referer 部分匹配) -> `"heuristic"`

4. 如果指纹和自报都未匹配, 使用 CLI profile 自身的 `clientAppId`/`clientAppName` (如 codex profile 提供 `clientAppId: "codex"`, `clientAppName: "Codex CLI"`)

### Gemini CLI 路径检测
`gemini_cli` profile 通过 downstreamPath 模式检测:
- 匹配 `/v1internal:generateContent`, `/v1internal:streamGenerateContent`, `/v1internal:countTokens` 即识别为 `gemini_cli`

## WebSocket (Responses) 详细规格

### 模块: `responses_ws.go`
对应 TS: `responsesWebsocket.ts` (845 行)

### 架构概述
WebSocket transport 在 HTTP server 的 `upgrade` 事件上注册, 而非通过路由注册. 仅在 pathname 为 `/v1/responses` 时处理升级.

### WebSocket 升级握手
```
1. 监听 server.on('upgrade') 事件
2. 解析 URL pathname, 仅匹配 /v1/responses
3. 提取 auth token 来源 (按优先级):
   a. Authorization: Bearer <token>
   b. x-api-key header
   c. x-goog-api-key header
   d. key query parameter
4. 验证 token (authorizeDownstreamToken):
   - 失败: 发送 HTTP upgrade 错误 (401/403), 关闭 socket
   - 成功: 执行 WebSocket upgrade
5. x-codex-turn-state header: 在 WS upgrade 响应 headers 中回显
```

### WS 连接生命周期 (`handleResponsesWebsocketConnection`)
```
1. 生成/提取 websocketSessionID:
   - 来源: session-id / session_id / conversation-id / conversation_id header
   - 兜底: randomUUID()

2. 初始化状态:
   - lastRequest = null (上一个归一化后的请求)
   - lastResponseOutput = [] (上一个响应的 output 数组)
   - selectedChannel = null (当前选中的 channel)
   - messageQueue = Promise.resolve() (消息序列化队列)

3. 注册 socket.once('close'): 清理所有 codexWebsocketRuntime sessions
```

### WS 消息处理流程 (每条消息串行通过 messageQueue)
```
1. 解析 JSON payload
2. 提取 requestedModel (从 parsed.model 或 lastRequest.model)
3. 模型允许检查: isModelAllowedByPolicyOrAllowedRoutes(requestedModel, policy)
   - 不允许 -> 403 error frame, return
4. 第一次 Service Tier Policy 检查 (applyOpenAiServiceTierPolicy):
   - 基于 body 和 requestedModel 检查
   - 不通过 -> error frame, return
   - 通过 -> 将 service_tier 注入 parsed
5. 检测 incremental input 支持:
   - 检查当前 selectedChannel 是否支持 incremental input
   - 不满足则通过 supportsResponsesWebsocketIncrementalInput 查询
6. 判断是否 pre-warm 本地合成 (shouldHandleResponsesWebsocketPrewarmLocally):
   - 条件: !incrementalInput && !lastRequest && type=="response.create" && generate==false
7. 请求归一化 (normalizeResponsesWebsocketRequest):
   - 处理 response.create / response.append 消息类型
   - incremental mode: 每次 response.create 独立 (保留 previous_response_id)
   - non-incremental mode: 合并 input 数组 (lastRequest.input + lastResponseOutput + current.input)
   - 验证: model 必填, input 必须是数组
8. Managed key 配额消费: consumeManagedKeyRequest (仅 managed key)
9. Pre-warm 处理 (如果适用): 本地合成 response.created + response.completed, 不调上游, return
10. Channel 复用检查: 如果 model 改变, 重新 selectChannel
11. 第二次 Service Tier Policy 检查:
    - 基于归一化后的 request, actualModel, sitePlatform, accountType 检查
    - 不通过 -> error frame, return
12. 更新 lastRequest = nextRequestSnapshot
```

### Codex WS Runtime 路径 (685-784)
```
条件: selectedChannelSupportsCodexWebsocketTransport(selectedChannel, requestModel)
  = platform=="codex" && model match && config.codexUpstreamWebsocketEnabled
    && account.extraConfig 中 websockets 字段不为 false

1. 构建 OAuth provider headers (buildOauthProviderHeaders)
2. 生成 websocketRuntimeSessionKey:
   buildCodexSessionResponseStoreKey({sessionId, siteId, accountId, channelId}) || websocketSessionID
3. 通过 buildUpstreamEndpointRequest 构建上游请求:
   endpoint: "responses", stream: true, downstreamFormat: "responses"
4. 调用 codexWebsocketRuntime.sendRequest({sessionId, requestUrl, headers, body})
5. 成功: 逐条转发 events 到下游 socket, 收集 lastResponseOutput
6. 失败处理:
   a. runtimeError.events.length == 0 (零事件):
      -> HTTP fallback: forwardResponsesRequestViaHttp (POST /v1/responses)
   b. runtimeError.events.length > 0 (有部分事件):
      -> 转发已收到的事件
      -> 检查是否包含 terminal event (response.completed/failed/incomplete)
      -> 没有 terminal event -> 发送 408 error frame
      -> 有 terminal event -> 不发送额外错误 (即使 runtime 报错)
```

### HTTP Fallback 路径 (`forwardResponsesRequestViaHttp`)
```
1. 构建 inject headers (WS 请求的原始 headers, 去掉 WS 相关 headers)
2. 设置内部 headers:
   - x-metapi-responses-websocket-transport: "1"
   - x-metapi-responses-websocket-mode: "incremental" (如果适用)
3. 通过 app.inject("POST", "/v1/responses", headers, payload) 内部代理
4. Content-Type 检查:
   - 非 SSE: 解析 JSON, 直接发送 + 收集 output
   - SSE: 通过 openAiResponsesTransformer.pullSseEvents 解析, 逐 event 转发
   - 没有 terminal event -> 发送 408 error frame
```

### Pre-warm 本地合成 (`synthesizePrewarmResponsePayloads`)
```
触发条件: lastRequest == null && type == "response.create" && generate == false && !supportsIncrementalInput

合成两个事件, 不调任何上游:
1. response.created:
   {
     type: "response.created",
     response: {
       id: "resp_prewarm_<uuid>",
       object: "response",
       created_at: <now_sec>,
       status: "in_progress",
       model: <modelName>,
       output: []
     }
   }
2. response.completed:
   {
     type: "response.completed",
     response: {
       id: "resp_prewarm_<uuid>",
       object: "response",
       created_at: <now_sec>,
       status: "completed",
       model: <modelName>,
       output: [],
       usage: { input_tokens: 0, output_tokens: 0, total_tokens: 0 }
     }
   }
```

### Output 收集 (`collectResponsesOutput`)
从 WS events 中聚合 output 数组, 按优先级:
1. `response.output_item.added/done`: 按 output_index 收集
2. `response.completed/failed/incomplete` 的 `response.output` 数组
3. `response.completed/failed/incomplete` 的 `response.output_text` 字符串 -> 构造 message item
4. 裸 `output` 数组
5. 裸 `output_text` 字符串
6. 如果 collected 为空, 使用按 output_index 排序的 map 值

### Session 清理
- 每个 socket close: `codexWebsocketRuntime.closeSession(sessionKey)` per sessionKey
- App shutdown: `codexWebsocketRuntime.closeAllSessions()` + WebSocketServer.close()

### 内部 Headers
| Header | 值 | 用途 |
|--------|-----|------|
| `x-metapi-responses-websocket-mode` | `"incremental"` | 标记 incremental input 模式, HTTP forward 路径读取 |
| `x-metapi-responses-websocket-transport` | `"1"` | 标记请求来源为 WS transport, HTTP forward 路径读取 |
| `x-codex-turn-state` | (回显) | Codex turn state, WS upgrade 响应 headers 中回显 |

## Multipart 文件上传

### Multipart Buffer Parser
需要 multipart 的 surfaces (images/edits, videos) 在路由注册时调用 `ensureMultipartBufferParser(app)`:
```go
// 注册 content-type parser, 将 multipart/form-data 解析为 raw buffer
app.AddContentTypeParser("multipart/form-data", func(body []byte) {
    return body // raw bytes
})
```

### Multipart 请求处理
```go
func parseMultipartFormData(r *http.Request) (*multipart.Form, error) {
    contentType := r.Header.Get("Content-Type")
    if !strings.HasPrefix(contentType, "multipart/form-data") {
        return nil, nil // 非 multipart, 回退到 JSON
    }
    // 解析 multipart form
    return r.MultipartForm()
}
```

### FormData 操作
- `cloneFormDataWithOverrides(form, overrides map[string]string)`: 复制 FormData, 替换 overrides 中的字段值 (如替换 model 为 upstream model), 保留文件 entries.
- Images edits 和 Videos POST 都支持双模式: 优先尝试 multipart, 如果没有 multipart 数据则 fallback 到 JSON body.

## Edge Cases 与错误处理

### 请求验证
- body 中 `model` 缺失 -> 400 `{"error":{"message":"model is required","type":"invalid_request_error"}}`
- search `query` 为空 -> 400 `{"error":{"message":"query is required","type":"invalid_request_error"}}`
- search `stream=true` -> 400 `{"error":{"message":"search does not support streaming","type":"invalid_request_error"}}`
- search `max_results` 无效 -> 400 `{"error":{"message":"max_results must be an integer between 1 and 20","type":"invalid_request_error"}}`
- unknown `/responses/*` 子路径 -> 404 `{"error":{"message":"Unknown /responses alias path","type":"invalid_request_error"}}`
- GET `/v1/responses` 或 GET `/responses*` -> 426 (WebSocket upgrade required)
- `/v1/images/variations` -> 400 (Not supported)

### 上游错误
- 上游返回非 200 -> 透传错误到下游, 保留原始 error 格式
- 上游不可达 -> 502 `{"error":{"message":"Upstream error: <details>","type":"upstream_error"}}`
- Token 过期检测 (status > 0 时): 触发 `reportTokenExpired` alert
- 根据 `shouldRetryProxyRequest` 判断是否重试下一个 channel
- Channel selection 重试: `canRetryChannelSelection(retryCount, forcedChannelID)`

### SSE Streaming
- SSE headers 必须包含: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
- 流式响应中: 逐 chunk flush, 累积 SSE data events 解析 usage
- 非 200 状态的流式响应: 读取 error text, 按错误处理

### WebSocket
- WS 升级认证失败: 发送 HTTP 错误 (401/403) 后关闭 socket (不升级)
- WS 消息解析失败: 发送 error frame (400)
- WS runtime 零事件错误: HTTP fallback (POST /v1/responses)
- WS runtime 部分事件 + 无 terminal event: 发送已有事件 + 408 error frame
- WS runtime 部分事件 + 有 terminal event: 发送已有事件, 不发送额外错误
- Managed key 每个 WS 消息消费一次配额 (`consumeManagedKeyRequest`)

### Pre-warm (generate=false)
- Codex 客户端发送 `response.create` 且 `generate: false`, 且 channel 不支持 incremental input 时
- **本地合成** response.created + response.completed, 零 token usage, 不调上游
- 这是 Codex file-search pre-warm 模式的优化

### Service Tier Policy
- WebSocket 路径中应用两次: 一次在归一化前 (基于原始 body), 一次在 channel 选择后 (基于 actualModel/platform/accountType)
- 不通过 -> error frame, 不继续处理

### Gemini 特殊处理
- 非 Gemini-family 平台 + countTokens action -> 501
- Gemini CLI 平台 + 缺失 OAuth project -> 500
- Gemini-family 平台 401 -> OAuth token refresh (refreshOauthAccessTokenSingleflight), 重试
- Gemini models 列表: 根据 downstream policy 过滤

### Videos 任务映射
- POST 创建时保存 publicId -> upstreamVideoId mapping
- GET/DELETE 按 publicId 查找 mapping, 代理到上游的 upstreamVideoId
- mapping 不存在 -> 404

## Acceptance Criteria
- [ ] 13 个代理表面全部实现, 与 TS 版 endpoint 一一对应 (含所有 route alias + gemini CLI internal paths)
- [ ] Chat surface: `/v1/chat/completions` 和 `/chat/completions` 均传递 surface format `"openai"`
- [ ] Claude surface: `/v1/messages` 传递 surface format `"claude"`, `/v1/messages/count_tokens` 独立 handler
- [ ] Responses surface: POST `/v1/responses` 和 `/v1/responses/compact` 传递完整路径作为下游路径参数
- [ ] Responses aliases: `/responses` 和 `/responses/compact` 正确映射, 未知子路径返回 404
- [ ] GET `/v1/responses` 和 GET `/responses*` 均返回 426 WebSocket upgrade required
- [ ] Models endpoint: 根据 `anthropic-version`/`x-api-key` header 决定返回 OpenAI 或 Claude 格式
- [ ] Images edits + Videos: 支持 multipart/form-data 和 JSON body 双模式
- [ ] Images variations: 返回 400 not supported
- [ ] Search: query 必填, stream 拒绝, max_results 校验
- [ ] Gemini: 7 个路由全部实现, 包括 3 个 Gemini CLI internal paths
- [ ] Client detection: 返回结构化 DownstreamClientContext (6 字段), 不返回不存在的 `"openai"` kind
- [ ] Client kind identifiers: `"generic"`, `"codex"`, `"claude_code"`, `"gemini_cli"` (使用下划线, 非连字符)
- [ ] Streaming 端点: 正确设置 SSE headers (含 `Connection: keep-alive`), 逐 chunk flush
- [ ] 非 streaming 端点: 收集完整响应后返回 JSON, 检测 proxy failure
- [ ] WebSocket: 完整实现 upgrade 握手、消息序列化、Codex WS runtime、HTTP fallback、pre-warm 合成、service tier policy
- [ ] Multipart: `ensureMultipartBufferParser` 注册, `parseMultipartFormData` 解析, `cloneFormDataWithOverrides` 替换
- [ ] 请求体验证: model 必填检查 (chat/completions/embeddings/videos), search 参数校验
- [ ] Channel retry: 失败自动重试下一个 channel, 受 `getProxyMaxChannelRetries()` 限制
- [ ] Token 过期: 检测并 reportTokenExpired
- [ ] Proxy log: 每次请求/重试写 proxy_log (含 client context 所有字段)
- [ ] 错误响应格式与 OpenAI 兼容 (`{"error":{"message":"...","type":"..."}}`)

## Test Plan
| 文件 | 内容 |
|------|------|
| `handler/proxy/chat_test.go` | Chat endpoint E2E (mock upstream), surface format `"openai"` 验证 |
| `handler/proxy/chat_stream_test.go` | SSE streaming E2E, Connection: keep-alive 验证 |
| `handler/proxy/messages_test.go` | Claude messages + count_tokens |
| `handler/proxy/completions_test.go` | Completions streaming + non-streaming |
| `handler/proxy/responses_test.go` | Responses POST + compact + GET 426 + alias 404 |
| `handler/proxy/responses_ws_test.go` | WebSocket upgrade + message flow + pre-warm + HTTP fallback |
| `handler/proxy/models_test.go` | /v1/models dual format (OpenAI + Claude) |
| `handler/proxy/embeddings_test.go` | Embeddings POST + model 验证 |
| `handler/proxy/images_test.go` | Images gen + edits (multipart/JSON) + variations 400 |
| `handler/proxy/videos_test.go` | Videos CRUD + publicId mapping |
| `handler/proxy/search_test.go` | Search query/max_results 验证 |
| `handler/proxy/gemini_test.go` | Gemini models list + generateContent + CLI internal paths |
| `handler/proxy/files_test.go` | 文件上传/下载 |
| `handler/proxy/client_detect_test.go` | Client kind detection (all 4 kinds + app fingerprinting) |
| `handler/proxy/multipart_test.go` | Multipart 解析 + FormData clone |
