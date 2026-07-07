# Risk Assessment

## S.U.P.E.R Architecture Health Summary

| Principle | Status | Key Findings | Transformation Priority |
|:----------|:-------|:-------------|:------------------------|
| **S** Single Purpose | 🟡 | tokenRouter (3800 LOC 单文件) 和 modelService 职责过载; 其他模块合理 | **High** — Go 重写时必须拆分 |
| **U** Unidirectional Flow | 🟡 | modelService ↔ tokenRouter 存在循环依赖; proxy-core ↔ tokenRouter 双向 | **High** — Go 设计时必须通过接口打破循环 |
| **P** Ports over Implementation | 🟠 | 无显式接口契约; 模块间无 schema-validated 边界; tokenRouter 直接 DB + 内存操作 | **High** — Go 架构核心改进点 |
| **E** Environment-Agnostic | 🟢 | 大部分模块通过 config/DI 隔离环境; siteProxy 的 node:dns/node:tls 是唯一硬依赖 | **Low** — Go 标准库天然支持 |
| **R** Replaceable Parts | 🟡 | platforms/ 和 transformers/ 模块符合; tokenRouter/modelService 替换成本极高 | **Medium** — 接口化后改善 |

**Overall Health**: _2/5 principles healthy_ — **Technical Debt Alert**。tokenRouter + modelService + proxy-core 形成了高耦合核心 (S🟡 U🟡 P🟠 R🟠)。Go 重写是打破此耦合的窗口。

### S.U.P.E.R Violation Hotspots (排名)

| # | Module | Violations | Severity | 修复策略 |
|:--|:-------|:-----------|:---------|:---------|
| 1 | `tokenRouter.ts` (3800 LOC) | S,U,P,R | 🔴 Critical | **必须拆分**为 matcher/selector/cooldown/cache/circuit_breaker 五个包 |
| 2 | `modelService.ts` (4000 LOC) | S,U,P,R | 🔴 Critical | 拆分 probe/discovery/pricing 三个包; 通过 `RouteRebuilder` 接口打破与 tokenRouter 的双向依赖 |
| 3 | `proxy-core/` (67 files) | S,U,P | 🟡 High | Phase 设计时显式定义 `ProxyConductor` 接口, 隔离 surface/selection/execution 层次 |
| 4 | `oauth/service.ts` | S | 🟡 Medium | 拆分为 flow/connection/refresh 三个子模块 |
| 5 | `db/index.ts` (1525 LOC) | S | 🟡 Medium | Go 版自然拆分为 schema/migration/connection 包 |

## Risk Matrix

