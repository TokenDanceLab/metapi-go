# P0: Go 项目骨架 + Config + Chi Router + Docker

**S.U.P.E.R**: E (环境无关) · R (可替换) | **依赖**: 无 | **Size**: M

## 原始 TS 参考
- `D:\Code\TokenDance\metapi\src\server\config.ts` — 全量 config (~180 行)
- `D:\Code\TokenDance\metapi\src\server\index.ts` — 启动流程
- `D:\Code\TokenDance\metapi\.env.example` — env var 清单
- `D:\Code\TokenDance\metapi\Dockerfile.slim` — 构建/部署参考

## Go 目录结构

```
cmd/server/main.go          # main: config.Load → store.Open → migrate → router.New → app.Run
config/
  config.go                  # Config struct + Load() from env
  defaults.go                # 所有默认值常量
router/
  router.go                  # chi.NewRouter() 组装
  middleware.go              # CORS, request logger, recovery, rate-limit skeleton
app/
  app.go                     # App{Config,Store,Router,Server} + Start/Shutdown
  health.go                  # GET /health
Dockerfile                   # 多阶段: go build + frontend build → scratch
docker-compose.yml           # 开发用, 挂载 web/dist + data/
Makefile                     # build test lint docker-build run
```

## 功能规格

### Config 完整映射
`config.Load()` 读取环境变量 → 填充 Config struct。优先级: env > .env > 默认值。

所有 env var 保持与 TS 版完全一致的命名 (不添加前缀)。

### Chi Router 路由顺序
```go
r := chi.NewRouter()
r.Use(middleware.Logger)           // slog 结构化
r.Use(middleware.Recoverer)        // panic recovery
r.Use(middleware.RealIP)           // X-Forwarded-For
r.Use(corsMiddleware)              // CORS allow all origins
r.Get("/health", app.Health)       // k8s/docker healthcheck

r.Route("/api", func(r chi.Router) {
    r.Use(auth.AdminAuth)          // Bearer token + IP allowlist
    // P3-P11 注册具体路由
})

r.Route("/v1", func(r chi.Router) {
    r.Use(auth.ProxyAuth)          // managed key / global proxy token
    // P10 注册代理路由
})

// SPA fallback (最后)
r.NotFound(serveSPA(webFS))
```

### Graceful Shutdown
```go
srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}
go srv.ListenAndServe()
<-sigCh // SIGINT/SIGTERM
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
srv.Shutdown(ctx)
store.Close()
```

### Dockerfile
```dockerfile
# Stage 1: build frontend (复用现有 React SPA)
FROM node:25-alpine AS web
COPY web/ /web
WORKDIR /web
RUN npm ci && npm run build:web

# Stage 2: build Go
FROM golang:1.24-alpine AS go
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi ./cmd/server

# Stage 3: runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=web /web/dist /app/web/dist
COPY --from=go /app/metapi /app/metapi
EXPOSE 4000
CMD ["/app/metapi"]
```

## Acceptance Criteria
- [ ] `go build ./cmd/server` 通过, 无编译错误
- [ ] `./metapi` 启动 → `curl :4000/health` → `{"status":"ok"}`
- [ ] 所有 env var 解析正确 (bool "1"/"true"/"yes"/"on", int clamp, csv split, JSON parse)
- [ ] 缺失必填项时报清晰错误 (不是 panic trace)
- [ ] SIGTERM 后 5s 内优雅退出
- [ ] Docker 镜像 <20MB
- [ ] `make build` `make test` `make lint` `make docker-build` 全部可用
- [ ] slog 日志格式: `{"time":"...","level":"INFO","msg":"listening","port":4000}`

## Test Plan
| 文件 | 测试内容 |
|------|----------|
| `config/config_test.go` | 每个 parse 函数单元测试; 默认值; 边界值 |
| `app/app_test.go` | graceful shutdown 时序 |
| `router/router_test.go` | 路由注册顺序; SPA fallback 正确返回 index.html |

## Edge Cases
- `DB_TYPE=postgres` 但 `DB_URL` 为空 → 报错退出
- `PORT` 为负数/非数字 → 使用默认值 4000
- `CHECKIN_INTERVAL_HOURS` 超出 1-24 → clamp
- `TOKEN_ROUTER_FAILURE_COOLDOWN_MAX_SEC` 超过 30 天 → cap
- `.env` 文件不存在 → 不报错, 使用默认值
