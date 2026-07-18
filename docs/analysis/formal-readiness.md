# Formal readiness checklist

**Date**: 2026-07-19  
**Status**: design SSOT (no product flip without ACs)  
**Scope**: decide *what kind of “正式版”* MetAPI Go is allowed to claim  
**Related**: [`../STATE.md`](../STATE.md) · [`high-value-next.md`](./high-value-next.md) · [`residual-next-candidates.md`](./residual-next-candidates.md) · ops `projects/metapi/STATE.md`

---

## 1. Two different “正式”

| Track | Meaning | Audience | Current (v0.8.44) |
|:------|:--------|:---------|:------------------|
| **A. 对内正式可用** | 自己人/小流量当代理入口用；可维护、可升级、故障可解释 | 运维 / 内部 | **YES — 已达标** |
| **B. 对外宣传完备** | 对外说“企业全功能 / 高可用 / 计费准 / 多副本粘滞 / WS 全通” | 客户 / 公告 | **NO — 诚实 residual 未清** |

默认对外话术：**对内正式可用 · 受控生产**；不要写成“全量企业完备”。

---

## 2. Track A — 对内正式可用（已满足）

全部为 **must**；任一条��败则降级为“试用/止血态”。

### A1. 产品

| # | Gate | Evidence | v0.8.44 |
|:-:|:-----|:---------|:-------:|
| A1.1 | 有 semver Release + GHCR 镜像 | Release `v0.8.44` · `ghcr.io/tokendancelab/metapi-go:0.8.44` | ✅ |
| A1.2 | 主代理路径可用 | OpenAI/Anthropic/Gemini/Codex HTTP 路径；非 stub 生产默认 503 | ✅ |
| A1.3 | 管理面可用 | Admin UI + `/api/*` + 登录 | ✅ |
| A1.4 | 双库形态 | SQLite 单机 / PostgreSQL 生产 | ✅ |
| A1.5 | 连接预算可控 | `DB_PROFILE` + MaxOpen≤role LIMIT + 53300 退避 | ✅ |
| A1.6 | 健康探针 | `/health` liveness · `/ready` readiness | ✅ |
| A1.7 | Residual 诚实 | STATE/MASTER 不把 partial 写成 present | ✅ |
| A1.8 | 测试门禁 | pre-push `go vet` + `go test -race` | ✅ |

### A2. 运维（我们的 hk3）

| # | Gate | Live rule | Status |
|:-:|:-----|:----------|:------:|
| A2.1 | pin 明确 | compose image = release tip | ✅ 0.8.44 |
| A2.2 | MaxOpen ≤ role LIMIT | 当前 **1 = 1** | ✅ |
| A2.3 | 不与冷备双活同库 | us1 不得同时连生产 Azure PG | ✅ 规则在册 |
| A2.4 | 启动验证 | `/ready` ok · metrics `db_conn_errors_total=0` · Azure backends=1 | ✅ |
| A2.5 | 资源边界 | 2C2G 主机上 metapi 常驻 ≤ ~32MiB 目标；不拖垮 NewAPI | ✅ ~10MiB |
| A2.6 | 自动回魂策略明确 | `restart=no` = 人工 up；文档写明 | ✅ |

### A3. 明确允许的“未完成”

这些 **不阻断** Track A：

- P0-585 production e2e 未跑（unit load-proof 在）
- P0-555 计费 residual
- WS-1 / STICKY-B / UC-1
- OAuth client placeholder（未配真实 client 时登录失败是预期）
- Redis 未部署（单实例不需要）

---

## 3. Track B — 对外宣传完备（未满足）

全部为 **must** 才允许“企业完备 / HA / 计费准”话术。

| # | Gate | Why blocked today |
|:-:|:-----|:------------------|
| B1 | P0-585 **production e2e** multi-channel storm | 仍 partial |
| B2 | P0-555 计费 residual 关闭（media zeros / multi-instance lag AC） | present-with-residual |
| B3 | WS-1 有协议 AC 且实现 | residual 501/426 |
| B4 | 多实例 sticky 方案落地（LB pin **或** STICKY-B） | sticky 进程内 |
| B5 | UC-1 真实 registry 或永久隐藏入口 | 501 / log-only |
| B6 | 多副本连接预算演练（每副本 pool × N ≤ role） | 我们生产故意 1 副本 1 连接 |
| B7 | 公开 README 不出现“假 sticky / 假 updateAvailable” | 已挡；需持续 hygiene |
| B8 | 支持声明的高可用/重启策略与实测一致 | 当前 restart=no 不是 HA |

