# MetAPI Go

<div align="center">

**中转站的中转站 — 将分散的 AI 中转站聚合为一个统一网关**

Go 语言重写版。单二进制部署，无需 Node.js 运行时，与原 TypeScript 版功能完全对等。

<p align="center">
  <a href="README.md"><strong>中文</strong></a> |
  <a href="README_EN.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/TokenDanceLab/metapi-go/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/TokenDanceLab/metapi-go/actions/workflows/cd.yml"><img alt="CD" src="https://github.com/TokenDanceLab/metapi-go/actions/workflows/cd.yml/badge.svg"></a>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go">
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-MIT-green"></a>
  <a href="https://github.com/TokenDanceLab/metapi-go/pkgs/container/metapi-go"><img alt="Docker" src="https://img.shields.io/badge/ghcr-v0.4.0-blue?logo=docker"></a>
</p>

</div>

---

## 这是什么？

MetAPI 帮你把各处注册的 New API / One API / OneHub / DoneHub / Veloera / Sub2API 等站点，汇聚成**一个 API Key、一个入口**。

把 API 中转站当做模型供应商，MetAPI 就是你的统一网关——自动发现模型、智能路由请求、每天帮你签到领额度。

**Go 版相比原 TypeScript 版**：内存 85MB → 20MB，Docker 镜像 250MB → 15MB，启动 5-10s → 0.1s。单文件部署，拷贝即运行。

## 为什么用 Go？

| | Node.js (原版) | Go (本版) |
|---|---|---|
| 内存占用 | 85 MB | ~20 MB |
| Docker 镜像 | ~250 MB | ~15 MB |
| 启动时间 | 5-10 秒 | <0.1 秒 |
| 部署方式 | 需要 Node.js 运行时 | 单个二进制文件 |
| 并发能力 | 事件循环 | goroutines 多核并行 |

## 快速开始

### Docker（推荐）

```bash
docker run -d -p 4000:4000 \
  -v ./data:/app/data \
  -e AUTH_TOKEN=your-admin-token \
  -e PROXY_TOKEN=sk-your-proxy-token \
  ghcr.io/tokendancelab/metapi-go:latest
```

打开 `http://localhost:4000`，用 AUTH_TOKEN 登录。

### 从源码

```bash
git clone https://github.com/TokenDanceLab/metapi-go.git
cd metapi-go
go build -o metapi ./cmd/server
AUTH_TOKEN=admin PROXY_TOKEN=sk-proxy ./metapi
```

### 使用代理

```bash
curl http://localhost:4000/v1/chat/completions \
  -H "Authorization: Bearer sk-your-proxy-token" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"你好"}]}'
```

## 功能

### 协议代理
- `/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`、`/v1/models` 等 OpenAI 兼容端点
- `/v1/messages` Anthropic 原生端点
- Gemini、Codex 原生接口
- **实时协议转换**：发送 Anthropic 请求，获得 OpenAI 响应（或反过来）
- SSE 流式传输

### 路由引擎
- 加权随机、轮询、稳定优先三种策略
- 斐波那契退避冷却（`15 × fib(失败次数)`，上限 30 天）
- 站点级断路器 + 半开探测
- 粘滞会话
- 按 API Key 的细粒度路由策略

### 账号管理
- 14 个平台适配器自动检测
- 每日自动签到 + 奖励解析
- 余额追踪 + 价值评分
- OAuth PKCE 登录（Codex、Claude、Gemini CLI、Antigravity）

### 运维
- Webhook / Bark / Server酱 / Telegram / SMTP 五通道通知
- 27 表完整备份/恢复（支持 WebDAV 同步）
- SQLite → PostgreSQL 迁移工具
- 速率限制（100rps 管理，10rps OAuth）
- 15 个后台调度器

## 配置

所有环境变量与原 TypeScript 版完全一致，无缝替换。

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AUTH_TOKEN` | `change-me-admin-token` | 管理员令牌 |
| `PROXY_TOKEN` | `change-me-proxy-sk-token` | 代理 API Key |
| `PORT` | `4000` | 监听端口 |
| `DB_TYPE` | `sqlite` | 数据库类型（`sqlite` / `postgres`） |
| `CHECKIN_CRON` | `0 8 * * *` | 签到时间 |

完整配置见 [`.env.example`](.env.example)。

## 从 TypeScript 版迁移

```bash
# 1. 停止旧服务
# 2. 启动 Go 版（数据库文件和环境变量通用）
./metapi
```

数据库 Schema 完全一致，Go 版启动时自动执行幂等 migration。

## 项目结构

```
cmd/server/          主程序
cmd/migrate/         SQLite→PG 迁移工具
config/              配置（~100 环境变量）
store/               数据库（27 表，双方言）
auth/                认证 + 速率限制
routing/             路由引擎
proxy/               代理核心
platform/            14 平台适配器
transform/           四协议 SSE 转换
service/             业务逻辑
scheduler/           15 后台任务
handler/admin/       管理 API
handler/proxy/       代理端点
web/dist/            前端（构建产物，已嵌入）
```

## 相关项目

- [MetAPI (TypeScript)](https://github.com/cita-777/metapi) — 原版 Node.js 实现
- [TokenDance Gateway](https://github.com/TokenDanceLab/tokendance-gateway) — 生产级 NewAPI fork

## 许可证

MIT © [TokenDance Lab](https://github.com/TokenDanceLab)
