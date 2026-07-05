# MetAPI Go — Engineering Rules

Go rewrite of [MetAPI](https://github.com/cita-777/metapi). Feature parity with the original TypeScript version.

## Golden Rules

- **Feature parity**: Every behavior must match the TS reference (`D:\Code\TokenDance\metapi\src\server\`).
  When in doubt, read the TS source.
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
store/                  DB layer (27 tables, sqlx)
auth/                   Admin + proxy auth + rate limiting
routing/                TokenRouter (Fibonacci + weighted random)
proxy/                  ProxyCore (dual-loop orchestration)
platform/               14 upstream adapters
transform/              4-protocol SSE conversion
service/                Checkin, balance, notify, OAuth, backup
scheduler/              15 background jobs
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

- `docs/specs/p0-skeleton.md` through `p13-embed-ci.md` — per-phase implementation specs
- `docs/specs/review/` — cross-reference reviews + audit reports
- `docs/plan/` — dependency graph + milestones + fix plan
- `docs/progress/MASTER.md` — current project status
- `docs/analysis/` — S.U.P.E.R architecture assessment
- `docs/architecture.md` — Go architecture overview
- `docs/deployment.md` — deployment guide
- `docs/api.md` — admin API reference
- `docs/migration.md` — TS→Go migration

## Related Repos

- TS source (reference): `D:\Code\TokenDance\metapi`
- Gateway (production NewAPI fork): `D:\Code\TokenDance\tokendance-gateway`
- MetAPI skill (ops reference): `C:\Users\Ding\.claude\skills\metapi\SKILL.md`