**对外可用话术模板（推荐）**

> MetAPI Go v0.8.x 是可自托管的元聚合代理网关正式发行版。  
> 支持统一代理、路由与故障转移、站点账号与签到、SQLite/PostgreSQL。  
> 多实例粘滞会话、Responses WebSocket、远程升级中心、极端级联风暴生产 e2e 与完美计费仍为 residual，见文档。

---

## 4. 运行档位（运维 × 产品）

| Tier | 名称 | 适用 | 配置要点 | 我们现在 |
|:-----|:-----|:-----|:---------|:---------|
| **T0** | 止血/压死 | 事故 | exited 或 MaxOpen=1 + restart=no | 刚离开 |
| **T1** | 受控生产 | 低业务 / 共享小 PG | `DB_PROFILE=shared-tiny` 或 force 1–2；restart=no 或 on-failure | **当前 hk3** |
| **T2** | 常规生产 | 专用小库 | `normal` 10/3；restart unless-stopped；单实例 | 可选升级 |
| **T3** | 扩展生产 | 独占 PG | `dedicated` 或更大 MaxOpen；多副本 + LB；可选 Redis admission | 未做 |
| **T4** | 宣称 HA | 对外 SLA | Track B 全绿 + 演练 | 未做 |

**升级 T1→T2 门禁（示例）**

1. role LIMIT ≥ 目标 MaxOpen（建议 ≥3）  
2. 15m `db_conn_errors_total` 不增、Azure failed 连接 0  
3. NewAPI/其他 role 连接面仍健康  
4. 明确是否改 `restart` 策略（需批准）

---

## 5. Redis / 依赖

| 依赖 | Track A | Track B | 说明 |
|:-----|:--------|:--------|:-----|
| PostgreSQL 或 SQLite | 必需其一 | 必需 | 生产我们用 Azure PG |
| Redis | **不需要** | 仅多实例 RPM/TPM 共享时可选 | fail-open；不做 sticky |
| resin / 出口池 | **不需要** | 不需要 | 那是 Gateway/Grok 边车，不是 MetAPI |
| OAuth 真 client | 可选 | 若宣传 OAuth 登录则必需 | placeholder = 预期失败 |

---

## 6. 发布与沟通检查表（每次 tag）

- [ ] CHANGELOG 有用户可读 Fixed/Added  
- [ ] Release 非 draft；CD 出 `0.x.y`（无 v 前缀）镜像  
- [ ] STATE tip = tag；ops STATE pin 同步或注明 lag  
- [ ] residual 表未把 partial 写成 present  
- [ ] 若仅 Track A：Release 说明不写“HA/完美计费/多实例 sticky”  
- [ ] 若运维 T1：写明 pool/role 与 restart 策略  

---

## 7. 决策记录（2026-07-19）

| Decision | Choice |
|:---------|:-------|
| 当前对外定位 | **对内正式可用 · 受控生产（T1）** |
| 是否算“测试版” | **否** — v0.8.44 是正式 release；运维受控 ≠ 测试镜像 |
| 是否可正常使用 | **可** — 主路径可用；吞吐受 1/1 池限制 |
| 下一产品大波 | UI/UX 重构（见 [`ui-ux-refresh.md`](./ui-ux-refresh.md)），与 Track B residual 分开排期 |
| Track B | 不自动承诺；单开 Milestone + AC |

---

## 8. Owner map

| Question | Read |
|:---------|:-----|
| 产品现在能否对内用？ | 本文 §2 + STATE |
| 能否对外吹完备？ | 本文 §3 → 否 |
| 线上 pin/role？ | server `projects/metapi/STATE.md` |
| 还差哪些功能诚实项？ | `high-value-next.md` |
| UI 下一波怎么改？ | `ui-ux-refresh.md` |
