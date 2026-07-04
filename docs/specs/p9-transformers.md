# P9: Protocol Transformers — OpenAI↔Anthropic↔Gemini↔Codex Canonical + SSE Stream

**S.U.P.E.R**: S (单一职责) · U (单向流) · P (端口优先) · E (环境无关) · R (可替换) | **依赖**: P8 | **Size**: XL

> ✅ **S.U.P.E.R 全绿模块** — 这是 TS 版中架构最好的子系统。Go 版以此为模板，保持纯函数 + 零外部依赖。

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\transformers\canonical\` — 标准中间格式
- `D:\Code\TokenDance\metapi\src\server\transformers\openai\chat\` — OpenAI chat completions
- `D:\Code\TokenDance\metapi\src\server\transformers\openai\responses\` — OpenAI responses
- `D:\Code\TokenDance\metapi\src\server\transformers\anthropic\messages\` — Anthropic messages
- `D:\Code\TokenDance\metapi\src\server\transformers\gemini\generate-content\` — Gemini
- `D:\Code\TokenDance\metapi\src\server\transformers\shared\` — 共享工具
- `D:\Code\TokenDance\metapi\src\server\proxy-core\capabilities\responsesCompact.ts`

## Go 模块结构
```
transform/
  canonical/
    types.go             # Canonical 中间格式 struct 定义
    stream.go            # Canonical SSE chunk struct
  openai/
    chat/
      request.go         # ChatCompletionRequest → CanonicalRequest
      response.go        # CanonicalResponse → ChatCompletionChunk (SSE)
      stream.go          # SSE decoder/encoder
    responses/
      request.go         # ResponsesRequest → CanonicalRequest
      response.go        # CanonicalResponse → ResponsesChunk (SSE)
      compact.go         # responses compact optimization
    completions/
      request.go
      response.go
    embeddings/
      request.go
      response.go
    images/
      request.go
      response.go
    shared/
      tokenizer.go       # token 计数估算
      mapper.go          # 模型名映射
  anthropic/
    messages/
      request.go         # MessagesRequest → CanonicalRequest
      response.go        # CanonicalResponse → MessageStreamEvent (SSE)
      content_block.go   # Content block 类型处理
    shared/
      tokenizer.go
  gemini/
    generate_content/
      request.go         # GenerateContentRequest → CanonicalRequest
      response.go        # CanonicalResponse → GenerateContentResponse (SSE)
    shared/
      tokenizer.go
  codex/
    responses/
      request.go         # Codex responses 格式处理
      response.go
    shared/
      tokenizer.go
```

## 功能规格

### 转换流程
```
Downstream Client → [OpenAI/Anthropic/Gemini/Codex Request]
    ↓
Request Transformer (下游格式 → CanonicalRequest)
    ↓
[ProxyCore: Channel Selection → Upstream Execution]
    ↓
Response Transformer (Upstream SSE → CanonicalStreamChunk → 下游格式 SSE)
    ↓
Downstream Client ← [OpenAI/Anthropic/Gemini/Codex SSE Response]
```

### Canonical 中间格式
```go
type CanonicalRequest struct {
    Model    string
    Messages []CanonicalMessage  // role + content (text/image_url/...)
    Stream   bool
    MaxTokens int
    Temperature *float64
    TopP     *float64
    Tools    []CanonicalTool     // function definitions
    ToolChoice interface{}
    // ... 其他通用字段
}

type CanonicalMessage struct {
    Role    string // "system" | "user" | "assistant" | "tool"
    Content []CanonicalContentPart
}

type CanonicalContentPart struct {
    Type     string // "text" | "image_url" | "tool_use" | "tool_result"
    Text     string
    ImageURL *ImageURL
    ToolUse  *ToolUse
    ToolResult *ToolResult
}

type CanonicalStreamChunk struct {
    Index   int
    Delta   *CanonicalDelta
    FinishReason *string
    Usage   *UsageInfo
}

type CanonicalDelta struct {
    Role    *string
    Content *string
    ToolCalls []ToolCallDelta
}
```

### SSE Stream 处理
```go
// 上游 SSE → canonical chunks
type CanonicalDecoder struct {
    scanner *bufio.Scanner  // 按行读取 SSE
    format  UpstreamFormat  // openai / anthropic / gemini / codex
}
func (d *CanonicalDecoder) Decode() (*CanonicalStreamChunk, error)

