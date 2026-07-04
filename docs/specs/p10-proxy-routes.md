# P10: Proxy Routes (/v1/*)

**S.U.P.E.R**: S (单一职责) · U (单向流) | **依赖**: P7 + P8 + P9 | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\router.ts` — 代理路由注册
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\chat.ts` — chat + messages
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\completions.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\responses.ts` + `responsesWebsocket.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\embeddings.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\images.ts` + `images/edits.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\models.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\files.ts` + `inputFiles.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\videos.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\gemini.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\search.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\multipart.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\upstreamEndpoint.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\upstreamError.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\downstreamPolicy.ts`
- `D:\Code\TokenDance\metapi\src\server\routes\proxy\proxyBilling.ts`

## Go 模块结构
```
handler/proxy/
  router.go          # RegisterProxyRoutes(chi.Router)
  chat.go            # POST /v1/chat/completions + /chat/completions
  messages.go        # POST /v1/messages + /v1/messages/count_tokens
  completions.go     # POST /v1/completions
  responses.go       # POST /v1/responses + GET + compact + Codex passthrough
  responses_ws.go    # WebSocket: Codex responses via WS
  embeddings.go      # POST /v1/embeddings
  images.go          # POST /v1/images/generations + edits + variations
  models.go          # GET /v1/models
  files.go           # POST/GET files (upload/resolve)
  input_files.go     # 文件输入解析
  videos.go          # POST /v1/videos + GET/POST/DELETE :id
  gemini.go          # Gemini native surface
  search.go          # POST /v1/search
  multipart.go       # multipart/form-data 解析
```

## 端点清单 (11 表面)

| 表面 | Method | Path | 说明 |
|------|--------|------|------|
| OpenAI Chat | POST | `/v1/chat/completions` | OpenAI 兼容 chat |
| OpenAI Chat (alt) | POST | `/chat/completions` | 备用路径 |
| Claude Messages | POST | `/v1/messages` | Anthropic 原生 |
| Claude Count | POST | `/v1/messages/count_tokens` | Token 计数 |
| Completions | POST | `/v1/completions` | 传统 completions |
| Responses | POST | `/v1/responses` | OpenAI responses |
| Responses | GET | `/v1/responses` | 获取 response |
| Responses Compact | POST | `/v1/responses/compact` | Compact 优化 |
| Codex Passthrough | POST/GET | `/responses*` | Codex 原生直通 |
| Models | GET | `/v1/models` | 模型列表 |
| Embeddings | POST | `/v1/embeddings` | 向量嵌入 |
| Images Gen | POST | `/v1/images/generations` | 图片生成 |
| Images Edit | POST | `/v1/images/edits` | 图片编辑 |
| Images Var | POST | `/v1/images/variations` | 图片变体 |
| Videos | POST | `/v1/videos` | 视频生成 |
| Videos | GET/POST/DELETE | `/v1/videos/:id` | 视频状态/取消 |
| Files | POST | `/v1/files` | 文件上传 |
| Files | GET | `/v1/files/:id` | 文件下载 |
| Search | POST | `/v1/search` | 搜索 |
| Gemini | — | Gemini native 格式 | — |

## Handler 模式 (thin handler)
每个 handler 文件遵循此模式:
```go
func HandleChat(w http.ResponseWriter, r *http.Request) {
    // 1. 从 context 取 ProxyAuthContext
    auth := GetProxyAuth(r.Context())
    
    // 2. 解析下游请求格式 (OpenAI ChatCompletionRequest)
    var reqBody openai.ChatCompletionRequest
    json.NewDecoder(r.Body).Decode(&reqBody)
    
    // 3. 判断请求来源 (client family detection)
    clientKind := detectClientKind(r)
    
    // 4. 调用 P9 Transformer: 下游格式 → Canonical
    canonicalReq := openai.ChatRequestToCanonical(&reqBody)
    
    // 5. 调用 P8 ProxyCore: execute endpoint flow
    result := proxy.ExecuteEndpointFlow(r.Context(), proxy.EndpointFlowInput{
        Model:           reqBody.Model,
        CanonicalReq:    canonicalReq,
        SessionID:       getSessionID(r),
        DownstreamPolicy: auth.Policy,
        ClientKind:      clientKind,
        IsStream:        reqBody.Stream,
        RouteAlias:      "chat",
    })
    
    // 6. 调用 P9 Transformer: Upstream SSE → 下游 SSE
    if reqBody.Stream {
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        flusher := w.(http.Flusher)
        
        decoder := openai.NewChatSSEDecoder(result.Body, result.UpstreamFormat)
        encoder := openai.NewChatSSEEncoder(w)
        
        for chunk := range decoder.DecodeAll(r.Context()) {
            encoder.Encode(chunk)
            flusher.Flush()
        }
    } else {
        // 非流式: 收集所有 chunks → 组装完整响应
    }
    
    // 7. 写 proxy_log + billing (在 proxy-core 中完成)
}
```

### Client Detection
```
detectClientKind(request):
  - 检查 User-Agent / headers → "codex" / "claude-code" / "gemini-cli" / "openai" / "generic"
  - 用于 CLI profile 选择和 debug trace 标记
```

### Route Aliases
```
/chat/completions → alias "chat" (OpenAI surface)
/v1/messages → alias "claude" (Anthropic surface)
/v1/responses → alias "responses" (Codex surface)
```

## Acceptance Criteria
- [ ] 11 个代理表面全部实现，与 TS 版 endpoint 一一对应
- [ ] Streaming 端点: 正确设置 SSE headers, 逐 chunk flush
- [ ] 非 streaming 端点: 收集完整响应后返回 JSON
- [ ] WebSocket (responses) 正确升级连接
- [ ] Multipart 文件上传正确解析
- [ ] Content-Type 协商: 下游 → 上游保持一致
- [ ] 错误响应格式与 OpenAI 兼容

## Test Plan
| 文件 | 内容 |
|------|------|
| `handler/proxy/chat_test.go` | Chat endpoint E2E (mock upstream) |
| `handler/proxy/chat_stream_test.go` | SSE streaming E2E |
| `handler/proxy/models_test.go` | /v1/models 返回格式 |
| `handler/proxy/files_test.go` | 文件上传/下载 |
| `handler/proxy/client_detect_test.go` | Client kind detection |

## Edge Cases
- 请求 body 为空 → 400
- Stream=true 但客户端不支持 SSE → 降级返回非流式
- 上游返回非 200 → 透传错误到下游
- Websocket 握手失败 → 回退到 HTTP streaming