| Risk | Impact | Likelihood | Severity | Mitigation |
|:-----|:-------|:-----------|:---------|:-----------|
| **协议转换精度丢失** — OpenAI/Anthropic/Gemini/Codex 四向格式互转，SSE stream chunk 逐帧转换易出边界 case | 高 — 用户收到错误格式响应 | 中 | 🔴 **高** | P9 优先做各协议的 `roundtrip` 测试; 用现有 TS 版的请求/响应对照集做 golden file test |
| **tokenRouter 路由逻辑偏离** — 加权选择/冷却/断路器行为与原版不一致 | 高 — 通道选择错误导致代理不可用 | 中 | 🔴 **高** | Go 版实现后与 TS 版并行运行 A/B 对比; 同样输入验证选择结果一致性 |
| **16 平台适配器覆盖不全** — 14 个平台 x 多个方法, 边缘 case 遗漏 | 中 — 部分平台签到/余额失败 | 高 | 🟡 **中** | 按平台使用频率分优先級, newApi/oneApi/sub2api/openai 先做 |
| **streaming SSE 逐 chunk 转换 bug** — Go 与 JS 的 stream/buffer 语义不同 | 中 — 流式响应中断或乱码 | 中 | 🟡 **中** | 每个 transformer 做 streaming roundtrip 测试; 用 `bufio.Scanner` 按行处理 SSE |
| **SQLite ↔ PG 方言差异** — datetime 文本 vs timestamp, boolean 0/1 vs bool | 中 — 跨方言部署时行为不一致 | 中 | 🟡 **中** | 用同一测试套件对 SQLite 和 PG 各跑一遍; CI matrix 测试 |
| **前端 SPA 路由冲突** — Go embed 的 SPA fallback 与 API 路由冲突 | 低 — SPA 页面返回 404 | 低 | 🟢 **低** | 路由注册顺序: /api/* → /v1/* → /* (SPA fallback) |
| **密钥/凭据泄露** — Go binary 中的 config/env 处理不当 | 高 — 安全事故 | 极低 | 🟢 **低** | Go 版同样遵循: env file → config struct, 禁止硬编码, CI secret scanning |
| **Docker 部署回归** — 新 Dockerfile 与现有生产基础设施不兼容 | 中 — 部署失败 | 低 | 🟡 **中** | 保持与现有 `Dockerfile.slim` 相同的端口/卷/环境变量命名 |

## Technical Debt (从 TS 版继承)

1. **Legacy schema compat** — `legacySchemaCompat.ts` + `migrate.ts` 的 self-healing recovery loop 是 SQLite drift 的历史包袱, Go 版不需要 (从 scratch 写干净 migration)
2. **Drizzle proxy driver** — SQLite-dialect query builder 通过 proxy callback 模拟到 MySQL/PG, 性能/可维护性差。Go 版直接用 sqlx 方言化查询。
3. **单文件怪兽** — `tokenRouter.ts` (3800 LOC) 和 `modelService.ts` (4000 LOC) 是历史增长的产物, Go 版必须拆分
4. **循环依赖** — modelService ↔ tokenRouter 双向依赖通过运行时耦合隐藏。Go 编译器不接受 — 必须通过接口打破
5. **OAuth background refresh 休眠** — `oauthRefreshScheduler.ts` 定义但未在 `index.ts` 启动, token 刷新仅在代理请求时 lazy 触发。Go 版应决定: 启用 scheduler 或保持 lazy。

## Testing Risks

- TS 版有广泛测试 (vitest, 每个 route/service 几乎都有 `.test.ts`), 但测试不是 golden file 格式 — Go 版无法直接复用
- **Mitigation**: P0 建立 Go 测试基础设施 (testify + httptest + testcontainers 或 SQLite :memory:); 每个 P 阶段产出对应的 Go 测试
- Streaming 和 protocol transformers 是最难自动测试的部分 — 需要设计 SSE golden file 对照测试

## Project Governance Risks

- 现有 `AGENTS.md` 是 TS 版专用 (Drizzle files, Fastify, Vite, JS 测试框架) — 需为 Go 版重写
- metapi skill (`<agent-skill-dir>\metapi\SKILL.md`) 是运维/API 参考 — Go 版需同步更新 endpoint/部署说明
- Go 版从零开始, 无记忆文件 — Phase 4 时建立项目 memory surface

## Compatibility Concerns

| Concern | Current (TS) | Target (Go) | Risk |
|:--------|:-------------|:------------|:-----|
| API 兼容 | Fastify JSON 响应 | chi JSON 响应 | 低 — REST JSON 格式保持一致即可 |
| DB 兼容 | SQLite/MySQL/PG (via Drizzle proxy) | SQLite/PG (via sqlx) | 中 — 需保持 schema 一致, 文本时间格式不变 |
| Proxy 兼容 | OpenAI API → 上游 | 同 | 低 — 协议标准, 不随语言改变 |
| Config/env 兼容 | ~100 env vars | 同名 env vars | 低 — 保持 env var 命名一致 |
| Docker 兼容 | `node:25-alpine` + npm rebuild | `scratch`/`alpine` + 单二进制 | 低 — 端口/卷/entrypoint 保持一致 |
| Frontend 兼容 | `@fastify/static` 服务 | `embed` + `http.FileServer` | 极低 — SPA 是纯静态, 任何 HTTP server 服务无差别 |
