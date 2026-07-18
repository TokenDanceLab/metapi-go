# MetAPI Go — Engineering Rules

Go rewrite of [MetAPI](https://github.com/cita-777/metapi). Feature parity with the original TypeScript version.

## Golden Rules

- **Feature parity**: Every behavior must match the original TypeScript MetAPI server.
  Keep the TS reference checkout outside this public repo, and do not document local checkout paths.
- **Single binary**: The React SPA is pre-built and embedded via `go:embed`. Do not add `npm`/`node` to the
  production image.
- **Dual dialect**: SQLite (dev/test) and PostgreSQL (production). Use `store.Open(dialect, dsn)`. Never
  assume SQLite-only features.
- **API compatibility**: All JSON responses must use camelCase field names matching the TS frontend.
  All env var names are identical to the TS version (no prefix).
- **Before pushing**: `go build ./cmd/server && go vet ./... && go test ./... -count=1 -race` must pass.
  🚫 **严禁跳过本地 CI** — GitHub Actions 是验证闸不是调试环境。pre-push hook 强制拦截。

## Project Structure

```
cmd/server/main.go      Entry point
cmd/migrate/main.go     SQLite→PG migration tool
config/                 ~100 env vars from config.Load()
store/                  DB layer (28 tables, sqlx)
auth/                   Admin + proxy auth + rate limiting
routing/                TokenRouter (Fibonacci + weighted random)
proxy/                  ProxyCore (dual-loop orchestration)
platform/               14 upstream adapters
transform/              4-protocol SSE conversion
service/                Checkin, balance, notify, OAuth, backup
scheduler/              16 background jobs
handler/admin/          ~144 admin REST endpoints
handler/proxy/           ~30 proxy routes (OpenAI, Gemini, Claude, Codex, Files)
web/dist/               Pre-built React SPA (embedded)
docs/specs/             14 implementation specifications
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/go-chi/chi/v5` | HTTP router |
| `github.com/jmoiron/sqlx` | DB access |
| `modernc.org/sqlite` | Pure-Go SQLite (no CGO) |
| `github.com/jackc/pgx/v5` | PostgreSQL driver |
| `github.com/robfig/cron/v3` | Cron scheduler |

## Build & Test

```bash
go build -o metapi ./cmd/server       # Build server
go build -o metapi-migrate ./cmd/migrate  # Build migration tool
go test ./... -count=1 -race          # Run all tests with race detector
go vet ./...                          # Static analysis
golangci-lint run --timeout=3m        # Lint check
```

## Release Workflow

1. 确保本地 CI 全部通过（pre-push hook 自动检查）
2. 更新 `CHANGELOG.md`（按 Keep a Changelog 格式）
3. Tag + push：`git tag -a vX.Y.Z -m "vX.Y.Z — 简述"` → `git push origin vX.Y.Z`
4. Tag push 触发 GitHub Actions `release.yml` → 自动创建 GitHub Release
5. CD 自动构建 Docker 镜像推送到 `ghcr.io/tokendancelab/metapi-go:vX.Y.Z`

**版本号**：`vMAJOR.MINOR.PATCH`（SemVer 2.0）
- PATCH：bug 修复
- MINOR：新功能/性能优化
- MAJOR：不兼容 API 变更
- v0.x 阶段 minor 可用于新功能

## CI Discipline

- **本地不过不推 GitHub**：所有 push 前必须先通过 `go vet ./... && go test ./... -count=1 -race`
- git pre-push hook（`.githooks/pre-push`）自动拦截未通过本地 CI 的 push
- 紧急跳过：`git push --no-verify`
- Claude Code hook（`~/.claude/hooks/metapi-go-push-guard.sh`）额外兜底
- GitHub Actions 的算力是最后验证闸，不是调试环境

## Specs & Docs

**Map (start here):** [`docs/README.md`](docs/README.md)

| Path | Role |
|------|------|
| `docs/STATE.md` | **现状 SSOT** (verified product facts; keep slim) |
| `docs/progress/MASTER.md` | **开放项 + 硬门禁** (not a changelog) |
| `docs/log.md` | **进度日志** append-only (never overrides STATE) |
| `docs/architecture.md` | As-built package map (proxy/transform/routing; not proxycore/protocol) |
| `docs/design/BACKEND.md` | Backend philosophy, dependency rules, forbidden imports |
| `docs/design/DESIGN.md` | UI design system SSOT |
| `docs/analysis/residual-next-candidates.md` | Honest residual queue (what is NOT product yet) |
| `docs/analysis/original-gap-matrix.md` | Upstream parity evidence |
| `docs/api.md` / `docs/deployment.md` / `docs/migration.md` | API · deploy · migration |
| `docs/specs/` | Rewrite-era phase specs (large; historical) |
| `CHANGELOG.md` | Version narrative |

**Progress roles:** STATE = 现状 · MASTER = 开放门禁 · LOG = 时间线。Temporary HANDOFF/session summaries are **not** SSOT — archive or delete after use.  
**Ops host/image pin** lives in server `projects/metapi/STATE.md` (may lag this repo tip).  
**Honesty:** Prefer 501 / documented residual over stub theater. Do not claim cluster-wide sticky or WS product without the matching Milestone.

## Related References

- TS source: original TypeScript MetAPI repository, checked out separately when parity work needs it.
- Gateway fork: private deployment repository, not part of this public repo.
- Ops skill: operator-local reference only. Do not publish private filesystem paths.
