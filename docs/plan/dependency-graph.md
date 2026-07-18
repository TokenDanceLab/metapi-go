# Dependency Graph — MetAPI Go Rewrite

> **Status: historical rewrite-era graph**. Counts and package names are archival; living map is `docs/architecture.md`.

```mermaid
graph TD
  subgraph Phase0["Phase 0: Foundation"]
    P0["P0: Skeleton + Config + Docker"]
  end

  subgraph Phase1["Phase 1: Data Layer"]
    P1["P1: DB Schema + Migration<br/>27 tables, SQLite/PG"]
  end

  subgraph Phase2["Phase 2: Security"]
    P2["P2: Auth Middleware<br/>Admin IP CIDR + Proxy Key"]
  end

  subgraph Phase3["Phase 3: Core CRUD"]
    P3["P3: Sites + Accounts + Tokens<br/>36 REST endpoints"]
  end

  subgraph ParallelA["Parallel A: Business Services"]
    P4["P4: Platform Adapters<br/>14 platforms"]
    P5["P5: Checkin + Balance + Notify<br/>5 notification channels"]
    P6["P6: OAuth Subsystem<br/>4 providers PKCE"]
    P7["P7: TokenRouter<br/>Channel selection + cooldown"]
  end

  subgraph Phase8["Phase 8: Proxy Engine"]
    P8["P8: ProxyCore<br/>Endpoint flow + sessions"]
  end

  subgraph Phase9["Phase 9: Transformers"]
    P9["P9: Transformers<br/>4 protocols ↔ canonical"]
  end

  subgraph ParallelB["Parallel B: Endpoints"]
    P10["P10: Proxy Routes<br/>11 surfaces /v1/*"]
    P11["P11: Admin API<br/>60+ endpoints"]
  end

  subgraph Phase12["Phase 12: Runtime"]
    P12["P12: Schedulers<br/>15 background jobs"]
  end

  subgraph Phase13["Phase 13: Integration"]
    P13["P13: Embed + CI/CD + Docs"]
  end

  P0 --> P1
  P1 --> P2
  P2 --> P3
  P3 --> P4
  P3 --> P5
  P3 --> P6
  P3 --> P7
  P4 --> P7
  P7 --> P8
  P6 --> P8
  P8 --> P9
  P9 --> P10
  P7 --> P10
  P8 --> P10
  P5 --> P11
  P6 --> P11
  P7 --> P11
  P8 --> P11
  P3 --> P11
  P5 --> P12
  P6 --> P12
  P7 --> P12
  P8 --> P12
  P10 --> P13
  P11 --> P13
  P12 --> P13
```

## Parallel Execution Lanes

| Lane | Phases | Can run together? |
|------|--------|-------------------|
| **Foundation** | P0 → P1 → P2 → P3 | Sequential (hard deps) |
| **Parallel A** | P4, P5, P6, P7 | ✅ 可同时推进 (不同文件, 最小 merge conflict) |
| **Proxy Engine** | P8 → P9 | Sequential |
| **Parallel B** | P10, P11 | ✅ 可同时推进 (不同 handler 目录) |
| **Schedulers** | P12 | After Parallel A + Proxy Engine |
| **Integration** | P13 | After all |

## Merge Risk Assessment

| Parallel Group | Files Touched | Merge Risk |
|:---|:---|:---|
| P4 + P5 + P6 + P7 | `platform/`, `service/checkin/`, `service/oauth/`, `routing/` | 🟢 Low — 不同子目录 |
| P10 + P11 | `handler/proxy/`, `handler/admin/` | 🟢 Low — 不同子目录 |
