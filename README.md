# MetAPI Go

<div align="center">

**中转站的中转站，将分散的 AI 中转站聚合为一个统一网关**

[MetAPI](https://github.com/cita-777/metapi) 的 Go 语言重写版。单二进制部署，与原 TypeScript 版功能对等。

<p align="center">
  <a href="README.md"><strong>中文</strong></a> |
  <a href="README_EN.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml/badge.svg"></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26.5-00ADD8?logo=go">
  <a href="https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go"><img alt="Docker" src="https://img.shields.io/badge/ghcr-v0.8.42-blue?logo=docker"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-green"></a>
</p>

</div>

---

## 介绍

把你在各处注册的 New API / One API / OneHub / DoneHub / Veloera / AnyRouter / Sub2API 等站点，汇聚成**一个 API Key、一个入口**，自动发现模型、智能路由、成本最优。

MetAPI 作为中转站之上的**元聚合层**，把多个站点统一到一个入口，下游所有工具（Cursor、Claude Code、Codex、Open WebUI 等）即可无感接入全部模型。当前支持的上游范围不止传统聚合面板，还包括：

- 聚合面板：New API、One API、OneHub、DoneHub、Veloera、AnyRouter、Sub2API
- 通用兼容接口：OpenAI、Claude、Gemini 兼容端点，以及 `cliproxyapi`
- OAuth 连接：Codex、Claude、Gemini CLI、Antigravity

| 痛点 | MetAPI 怎么解决 |
|------|----------------|
| 每个站点一个 Key，下游工具配置一堆 | **统一代理入口**，模型自动聚合到 `/v1/*` |
| 不知道哪个站点用某个模型最便宜 | **智能路由**自动按成本、余额、使用率选最优通道 |
| 某个站点挂了，手动切换好麻烦 | **自动故障转移**，一个通道失败自动冷却并切到下一个 |
| 余额分散在各处，不知道还剩多少 | **集中看板**一目了然，余额不足自动告警 |
| 每天得去各站签到领额度 | **自动签到**定时执行，奖励自动追踪 |
| 不知道哪个站有什么模型 | **自动模型发现**，上游新增模型零配置出现在你的模型列表里 |

### Go 版有什么不同

和原 TypeScript 版功能完全一致，换个运行时：

| | Node.js（原版） | Go（本版） |
|---|---|---|
| 内存占用 | ~85 MB | ~20 MB |
| Docker 镜像 | ~250 MB | ~15 MB |
| 启动时间 | 5-10 秒 | 即时 |
| 部署方式 | 需要 Node 运行时 | 单个二进制文件 |

---

## 快速开始

### Docker

```bash
docker run -d --name metapi \
  -p 4000:4000 \
  -e AUTH_TOKEN=your-admin-token \
  -e PROXY_TOKEN=your-proxy-token \
  -e TZ=Asia/Shanghai \
  -v ./data:/app/data \
  --restart unless-stopped \
  ghcr.io/tokendancelab/metapi-go:latest
```

启动后访问 `http://localhost:4000`，用 `AUTH_TOKEN` 登录。

> 请务必修改 `AUTH_TOKEN` 和 `PROXY_TOKEN`，不要使用默认值。数据存储在 `./data` 目录，升级不会丢失。

### Docker Compose

```bash
mkdir metapi && cd metapi

cat > docker-compose.yml << 'EOF'
services:
  metapi:
    image: ghcr.io/tokendancelab/metapi-go:latest
    ports:
      - "4000:4000"
    volumes:
      - ./data:/app/data
    environment:
      AUTH_TOKEN: ${AUTH_TOKEN:?required}
      PROXY_TOKEN: ${PROXY_TOKEN:?required}
      CHECKIN_CRON: "0 8 * * *"
      BALANCE_REFRESH_CRON: "0 * * * *"
      TZ: Asia/Shanghai
    restart: unless-stopped
EOF

export AUTH_TOKEN=your-admin-token
export PROXY_TOKEN=your-proxy-token
docker compose up -d
```

### 从源码

```bash
git clone https://github.com/TokenDanceLab/metapi-go.git
cd metapi-go
go build -o metapi ./cmd/server
AUTH_TOKEN=admin PROXY_TOKEN=proxy-token ./metapi
```

---

## 核心功能

### 统一代理网关

兼容 **OpenAI** 与 **Claude** 下游格式，对接所有主流客户端。支持 Chat Completions、Responses、Messages、Completions、Embeddings、Images、Models，以及标准 `/v1/files` 文件接口。完整的 SSE 流式传输，自动格式转换。

### 智能路由引擎

自动发现所有上游站点的可用模型，零配置生成路由表。多通道概率分摊，基于成本、余额、使用率加权分配。失败通道自动冷却与避让，请求失败自动重试切到其他可用通道。

### 多平台聚合管理

| 平台 | 适配器 | 说明 |
|------|--------|------|
| New API | `new-api` | 新一代大模型网关 |
| One API | `one-api` | 经典 OpenAI 接口聚合 |
| OneHub | `onehub` | One API 增强分支 |
| DoneHub | `done-hub` | OneHub 增强分支 |
| Veloera | `veloera` | API 网关平台 |
| AnyRouter | `anyrouter` | 通用路由平台 |
| Sub2API | `sub2api` | 订阅制中转平台 |
| OpenAI / Claude / Gemini | `openapi` / `claude` / `gemini` | 标准兼容接口 |

各平台适配器覆盖模型枚举、余额查询、Token 管理、代理接入等通用能力。

### 账号与 Token 管理

多站点多账号，每个账号可持有多个 API Token。凭证加密存储在本地数据库中。Token 过期自动重新登录获取新凭证，禁用站点自动级联禁用所有关联账号。

### 自动签到

Cron 定时执行（默认每日 08:00），智能解析奖励金额，签到失败自动通知。按账号启用/禁用控制，完整签到日志与历史查询。

### 余额管理

定时余额刷新（默认每小时），批量更新所有活跃账号。收入追踪：每日/累计收入与消费趋势分析。凭证过期自动重新登录。

### 告警通知

支持五种通知渠道：Webhook、Bark、Server酱、Telegram Bot、SMTP 邮件。告警场景包括余额不足预警、站点/账号异常、签到失败、代理请求失败、Token 过期提醒、每日摘要报告。

### 轻量部署

单 Docker 容器，默认本地数据目录部署，支持外接 PostgreSQL 运行时数据库。Go 单二进制，15MB 镜像，启动即时。数据完整导入导出，迁移无忧。

---

## 配置

所有环境变量与原 TypeScript 版完全一致，无缝替换。

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AUTH_TOKEN` | `change-me-admin-token` | 管理员令牌 |
| `PROXY_TOKEN` | `change-me-proxy-sk-token` | 代理 API Key |
| `PROXY_MAX_BUFFERED_RESPONSE_BYTES` | `20971520` | 非流式上游响应的最大缓冲字节数，默认 20 MiB，超限返回 502 |
| `METAPI_ENABLE_PROXY_STUB` | 空 | 测试/演示用本地代理 stub 开关；生产保持为空，未配置上游转发时返回 503 |
| `PORT` | `4000` | 监听端口 |
| `DB_TYPE` | `sqlite` | 数据库类型（`sqlite` / `postgres`）；提供 PostgreSQL URL 时可自动推断为 `postgres` |
| `DATABASE_URL` / `DB_URL` | 空 | PostgreSQL 连接串或 SQLite 文件路径；`DB_URL` 优先，`DATABASE_URL` 用于兼容部署平台 |
| `DB_SSLMODE` | 空 | PostgreSQL TLS 模式；支持 `disable`、`allow`、`prefer`、`require`、`verify-ca`、`verify-full`；非空时覆盖连接串中的 `sslmode` |
| `DB_MAX_OPEN_CONNS` / `DB_MAX_IDLE_CONNS` | `20` / `5` | PostgreSQL 应用池预算；生产值不得超过数据库 role connection limit |
| `DB_CONN_MAX_LIFETIME_SEC` / `DB_CONN_MAX_IDLE_TIME_SEC` | `1800` / `300` | PostgreSQL 连接寿命与空闲回收时间（秒） |
| `TRUSTED_PROXY_CIDRS` | 空 | 允许提供 `X-Forwarded-For` / `X-Real-IP` 的反向代理 CIDR CSV；默认忽略 forwarded headers |
| `ADMIN_CORS_ALLOWED_ORIGINS` | 空 | 允许跨域访问 `/api/*` 管理接口的精确 `http(s)` origin CSV；默认只支持同源管理 UI，禁止 `*` |
| `CHECKIN_CRON` | `0 8 * * *` | 签到时间 |
| `BALANCE_REFRESH_CRON` | `0 * * * *` | 余额刷新频率 |

当前运行时支持两种数据库形态：单进程 SQLite；PostgreSQL 生产部署。PostgreSQL 模式下，产生外部请求、通知、上传、清理或同步副作用的后台任务使用 PG advisory lock，避免多副本重复执行同一批任务。可选 `REDIS_URL` / `METAPI_REDIS_URL` 仅用于多实例下游 Key 的 **RPM/TPM admission** 共享计数（`auth.ConfigureSharedAdmissionFromRedisURL` + `internal/sharedcount`，不可达时 fail-open 回退进程内窗口）；留空则无需 Redis 进程。Sticky session 仍是进程内绑定，**不会**因配置 Redis 而跨实例共享（STICKY-B 仍为 residual，非产品）。详见 [`docs/analysis/redis-shared-state.md`](docs/analysis/redis-shared-state.md)。

代理转发没有配置路由和上游依赖时，生产默认返回 HTTP 503。`METAPI_ENABLE_PROXY_STUB=1` 只用于测试或演示，避免把本地假响应误当成真实上游调用。

[`.env.example`](.env.example) 中有完整的环境变量清单。

## 运维健康检查

- `GET /health` 是 liveness，只确认 HTTP 进程存活。
- `GET /ready` 是 readiness，会检查数据库；数据库不可用或进程正在关停时返回 HTTP 503。
- Docker 默认执行 `metapi healthcheck`，等价于探测 `http://127.0.0.1:${PORT}/ready`。
- 可用 `METAPI_HEALTHCHECK_URL` 或 `METAPI_HEALTHCHECK_PATH` 覆盖容器健康检查目标。

---

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 | [chi](https://github.com/go-chi/chi) 路由 + `net/http` |
| 语言 | Go 1.26.5 |
| 数据库 | SQLite / PostgreSQL + [sqlx](https://github.com/jmoiron/sqlx)；可选 Redis 仅用于 RPM/TPM admission（非必需） |
| 定时任务 | [robfig/cron](https://github.com/robfig/cron) |
| 容器化 | Docker（Alpine，15MB 镜像） |
| 前端 | React 19 + Vite 8 + Tailwind CSS v4（内嵌） |

---

## 数据与隐私

MetAPI 完全自托管，所有数据（账号、令牌、路由、日志）均存储在你自己的部署环境中，不会向任何第三方发送数据。代理请求仅在你的服务器与上游站点之间直连传输。

---

## 从 TypeScript 版迁移

数据库 Schema 完全一致，Go 版启动时自动执行幂等 migration。停止旧服务，用同样的环境变量启动 Go 版即可。

---

## 文档导航

| 文档 | 用途 |
|------|------|
| [docs/README.md](docs/README.md) | **文档地图**（先看这个） |
| [docs/architecture.md](docs/architecture.md) | 包结构与请求路径 |
| [docs/progress/MASTER.md](docs/progress/MASTER.md) | 当前里程碑 / 活跃 Issue |
| [docs/analysis/residual-next-candidates.md](docs/analysis/residual-next-candidates.md) | 诚实 residual 队列 |
| [CHANGELOG.md](CHANGELOG.md) | 版本变更 |

---

## 开发

```bash
make build    # 构建
make test     # 运行全部测试
make vet      # go vet
make lint     # 代码检查
make vuln     # govulncheck 漏洞扫描
make bench-routing  # 路由权重选择 benchmark
make verify   # 本地发布门禁
make docker-verify  # 构建完整 Docker 镜像（需要 Docker）
```

---

## 相关项目

- [MetAPI (TypeScript)](https://github.com/cita-777/metapi)，原版 Node.js 实现
- [New API](https://github.com/QuantumNous/new-api)，主要上游之一
- [One API](https://github.com/songquanpeng/one-api)，经典 OpenAI 接口聚合

---

## 许可证

[MIT](LICENSE)
