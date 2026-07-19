# UI 原版对照 · 功能/按钮/观感（2026-07-20）

**Scope**: 回应「UIUX 太丑 + 原版功能和按钮全没了」  
**对照面**:
- **metapi-go** tip `98b6aff` / product pages under `web/`
- **原版 TokenDance/metapi**（同 monorepo sibling web；与 cita-777 同源 web 壳；含 Electron desktop）
- **截图**: `docs/analysis/ui-shots/page-*-win32.png`（空库，**早于 #553/#554**，已过期）vs `shell-*-win32.png`（dense mock）

> 结论先说：**侧栏路由与页面按钮面基本齐平，没有「原版功能整页被删」**。  
> 用户体感来自 **空库稀疏 + 主题换肤 + 截图/线上 pin 落后 tip**，不是功能缺失矩阵。

---

## 1. 路由 / 侧栏（完整对照）

| 路由 | 原版 | metapi-go | 备注 |
|:-----|:----:|:---------:|:-----|
| `/` 仪表盘 | ✅ | ✅ | go 多 first-run strip（#553） |
| `/sites` 站点管理 | ✅ | ✅ | go 空库 defer 权重横幅（#554） |
| `/site-announcements` | ✅ | ✅ | |
| `/accounts` 连接管理 | ✅ | ✅ | |
| `/oauth` | ✅ | ✅ | |
| `/tokens` | ✅ redirect→accounts | ✅ 同 | 两边都是 `Navigate` 到连接管理嵌套令牌 |
| `/downstream-keys` | ✅ | ✅ | |
| `/checkin` | ✅ | ✅ | |
| `/routes` | ✅ | ✅ | |
| `/logs` | ✅ | ✅ | |
| `/monitor` | ✅ | ✅ | |
| `/settings` | ✅ | ✅ | |
| `/events` | ✅ | ✅ | |
| `/settings/import-export` | ✅ | ✅ | |
| `/settings/notify` | ✅ | ✅ | |
| `/models` `/playground` `/about` | ✅ | ✅ | topbar 导航 |
| `/__design__` | go 独有 gallery | ✅ | 设计系统验收，非产品功能 |

**侧栏分组**（控制台 10 + 系统 4）两边同构；topbar 模型广场/操练场/关于 + Ctrl+K 搜索 + 通知 + 主题切换均在。

