# Dependency Graph — Production Hardening

> **Status: historical / superseded** (pre-v0.4 hardening era). Not an open board.

```mermaid
graph TD
    subgraph Phase1[Phase 1: Critical Fixes]
        T1_1[1.1: RuntimeExecutor 接入]
        T1_2[1.2: 消除 OAuth panic]
        T1_3[1.3: 默认密钥告警]
        T1_4[1.4: WriteTimeout SSE]
    end

    subgraph Phase2[Phase 2: Security & Reliability]
        T2_1[2.1: ConstantTimeCompare]
        T2_2[2.2: CI 修复]
        T2_3[2.3: DB 连接池]
        T2_4[2.4: log.Printf → slog]
        T2_5[2.5: Request ID 中间件]
        T2_6[2.6: context.Background 替换]
        T2_7[2.7: re-panic 修复]
    end

    subgraph Phase3[Phase 3: Observability & Tests]
        T3_1[3.1: 零覆盖包补齐]
        T3_2[3.2: admin 覆盖率提升]
        T3_3[3.3: /metrics 端点]
        T3_4[3.4: 安全响应头]
        T3_5[3.5: APIError 类型]
    end

    subgraph Phase4[Phase 4: Polish]
        T4_1[4.1: TODO 清零]
        T4_2[4.2: CORS 锁定]
        T4_3[4.3: /debug/vars 保护]
        T4_4[4.4: chatFormatsCore 拆分]
        T4_5[4.5: PGO 启用]
    end

    subgraph Phase5[Phase 5: Release]
        T5_1[5.1: 全量验证]
        T5_2[5.2: CI/CD 验证]
        T5_3[5.3: AGENTS.md 更新]
        T5_4[5.4: Tag v0.4.0]
    end

    Phase1 --> Phase2
    Phase2 --> Phase3
    Phase3 --> Phase4
    Phase4 --> T5_1
    T5_1 --> T5_2
    T5_2 --> T5_3
    T5_2 --> T5_4
```

## Parallel Execution Lanes

| Phase | Lane A | Lane B | Merge Risk |
|-------|--------|--------|------------|
| P1 | 1.1 + 1.2 + 1.3（不同文件） | 1.4（app.go + upstream.go） | 🟢 Low |
| P2 | 2.1 + 2.2 + 2.3 + 2.4 | 2.5 + 2.6 + 2.7 | 🟡 Med（auth+ci+store vs oauth+scheduler） |
| P3 | 3.1 + 3.2（测试文件） | 3.3 + 3.4 + 3.5（新中间件 + metric） | 🟢 Low |
| P4 | 4.1 + 4.2 + 4.3 | 4.4 + 4.5（transform + Dockerfile） | 🟢 Low |
| P5 | Sequential | — | — |

## Critical Path

```
1.1 → 2.x → 3.x → 4.x → 5.1 → 5.2 → 5.4
```

Tasks 1.1（RuntimeExecutor 接入）是最高优先级的单项修复——它修复所有 proxy 路径的超时问题，且是后续测试的基础。