// canonical chunks → 下游 SSE
type SSEEncoder struct {
    writer  io.Writer
    format  DownstreamFormat
}
func (e *SSEEncoder) Encode(chunk *CanonicalStreamChunk) error
```

响应处理: 每个 canonical chunk 在从 decoder 读取后立即通过 encoder 写入下游 — 零缓冲, 实时流式。

### Anthropic → OpenAI 转换 (最复杂路径, 示例)
```
Anthropic Request:
  {model, system (string), messages[{role,content[{type,text|image|tool_use|tool_result}]}],
   tools[{name,description,input_schema}], tool_choice, max_tokens, stream:true}

→ CanonicalRequest:
  {messages[0].role=system, messages[1+]=Anthropic messages,
   tools, tool_choice, max_tokens, stream:true}

→ OpenAI Response (SSE):
  data: {"id":"...","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}
  data: {"id":"...","choices":[{"index":0,"delta":{"content":"Hello"}}]}
  data: {"id":"...","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"...","type":"function","function":{"name":"...","arguments":"..."}}]}}]}
  data: {"id":"...","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{...}}
  data: [DONE]

Anthropic SSE input format:
  event: message_start / content_block_start / content_block_delta / content_block_stop / message_delta / message_stop / ping
```

### Responses Compact (Codex 优化)
当上游是 Codex 且下游需要 compact responses:
1. 收集完整 response
2. 按 conversation 语义重新组织
3. 合并冗余 content blocks
4. 减少 chunk 数量

## Acceptance Criteria (严格! 这是最高风险模块)
- [ ] OpenAI Chat → Canonical → OpenAI Chat: roundtrip 无损
- [ ] Anthropic Messages → Canonical → Anthropic Messages: roundtrip 无损
- [ ] OpenAI Chat → Canonical → Anthropic SSE: 正确逐 chunk 转换
- [ ] Anthropic SSE → Canonical → OpenAI SSE: 正确逐 chunk 转换
- [ ] Gemini GenerateContent → Canonical → OpenAI SSE: 正确转换
- [ ] Codex Responses → Canonical → OpenAI Responses: 正确转换
- [ ] 所有 SSE chunk 格式: `data: {...}\n\n` 并以 `data: [DONE]\n\n` 结束
- [ ] Tool calls 跨协议正确保留 (Anthropic tool_use ↔ OpenAI function call)
- [ ] Content blocks with images 正确传递
- [ ] Token usage 统计正确映射到下游格式
- [ ] Responses compact 优化正确工作
- [ ] 流式中途断开 → 不写损坏的 chunk, 发送 error event

## Test Plan (Golden File 测试!)
| 文件 | 内容 |
|------|------|
| `transform/canonical/types_test.go` | struct 序列化/反序列化 |
| `transform/openai/chat/roundtrip_test.go` | OpenAI ↔ canonical, golden files |
| `transform/openai/responses/roundtrip_test.go` | Responses ↔ canonical |
| `transform/anthropic/messages/roundtrip_test.go` | Anthropic ↔ canonical, golden files |
| `transform/gemini/roundtrip_test.go` | Gemini ↔ canonical |
| `transform/codex/roundtrip_test.go` | Codex ↔ canonical |
| `transform/*/stream_test.go` | SSE decoder/encoder golden files |
| `testdata/openai_chat_request.json` | Golden: OpenAI chat request |
| `testdata/anthropic_message_stream.txt` | Golden: Anthropic SSE stream |
| `testdata/canonical_request.json` | Golden: canonical format |

## Edge Cases
- SSE line 不以 `data:` 开头 → 跳过 (comment/event line)
- Anthropic `content_block_stop` 不产生 output chunk
- Gemini 用 `\r\n\r\n` 作为事件分隔符 → 正确处理 CRLF
- Tool call 的 arguments 是多 chunk 增量 (JSON 拼接)
- 上游返回非流式响应 (stream=false) → 包装为单 chunk SSE
- 空 content block → 不生成 chunk (Anthropic thinking 块)
