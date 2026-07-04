# MetAPI Go

<div align="center">

[![CI](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml/badge.svg)](https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml)
[![CD](https://github.com/TokenDanceLab/metapi-go/actions/workflows/cd.yml/badge.svg)](https://github.com/TokenDanceLab/metapi-go/actions/workflows/cd.yml)
[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Docker](https://img.shields.io/badge/ghcr-v0.2.0-blue?logo=docker)](https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go)

**AI API 聚合平台的元层管理与统一代理网关。**

[TokenDance Lab](https://github.com/TokenDanceLab) 对 [MetAPI](https://github.com/cita-777/metapi) 的 Go 语言重写。
单静态二进制。无需 Node.js 运行时。功能完全对等。

[English](README.md)

</div>

---

## 什么是 MetAPI？

MetAPI 是一个统一代理网关，位于你的 AI 应用与多个 API 提供商之间。它管理 API 密钥、自动签到获取免费额度、跨提供商负载均衡，并在 AI 协议格式之间透明转换（OpenAI、Anthropic、Gemini、Codex）。

**适用场景：**
- 将多个 NewAPI/OneAPI 站点的免费 API 密钥聚合到一个端点
- 自动执行每日签到以维持积分余额
- 根据成本、延迟和可用性智能路由请求
- 部署单个 OpenAI 兼容的 `/v1/chat/completions` 端点，后端由几十个上游密钥支持
- 需要轻量级、高性能、低资源占用的部署

## 为什么用 Go？

| | Node.js (原版) | Go (本版) |
|---|---|---|
| **内存占用** | 85 MB | ~20 MB |
| **Docker 镜像** | ~250 MB | ~15 MB |
| **启动时间** | 5-10 秒 | <100 毫秒 |
| **二进制** | 依赖 Node 25 运行时 | 单个 7 MB 文件 |
| **并发能力** | 事件循环 + worker 线程 | goroutines（轻量、多核） |
| **部署方式** | `npm rebuild` 原生插件 | 拷贝一个文件 |

## 功能特性

### AI 协议代理
- **OpenAI 兼容** `/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`、`/v1/images`、`/v1/models`
- **Anthropic** `/v1/messages`、`/v1/messages/count_tokens`
- **Gemini** 原生接口
- **Codex** responses 直通，支持 WebSocket
- **实时协议转换** — 发送 Anthropic 请求，获得 OpenAI 响应，反之亦然
- **SSE 流式传输** 零缓冲逐块转换

### 路由引擎
- **3 种策略**：加权随机、轮询、稳定优先（含观察门控）
- **斐波那契退避** 冷却：`15 × fib(失败次数)`，上限 30 天
- **站点级断路器** 含半开探测
- **粘滞会话** 维持对话连续性
- **按 API 密钥的路由策略**：模型白名单、站点排除、权重倍数
- **会话级通道并发限制** 含排队

### 账号管理
- **14 个平台适配器**：NewAPI、OneAPI、OneHub、DoneHub、Veloera、Sub2API、OpenAI、Claude、Codex、Gemini、Gemini CLI、Antigravity、CliProxyAPI、AnyRouter
- **自动平台检测** 通过 URL 探测和标题分析
- **每日签到自动化**（cron 或间隔模式）含奖励解析
- **余额追踪** 与价值评分
- **API 密钥提取** 与同步
- **OAuth 集成**（Codex、Claude、Gemini CLI、Antigravity）含 PKCE S256

### 运维能力
- **5 通道通知**：Webhook、Bark、Server酱、Telegram、SMTP
- **备份/恢复**：完整 27 表 JSON 导出/导入，支持 WebDAV 同步
- **SQLite→PostgreSQL 迁移** 工具
- **用量分析**：按站点、按模型、按天/小时聚合
- **管理面板**：React SPA，20+ 页面
- **速率限制**：按 IP 令牌桶（管理 100 rps，OAuth 10 rps）
- **15 个后台调度器**覆盖签到、余额、探测、清理、聚合

## 快速开始

### Docker（推荐）

```bash
docker run -d \
  -p 4000:4000 \
  -v ./data:/app/data \
  -e AUTH_TOKEN=your-admin-token \
  -e PROXY_TOKEN=sk-your-proxy-token \
  ghcr.io/tokendancelab/metapi-go:latest
```

浏览器打开 `http://localhost:4000`。

### 从源码构建

```bash
# 前置条件：Go 1.24+
git clone https://github.com/TokenDanceLab/metapi-go.git
cd metapi-go

# 前端已预构建并嵌入 — 直接构建运行
go build -o metapi ./cmd/server
AUTH_TOKEN=admin PROXY_TOKEN=sk-proxy ./metapi
```

### Docker Compose

```bash
cp .env.example .env
# 编辑 .env 填入你的 token
docker compose up -d
```

## 代理使用

将任意 OpenAI 兼容客户端指向你的 MetAPI 实例：

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-your-proxy-token" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'
```

## 从 TypeScript 版迁移

```bash
# 1. 构建迁移工具
go build -o metapi-migrate ./cmd/migrate

# 2. 停止旧版 TS 服务

# 3. 迁移 SQLite → PostgreSQL（可选）
./metapi-migrate --from sqlite://data/hub.db --to postgres://user:pass@host/db --progress --verify

# 4. 用相同的环境变量启动 — 无缝替换
AUTH_TOKEN=... PROXY_TOKEN=... ./metapi
```

详见 [docs/migration.md](docs/migration.md)。

## 配置

所有配置通过环境变量设置 — 与原 TypeScript 版完全一致。

| 变量 | 默认值 | 说明 |
|----------|---------|-------------|
| `AUTH_TOKEN` | `change-me-admin-token` | 管理 API 令牌 |
| `PROXY_TOKEN` | `change-me-proxy-sk-token` | 全局代理 API 密钥 |
| `PORT` | `4000` | HTTP 监听端口 |
| `DB_TYPE` | `sqlite` | `sqlite` 或 `postgres` |
| `DB_URL` | `./data/hub.db` | 数据库连接串 |
| `CHECKIN_CRON` | `0 8 * * *` | 每日签到计划 |
| `BALANCE_REFRESH_CRON` | `0 * * * *` | 余额刷新频率 |

完整配置见 [`.env.example`](.env.example)（约 100 项）。

## 项目结构

```
├── cmd/server/          # 主程序入口
├── cmd/migrate/         # SQLite→PG 迁移工具
├── config/              # 配置加载（~100 个 env var）
├── store/               # 数据库层（27 表，SQLite + PG 双方言）
├── auth/                # 认证中间件 + 速率限制
├── routing/             # 路由引擎（斐波那契冷却 + 加权随机）
├── proxy/               # 代理核心（双层循环编排）
├── platform/            # 14 个上游平台适配器
├── transform/           # 4 协议 SSE 转换
├── service/             # 业务逻辑（签到/余额/通知/OAuth/备份）
├── scheduler/           # 15 个后台调度器
├── handler/admin/       # 管理 API（~100 端点）
├── handler/proxy/       # 代理路由（11 接口面）
├── web/dist/            # 前端静态文件（构建产物，已嵌入）
└── docs/                # 文档与规格
```

## 文档

| 文档 | 说明 |
|----------|-------------|
| [docs/deployment.md](docs/deployment.md) | 部署指南：Docker、nginx、TLS |
| [docs/architecture.md](docs/architecture.md) | Go 版架构概览 |
| [docs/api.md](docs/api.md) | 管理 API 参考 |
| [docs/migration.md](docs/migration.md) | TS → Go 迁移指南 |
| [docs/specs/](docs/specs/) | 14 份实现规格文档 |

## 开发

```bash
make build          # 构建
make test           # 运行全部测试
make lint           # 运行代码检查
make docker-build   # 构建 Docker 镜像
```

## 相关项目

- [MetAPI (TypeScript)](https://github.com/cita-777/metapi) — 原版 Node.js 实现
- [TokenDance Gateway](https://github.com/TokenDanceLab/tokendance-gateway) — 生产级 NewAPI fork

## 许可证

MIT © [TokenDance Lab](https://github.com/TokenDanceLab)
