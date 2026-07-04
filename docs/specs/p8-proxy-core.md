# P8: ProxyCore — 代理编排核心

**S.U.P.E.R**: S (单一职责) · P (端口优先) | **依赖**: P7 | **Size**: L

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\proxy-core\conductor\DefaultProxyConductor.ts`
- `D:\Code\TokenDance\metapi\src\server\proxy-core\channelSelection.ts`
- `D:\Code\TokenDance\metapi\src\server\proxy-core\orchestration\endpointFlow.ts`
- `D:\Code\TokenDance\metapi\src\server\proxy-core\executors\` — runtime executors
- `D:\Code\TokenDance\metapi\src\server\proxy-core\providers\`
- `D:\Code\TokenDance\metapi\src\server\proxy-core\surfaces\` — chatSurface, sharedSurface
- `D:\Code\TokenDance\metapi\src\server\proxy-core\runtime\`
- `D:\Code\TokenDance\metapi\src\server\proxy-core\cliProfiles\` — codex/claude/gemini/generic/claude-code
- `D:\Code\TokenDance\metapi\src\server\services\proxyChannelCoordinator.ts`
- `D:\Code\TokenDance\metapi\src\server\services\proxyChannelRetry.ts`
- `D:\Code\TokenDance\metapi\src\server\services\proxyFailureJudge.ts`

## Go 模块结构
```
proxy/
  conductor.go          # ProxyConductor: 主编排器
  endpoint_flow.go      # ExecuteEndpointFlow: 单次请求全流程
  channel_retry.go      # ChannelRetry: 重试/退避策略
  failure_judge.go      # FailureJudge: 错误分类 (是否应重试/降级)
  executor.go           # RuntimeExecutor: HTTP 请求执行
  surface.go            # SharedSurface: 会话/通道簿记
  session.go            # SessionLeaseManager: 会话级通道租约
  profile.go            # CLIProfile 接口
  profiles/
    codex.go            # Codex CLI profile
    claude.go           # Claude CLI profile
    gemini.go           # Gemini CLI profile
    generic.go          # 通用 OpenAI profile
    claude_code.go      # Claude Code profile
```

## 功能规格

### ExecuteEndpointFlow — 核心流程
```
1. Channel Selection (调用 P7 TokenRouter)
   selectedChannel = tokenRouter.SelectChannel(model, sessionID, downstreamPolicy)
   
2. Session Lease (如启用)
   SessionLeaseManager.Acquire(sessionID, selectedChannel)
   等待队列 (concurrency limit + queue wait ms)

3. Request Building
   - 根据 CLI profile 构建请求
   - 注入 OAuth headers (如需)
   - 应用 payload rules
   - Site proxy 配置

4. Execution
   executor.Execute(ctx, targetURL, request)
   - first_byte_timeout (PROXY_FIRST_BYTE_TIMEOUT_SEC)
   - SSE stream: 逐 chunk 读取, 应用 transformer

5. Result Handling
   Success:
     - tokenRouter.RecordSuccess(channel)
     - 写 proxy_log (success)
     - 写 proxy_debug_trace (如启用)
     - 更新 billing counters
   Failure:
     - FailureJudge.Classify(error) → retryable/downgrade/fatal
     - tokenRouter.RecordFailure(channel)
     - 如果 retryable 且 attempts < PROXY_MAX_CHANNEL_ATTEMPTS:
       → tokenRouter.SelectNextChannel(previousChannel)
       → 重试 (最多 3 次)
     - 如果 downgrade:
       → 尝试降级路径 (responses compact / cross-protocol fallback)
     - 写 proxy_log (failed)
     - 触发告警

6. Session Lease Release
```

### Session Lease Manager
```go
type SessionLeaseManager struct {
    concurrencyLimit int     // PROXY_SESSION_CHANNEL_CONCURRENCY_LIMIT (2)
    queueWaitMs      int     // 1500
    leaseTTLMs       int     // 90000
    keepaliveMs      int     // 15000
}
// 同一 session + channel 最多 concurrencyLimit 个并发请求
// 超出排队等待 queueWaitMs
```

### CLI Profiles
5 个 CLI profile 处理不同客户端的请求特征:
- **Codex**: `x-codex-*` headers, 特殊的 response format
- **Claude**: `anthropic-version`, `x-api-key`
- **Gemini CLI**: OAuth bearer token
- **Generic**: 标准 OpenAI headers
- **Claude Code**: 特殊的 tool use / streaming 格式

### Failure Classification
```
FailureJudge.Classify(error, response):
  - HTTP 4xx (非 429) → 不重试 (fatal)
  - HTTP 429 → 重试 (retryable, 等待 Retry-After)
  - HTTP 5xx → 重试 (retryable)
  - 连接错误/超时 → 重试 (retryable)
  - 空响应 body → 如果 PROXY_EMPTY_CONTENT_FAIL=true → 重试
  - 响应含 PROXY_ERROR_KEYWORDS → 重试
  - 协议转换失败 → 降级尝试 (downgrade)
```

## Acceptance Criteria
- [ ] endpoint flow 完整工作: select → execute → record → retry/downgrade
- [ ] 最多重试 PROXY_MAX_CHANNEL_ATTEMPTS (3) 次
- [ ] Session lease 并发限制正确
- [ ] 5 个 CLI profile 正确处理各自请求头/响应格式
- [ ] Failure judge 正确分类 429/5xx/connection/timeout
- [ ] 重试时排除已失败的 channel
- [ ] Proxy log 写入正确 (success/failed/retried)
- [ ] Debug trace (如启用) 记录完整决策链

## Test Plan
| 文件 | 内容 |
|------|------|
| `proxy/endpoint_flow_test.go` | 完整 flow + mock |
| `proxy/channel_retry_test.go` | 重试次数/退避 |
| `proxy/failure_judge_test.go` | 各 HTTP 状态码分类 |
| `proxy/session_test.go` | 租约获取/释放/超时 |
| `proxy/profiles/*_test.go` | 各 profile 的请求构建 |

## Edge Cases
- 所有 channel 尝试失败 → 返回最终错误给客户端
- Session lease 等待超时 → 返回 429 Too Many Requests
- 首次 channel 失败, 重试成功 → proxy_log.status = "retried"
- Downgrade 也失败 → 返回原始错误
- 流式响应中途断开 → 已发送的 chunk 不收回, log 标记 failed
