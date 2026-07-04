# P13: Frontend Embed + SQLite→PG Migration Tool + CI/CD + Docs

**S.U.P.E.R**: E (环境无关) · R (可替换) | **依赖**: P0-P12 (全部) | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\vite.config.ts` — 前端构建配置
- `D:\Code\TokenDance\metapi\Dockerfile.slim` — 现有 Docker 部署
- `D:\Code\TokenDance\metapi\docker\docker-compose.yml`
- `D:\Code\TokenDance\metapi\src\server\services\databaseMigrationService.ts` — 跨方言迁移
- `D:\Code\TokenDance\metapi\.github\` — CI workflows

## Go 模块结构
```
cmd/server/main.go        # + embed 前端静态文件
cmd/migrate/main.go       # 独立迁移工具: SQLite → PG
web/                      # (已存在) React SPA 源码
web/dist/                 # (构建产物, gitignore) embed 目标
.github/workflows/
  ci.yml                  # Go lint + test + build (SQLite + PG)
  cd.yml                  # Docker build + push to ghcr.io
Dockerfile                # 多阶段: frontend build + go build → scratch
docker-compose.yml        # 开发/生产 compose
docs/
  deployment.md           # 部署指南
  architecture.md         # Go 版架构概览
  api.md                  # API 参考 (迁移自 TS 版前端)
  migration.md            # TS → Go 迁移指南
```

## 功能规格

### 1. Frontend Embed
```go
//go:embed web/dist
var webFS embed.FS

// chi router
r.NotFound(func(w http.ResponseWriter, r *http.Request) {
    // 排除 API 路径
    if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") {
        http.NotFound(w, r)
        return
    }
    // SPA fallback
    data, err := webFS.ReadFile("web/dist/index.html")
    if err != nil { http.NotFound(w, r); return }
    w.Header().Set("Content-Type", "text/html")
    w.Write(data)
})
// 静态资源
r.Handle("/assets/*", http.FileServer(http.FS(webFS)))
```

### 2. SQLite → PG Migration Tool
```bash
# 独立二进制
go build -o metapi-migrate ./cmd/migrate

# 使用
metapi-migrate --from sqlite://data/hub.db --to postgres://user:pass@host/db
```

功能:
- 读取 SQLite 的所有 27 张表数据
- 批量插入 PG (事务包装, 每 1000 行一批)
- 处理类型转换 (INTEGER 0/1 → BOOLEAN, TEXT datetime → TEXT)
- 处理自增 ID 冲突 (SETVAL 同步序列)
- 进度条 + 校验和验证
- Dry-run 模式

### 3. CI/CD

**ci.yml**:
```yaml
on: [push, pull_request]
jobs:
  lint:     golangci-lint
  test:     go test ./... (SQLite :memory:)
  test-pg:  services: postgres (testcontainers)
  build:    go build ./cmd/server
```

**cd.yml**:
```yaml
on:
  push:
    tags: ['v*']
jobs:
  docker:
    - Build multi-arch (amd64) Docker image
    - Push to ghcr.io/tokendancelab/metapi-go:latest + :vX.Y.Z
```

### 4. Dockerfile (最终版)
```dockerfile
# Stage 1: Frontend build
FROM node:25-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build:web

# Stage 2: Go build
FROM golang:1.24-alpine AS go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi ./cmd/server

# Stage 3: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl
COPY --from=go /app/metapi /usr/local/bin/metapi
EXPOSE 4000
ENV DATA_DIR=/app/data
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s CMD curl -f http://localhost:4000/health || exit 1
CMD ["metapi"]
```

### 5. Documentation

| 文档 | 内容 |
|------|------|
| `README.md` | 项目简介, 特性, 快速开始 (go build + docker) |
| `docs/deployment.md` | 完整部署指南: env vars, docker-compose, nginx reverse proxy, TLS |
| `docs/architecture.md` | Go 版架构概览, 与 TS 版差异, S.U.P.E.R 改进 |
| `docs/api.md` | Admin API 参考 (与 TS 版一致) |
| `docs/migration.md` | TS → Go 迁移步骤: 停服 → dump → 切换 binary → 验证 |
| `docs/specs/*.md` | (本次创建的) 14 个 spec 文件 |

## Acceptance Criteria
- [ ] `go build ./cmd/server` 产出包含前端的单二进制
- [ ] `./metapi` 启动 → 浏览器打开 `:4000` → 看到 React SPA
- [ ] `./metapi-migrate` SQLite → PG 数据完整转移
- [ ] CI: go test 全绿 (SQLite + PG 两个 runner)
- [ ] Docker 镜像构建成功, push 到 ghcr.io
- [ ] Docker 镜像 <25MB (含前端)
- [ ] Healthcheck 正常
- [ ] 文档完整: deployment + architecture + api + migration

## Test Plan
| 内容 | 验证方式 |
|------|----------|
| `go test ./...` | CI 自动运行 |
| `go build ./cmd/server` | CI 构建检查 |
| SQLite → PG 迁移 | 测试: 创建 SQLite DB → migrate → 对比 PG 行数/checksum |
| Docker healthcheck | `docker run` → `curl :4000/health` |

## Edge Cases
- web/dist 不存在 → go build 失败, 提示先构建前端
- Migration: PG 已有数据 → 默认 skip, 需要 `--overwrite` 标志
- Migration: SQLite 表为空 → 仍然成功 (0 行迁移)
- Docker: data 目录为空 → 自动创建 + migration
