# P7: TokenRouter — 路由选择引擎

**S.U.P.E.R**: S (拆分!) · U (打破循环!) · P (接口契约!) · R (可替换!) | **依赖**: P3 + P4 | **Size**: XL

> ⚠️ **S.U.P.E.R 关键修复点**: TS 版 tokenRouter.ts 3800 行单文件 + modelService 循环依赖。
> Go 版必须拆分为独立模块 + 接口契约 + 单向依赖。

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\services\tokenRouter.ts` (~3800 LOC, 最大单体)
- `D:\Code\TokenDance\metapi\src\server\services\routeRoutingStrategy.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeCooldownService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeRefreshWorkflow.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeDecisionRefreshService.ts`
- `D:\Code\TokenDance\metapi\src\server\services\routeDecisionSnapshotStore.ts`
- `D:\Code\TokenDance\metapi\src\server\services\modelService.ts` (循环依赖)
- `D:\Code\TokenDance\metapi\src\server\services\modelPricingService.ts`

## Go 模块结构 (拆分!)
```
routing/
  router.go              # TokenRouter: 主入口, 组合以下模块
  selector.go            # ChannelSelector: selectChannel, selectNextChannel
  matcher.go             # ModelMatcher: 模型名 regex/glob 匹配 token_routes
  cooldown.go            # CooldownManager: 失败冷却/断路器
  weights.go             # WeightCalculator: 加权选择算法
  cache.go               # RouteCache: token_router_cache_ttl_ms 短期缓存
  circuit_breaker.go     # CircuitBreaker: site 级断路器
  sticky.go              # StickySession: session→channel 粘滞
  workflow.go            # RouteRefreshWorkflow: rebuildRoutes, refreshModels
  decision.go            # RouteDecisionService: 路由决策查询/批量/刷新
  snapshot.go            # RouteDecisionSnapshotStore: 决策快照持久化
  pricing.go             # PricingReference: 模型定价参考

  ports.go               # 接口契约!
    type ModelProvider interface {
        GetAvailableModels(ctx, accountID) ([]ModelInfo, error)
        RefreshModelsForAccount(ctx, accountID) error
    }
    type TokenProvider interface {
        GetTokens(ctx, accountID) ([]Token, error)
        GetDefaultToken(ctx, accountID) (*Token, error)
    }
    type PricingProvider interface {
        GetReferenceCost(ctx, model, siteID) (float64, error)
    }
```

## 核心功能规格

### Channel Selection 流程
```
SelectChannel(model, sessionID, downstreamPolicy):
  1. Cache lookup (token_router_cache_ttl_ms)
  2. Model matching: 查 token_routes WHERE model_pattern matches 请求模型
  3. 如果没有匹配路由 → 创建一个 default route
  4. 获取候选 channels (route_channels):
     - channel.enabled = true
     - account.status = active
     - site.status = active
     - 排除 excludedSiteIds (来自 downstreamPolicy)
     - 排除 excludedCredentialRefs
  5. Filter:
     - CooldownManager.IsCooledDown(channel) → skip
     - CircuitBreaker.IsOpen(site) → skip
     - 使用 siteRuntimeHealth 降低不可用站点的权重
  6. Score & Rank:
     - 每个 channel 计算 weighted score:
       weight_score = channel.weight × config.BaseWeightFactor
       value_score = account.value_score × config.ValueScoreFactor
       cost_score = (1 / unit_cost) × config.CostWeight (越低越好)
       balance_score = (balance / quota) × config.BalanceWeight
       usage_score = (1 - usage_trend) × config.UsageWeight
       总分 = weight + value + cost + balance + usage
     - 乘以 siteWeightMultipliers (来自 downstreamPolicy)
     - 乘以 siteRuntimeHealthMultiplier
  7. 选择得分最高的 channel
  8. 如果 sticky session 启用 → 检查 session 有无已绑定 channel, TTL 内复用
  9. Cache 结果
  10. 返回 channel

SelectNextChannel(previousChannel, model, sessionID):
  - 调用 SelectChannel 但排除 previousChannel
  - 用于重试场景
```

### Weight Calculation
```go
func CalculateWeight(channel Channel, account Account, cfg RoutingWeights, downstreamMultiplier float64, healthMultiplier float64) float64
```

### Cooldown / Circuit Breaker
```
RecordSuccess(channel):
  - consecutiveFailCount = 0, cooldownLevel = 0

RecordFailure(channel):
  - consecutiveFailCount++
  - cooldownLevel = min(consecutiveFailCount, maxCooldownLevel)
  - cooldownUntil = now + min(backoff^cooldownLevel, TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC)
  - 如果 site 的全局失败率 > 阈值 → open circuit breaker

isChannelCooledDown(channel):
  - 检查 cooldownUntil > now

isSiteBreakerOpen(site):
  - 断路器打开 → 所有 channel 跳过
  - 30s 后尝试 half-open (放行一个请求探测)
```

### Route Refresh Workflow
```
RebuildRoutesOnly():
  1. 删除所有自动生成的 route_channels (manual_override=false)
  2. 遍历所有 token_routes:
     - 对每个 route, 匹配所有 account → 创建 route_channels
  3. OAuth route units: 按 strategy 创建 channel
  4. 清除路由缓存

RefreshModelsAndRebuildRoutes():
  1. 对所有 active 账号并发调用 adapter.GetModels
  2. 更新 model_availability + token_model_availability
  3. RebuildRoutesOnly()
```

## Acceptance Criteria (严格!)
- [ ] selectChannel 返回的 channel 加权分数 ≥ TS 版相同输入的计算结果
- [ ] cooldown 指数退避正确: fail 1→cooldown 1, fail 2→cooldown 2, ... ceiling 30d
- [ ] 断路器 half-open 状态正确探测
- [ ] sticky session: 同一 sessionID 在 TTL 内返回相同 channel
- [ ] 缓存: 同一 model 在 ttl 内不重复计算
- [ ] Route rebuild 不删除 manual_override=true 的 channel
- [ ] DownstreamPolicy excludedSiteIDs / excludedCredentialRefs 正确排除
- [ ] 与 modelService 无循环依赖 (通过 ModelProvider 接口)

## Test Plan (高覆盖!)
| 文件 | 内容 |
|------|------|
| `routing/selector_test.go` | selectChannel 完整流程, 含 mock 依赖 |
| `routing/selector_compare_test.go` | 与 TS 版相同输入 → 相同选择结果 (golden file) |
| `routing/matcher_test.go` | 50+ 个模型名 pattern 匹配测试 |
| `routing/cooldown_test.go` | 指数退避, ceiling, 清零逻辑 |
| `routing/circuit_breaker_test.go` | 打开/关闭/half-open 状态机 |
| `routing/weights_test.go` | 加权计算, multiplier 叠加 |
| `routing/cache_test.go` | TTL, 缓存键, 失效 |
| `routing/workflow_test.go` | Route rebuild, model refresh |

## Edge Cases
- 所有 channel 都在 cooldown → 返回错误, 不选任何 channel
- 所有 channel 被 excludedSiteIDs 过滤 → 返回错误
- 0 个 token_routes → 创建默认 route
- Weight 全 0 → 按 priority 排序返回第一个
- Cache TTL 内同一 session → 使用相同 channel (即使权重变)