**原版独有、web 管理台不对等**：
- Electron **desktop/** 壳（托盘、本地打包、updater 入口）—— metapi-go 是 **Go 网关 + 浏览器 admin**，不是 Electron 客户端。这不是「按钮被删」，是产品形态不同。

---

## 2. 页面按钮密度（`<button` 计数抽样）

| 页面 | go | 原版 | 判定 |
|:-----|---:|-----:|:-----|
| Dashboard | 12 | 11 | 齐 / go 略多 |
| Sites | 83 | 83 | 齐 |
| Accounts | 90 | 90 | 齐 |
| Tokens panel | 47 | 47 | 齐 |
| TokenRoutes | 37 | 37 | 齐 |
| Settings | 64 | 64 | 齐 |
| DownstreamKeys | 30 | 30 | 齐 |

行数级页面体量也同量级（如 Accounts ~3462/3483、Sites ~2419/2363）。  
**批量操作、添加/编辑/删除、同步令牌、刷新状态、自动检测、系统代理批量** 等在 go 页内均在。

### 易被误判为「没了」的设计

| 现象 | 原因 |
|:-----|:-----|
| 空库仪表盘只有 0 | 无站点/无流量；**tip 已折叠为零 KPI + 开始使用条**（#553），仓库内 `page-dashboard-*-win32.png` **仍是旧图** |
| 空库站点页大片空白 | EmptyState 正常；行级按钮在有数据后才出现 |
| 「账号令牌」侧栏入口 | 两边都从 `/tokens` **重定向到连接管理**内嵌面板，不是独立侧栏项 |
| 生产看起来「旧」 | ops pin **v0.8.44**；UI-REFRESH / first-run 在 tip **未发版**（release decision residual） |
| Mock 很「满」真实很「空」 | `shell-dashboard` 是 #538 验收 mock（有 12.4k 请求），不是真实 DB |
| Shell mock 侧栏曾只有 3～4 项 | **已修**（2026-07-20）：mock 对齐生产 14 项，避免再被当成「目录被砍」 |

---

## 3. 观感 / 主题差异（「丑」的可指认点）

| 维度 | 原版 web | metapi-go (M51+) |
|:-----|:---------|:-----------------|
| Primary | indigo `#4f46e5` / dark `#6366f1` | GCP blue `#1a73e8` / dark `#8ab4f8` |
| Chrome | 偏实色侧栏 | glass topbar/sidebar + cool gray canvas |
| 空态 | 混杂 empty 文案 | 统一 `EmptyState` + dual CTA |
| 密度 | 有数据时表格密 | 同；**无数据时** go 更「呼吸」→ 易被读成空壳 |

**真实丑点（人眼，按优先级）**：

1. **P0 体感**：空库 5 张全 0 KPI（旧截图/旧 pin）— tip 已修，需 **发版或重录 shot**  
2. **P0 体感**：Sites 空库仍顶 amber 权重长文（旧截图）— tip #554 已 defer  
3. **P1**：glass + 大白卡片 + 冷灰底在 Windows 上偏「模板后台」，对比原版 indigo 亲和度下降  
4. **P1**：sidebar 全量导航在 first-run 时信息过载（NAV-1 未做 progressive disclosure）  
5. **P2**：mock gallery 评分不能代表 first-run 信任

---

## 4. 后端/能力 residual（不是「按钮没了」）

见 [`high-value-next.md`](./high-value-next.md) / [`original-gap-matrix.md`](./original-gap-matrix.md)：  
P0-585 partial、P0-555 residual、#579/#547/#584 partial 等是 **协议/计费/键权重**，不是 UI 删按钮。

---

## 5. 推荐下一步（未开 Issue，需决策）

| ID | 动作 | 价值 |
|:---|:-----|:-----|
| **UI-REL-1** | 决定 tip 是否打 **v0.8.45** 发版（把 first-run + glass 推到 ops pin） | 用户立刻看到「功能在」 |
| **SHOT-1** | 空库重录 `page-dashboard/sites/settings-*-win32.png`（含 #553/#554） | 文档/评分诚实；需 `METAPI_UI_AUTH_TOKEN` + 空库 |
| **MOCK-NAV** | Shell mock 侧栏对齐生产 14 项 | **done tip** — `DesignSystemGallery` + shell shots + win32 gallery baselines |
| **VIS-1** | 可选：primary 回 indigo 或提供主题 preset（GCP / Classic） | 缓解「不像原版」 |
| **NAV-1** | first-run 侧栏强调 站点/连接/设置（progressive） | 降 onboarding 噪音 |
| **DENSE-1** | 有数据页（Sites/Accounts 表格）再做一 denser pass | 运营态「满」感 |
| **CONSOLE-1** | 系统字体 + pill 导航 + 冷静标题 + 高分屏 content max-width | **done tip** — tokens/index.css/index.html + DESIGN |

**默认建议**：先 **UI-REL-1 + SHOT-1**；主题 preset（VIS-1）仍可选。

---

## 6. 验证命令（复现对照）

```bash
# 路由
rg -n "Route path=" web/App.tsx ../metapi/src/web/App.tsx

# 按钮密度
rg -c '<button' web/pages/{Dashboard,Sites,Accounts,Tokens,TokenRoutes,Settings,DownstreamKeys}.tsx

# first-run 是否在 tip
rg -n "isFirstRunBootstrap|dashboard-getting-started" web/pages/Dashboard.tsx
rg -n "sites.length > 0" web/pages/Sites.tsx   # weight banner guard
```

---

## 7. Decision log

| Decision | Rationale |
|:---------|:----------|
| 不做「恢复丢失按钮」大扫除 | 静态对照显示按钮面齐平 |
| 把投诉拆成 发版/截图/主题/first-run | 可独立验收 |
| 文档落此文件 | 下一 agent 不重复 inventory |
