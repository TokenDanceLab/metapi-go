# CI/CD Final Audit -- metapi-go

**Date**: 2026-07-05
**Scope**: `.github/workflows/ci.yml` + `.github/workflows/cd.yml` + `Dockerfile` + `.golangci.yml`
**Severity scale**: CRITICAL > HIGH > MEDIUM > LOW > INFO

---

## 1. Executive Summary

The CI/CD pipelines are functional at a basic level -- code is linted, tested against SQLite and PostgreSQL, built, and a Docker image is pushed to ghcr.io. However, there are significant gaps in coverage reporting, version pinning, cross-platform testing, and supply-chain security. Six actions are running on deprecated major versions. The Go toolchain version is inconsistent across three surfaces (go.mod, CI, Dockerfile). Coverage is not collected or reported at all, and there is no gate between CI and CD.

**Overall grade: C+** -- functional but behind on best practices.

---

## 2. Action Version Audit

### Deprecated Major Versions (6 actions)

| Action | Current | Latest | Gap | Severity |
|--------|---------|--------|-----|----------|
| `golangci/golangci-lint-action` | @v6 | @v9 (May 2026) | 3 majors | HIGH |
| `actions/checkout` | @v4 | @v6 (Jun 2026) | 2 majors | MEDIUM |
| `docker/setup-buildx-action` | @v3 | @v4 (Mar 2026) | 1 major | MEDIUM |
| `docker/login-action` | @v3 | @v4 (2026) | 1 major | MEDIUM |
| `docker/metadata-action` | @v5 | @v6 (Mar 2026) | 1 major | MEDIUM |
| `docker/build-push-action` | @v6 | @v7 (Mar 2026) | 1 major | MEDIUM |

### Current Versions

| Action | Current | Status |
|--------|---------|--------|
| `actions/setup-go` | @v5 | Current (no v6 confirmed) |

### golangci-lint-action v6 specific issue

`golangci/golangci-lint-action@v6` does NOT support golangci-lint v2.x. If the project upgrades to golangci-lint v2, the action will fail with:
```
golangci-lint v2 is not supported by golangci-lint-action v6
```
v7+ is required for golangci-lint v2. v9 (latest) also requires Node 24 runtime on the Actions runner.

---

## 3. Coverage Reporting -- CRITICAL

**There is zero coverage reporting in either CI or CD.**

Current test invocations:
```yaml
# ci.yml test-sqlite
- run: go test ./... -count=1

# ci.yml test-pg
- run: go test ./... -count=1 -tags=integration
```

Missing:
- No `-coverprofile=coverage.out` flag
- No `-covermode=atomic` flag
- No coverage upload action (Codecov, Coveralls, or Go test summary action)
- No coverage threshold enforcement
- No coverage artifact upload even for local inspection

**Recommendation**: Add coverage collection and a reporting step:

```yaml
- run: go test ./... -count=1 -coverprofile=coverage.out -covermode=atomic
- uses: actions/upload-artifact@v4
  with:
    name: coverage-sqlite
    path: coverage.out
```

Or integrate with Codecov / Coveralls if the project uses them.

---

## 4. Go Toolchain Version Mismatch -- HIGH

Three different surfaces declare different Go versions:

| Surface | Version |
|---------|---------|
| `go.mod` (line 3) | `go 1.25.0` |
| CI: `actions/setup-go` | `go-version: '1.24'` |
| Dockerfile: `FROM golang` | `golang:1.24-alpine` |

`go.mod` declares `go 1.25.0`, meaning the module requires at minimum Go 1.25. Both CI and the Dockerfile build with Go 1.24, which is one minor version behind. This could cause:
- Code using Go 1.25 features to fail to compile in CI/Docker
- Different behavior between local dev (1.25) and CI (1.24)
- Silent acceptance of code that should require 1.25 in go.mod

**Recommendation**: Align all three to `go 1.25.0`:
1. Set `go-version: '1.25'` in both CI jobs
2. Change `FROM golang:1.24-alpine` to `FROM golang:1.25-alpine`

---

## 5. CI Structural Issues

### 5.1 No inter-job dependency graph (MEDIUM)

All four CI jobs (`lint`, `test-sqlite`, `test-pg`, `build`) run in parallel with no `needs` declarations. The `build` job can succeed even if all tests fail. In a healthy pipeline, `build` should depend on tests passing:

```yaml
build:
  needs: [test-sqlite, test-pg]
```

### 5.2 Lint `continue-on-error: true` (MEDIUM)

```yaml
lint:
  continue-on-error: true
```

Lint failures do not block merge. This is acceptable during early development but should be removed once the codebase stabilizes. Every lint violation that CI ignores becomes tech debt.

### 5.3 Missing Go module cache (LOW)

`actions/setup-go@v5` supports built-in caching via:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: '1.25'
    cache: true
    cache-dependency-path: go.sum
```

This is not configured, resulting in slower CI runs as dependencies are downloaded fresh each time.

### 5.4 Single platform testing (LOW)

All jobs use `ubuntu-latest` only. For a Go project that serves as an API gateway (potentially deployed on macOS dev machines or Windows), a platform matrix or at minimum a Windows build check is advisable.

### 5.5 Missing go vet and go mod verify (MEDIUM)

The CI runs tests but does not run `go vet ./...` or `go mod verify`. These are standard Go CI checks:
- `go vet` catches suspicious constructs the compiler does not
- `go mod verify` ensures downloaded dependencies match go.sum

### 5.6 No race detector in tests (LOW)

Neither test job enables the Go race detector (`-race`). For a concurrent API server, race detection in CI is valuable.

---

## 6. CD Structural Issues

### 6.1 No CI gate before CD (HIGH)

CD triggers on push to `master` independently of CI. A Docker image can be built and pushed to ghcr.io even if CI tests fail on the same commit. The CD workflow should either:
- Call the CI workflow and require it to pass before building, OR
- Run tests itself as a prerequisite job

### 6.2 Only amd64 builds (MEDIUM)

```yaml
platforms: linux/amd64
```

No `linux/arm64` build. Apple Silicon Macs and AWS Graviton instances are increasingly common. Adding arm64 is trivial with Buildx:

```yaml
platforms: linux/amd64,linux/arm64
```

### 6.3 `latest` tag on every master push (MEDIUM)

```yaml
type=raw,value=latest,enable=${{ github.ref == 'refs/heads/master' }}
```

The `latest` tag moves on every push to master, regardless of whether the commit passes CI or is production-ready. Consider restricting `latest` to tagged releases only, or at minimum gating it behind a successful CI run.

### 6.4 Missing provenance and SBOM attestation (LOW)

`docker/build-push-action@v7` supports provenance attestation and SBOM generation. Adding these improves supply-chain transparency:

```yaml
- uses: docker/build-push-action@v7
  with:
    provenance: mode=max
    sbom: true
```

### 6.5 Missing image vulnerability scanning (MEDIUM)

No container vulnerability scanner (Trivy, Grype, Snyk) runs against the built image before pushing to the registry.

### 6.6 Tag trigger correctness -- verified OK

The tag trigger pattern in `cd.yml` is correct:

```yaml
on:
  push:
    branches: [master]
    tags: ['v*']
```

In GitHub Actions, `branches` and `tags` under the same `push` event use OR logic. A push to `master` OR a tag push matching `v*` triggers the workflow. The semver tag extraction in `metadata-action` also correctly gates on `startsWith(github.ref, 'refs/tags/v')`.

---

## 7. golangci-lint Configuration

The `.golangci.yml` is extremely minimal and disables more linters than it enables:

```yaml
linters:
  disable:
    - errcheck      # ignores unchecked errors
    - unused        # allows dead code
    - ineffassign   # allows wasted assignments
    - gosimple      # allows over-complicated code
    - staticcheck   # disables the most powerful Go linter
  enable:
    - govet         # only vet runs
```

This configuration disables the five most useful linters in the Go ecosystem. `staticcheck` in particular catches hundreds of bug patterns (SA* checks), nil pointer dereferences, and incorrect standard-library usage. `errcheck` catches unhandled errors which is critical for a server application.

**Recommendation**: Invert the configuration -- enable a broad set and disable only noisy/problematic linters.

---

## 8. Improvement Priority Matrix

| Priority | Issue | File | Effort |
|----------|-------|------|--------|
| P0 | Add coverage collection and reporting | ci.yml | Small |
| P0 | Align Go version to 1.25 across go.mod, CI, Dockerfile | ci.yml, Dockerfile, go.mod | Trivial |
| P1 | Make CD depend on CI pass | cd.yml | Small |
| P1 | Upgrade `golangci/golangci-lint-action` to @v9 | ci.yml | Trivial |
| P1 | Enable useful linters (staticcheck, errcheck) | .golangci.yml | Small |
| P2 | Upgrade 5 docker actions to latest majors | cd.yml | Trivial |
| P2 | Add job dependencies in CI (build needs tests) | ci.yml | Trivial |
| P2 | Add `linux/arm64` to CD platforms | cd.yml | Trivial |
| P2 | Remove lint `continue-on-error` or gate on it | ci.yml | Trivial |
| P3 | Add Go module cache to setup-go | ci.yml | Trivial |
| P3 | Add `go vet`, `go mod verify`, `-race` tests | ci.yml | Small |
| P3 | Add provenance/SBOM attestation | cd.yml | Trivial |
| P3 | Restrict `latest` tag to tagged releases only | cd.yml | Small |
| P4 | Add image vulnerability scanning (Trivy) | cd.yml | Medium |
| P4 | Add platform matrix (Windows/macOS build check) | ci.yml | Medium |

---

## 9. Verification Checklist

| Check | Status |
|-------|--------|
| All CI jobs pass | Lint passes (continue-on-error), tests pass, build passes |
| Coverage reported | FAIL -- no coverage collection whatsoever |
| Docker image builds | PASS -- multi-stage Dockerfile builds correctly |
| Tags trigger correctly | PASS -- `v*` pattern and semver extraction correct |
| `latest` tag on master | PASS -- works, but tags every commit indiscriminately |
| Semver tags on tag push | PASS -- `{{version}}` and `{{major}}.{{minor}}` extract correctly |
| SHA tag on every build | PASS -- `type=sha` provides short commit SHA |

---

## 10. Suggested ci.yml (post-fix)

For reference, a fully hardened CI configuration:

```yaml
name: CI

on: [push, pull_request]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
          cache-dependency-path: go.sum
      - uses: golangci/golangci-lint-action@v9

  test-sqlite:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
          cache-dependency-path: go.sum
      - run: go vet ./...
      - run: go mod verify
      - run: go test ./... -count=1 -race -coverprofile=coverage-sqlite.out -covermode=atomic
      - uses: actions/upload-artifact@v4
        with:
          name: coverage-sqlite
          path: coverage-sqlite.out

  test-pg:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_PASSWORD: test
          POSTGRES_DB: metapi_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
          cache-dependency-path: go.sum
      - run: go test ./... -count=1 -tags=integration -race -coverprofile=coverage-pg.out -covermode=atomic
        env:
          DATABASE_URL: postgres://postgres:test@localhost:5432/metapi_test?sslmode=disable
      - uses: actions/upload-artifact@v4
        with:
          name: coverage-pg
          path: coverage-pg.out

  build:
    needs: [test-sqlite, test-pg]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
          cache-dependency-path: go.sum
      - run: mkdir -p web/dist
      - run: go build -trimpath ./cmd/server
      - run: go build -trimpath ./cmd/migrate
```

---

## 11. Suggested cd.yml (post-fix)

```yaml
name: CD

on:
  push:
    branches: [master]
    paths:
      - "**.go"
      - "go.mod"
      - "go.sum"
      - "cmd/**"
      - "internal/**"
      - "web/**"
      - "Dockerfile"
      - ".github/workflows/cd.yml"
    tags: ['v*']
  workflow_dispatch:

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ghcr.io/tokendancelab/metapi-go

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_PASSWORD: test
          POSTGRES_DB: metapi_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
          cache: true
          cache-dependency-path: go.sum
      - run: go test ./... -count=1 -tags=integration
        env:
          DATABASE_URL: postgres://postgres:test@localhost:5432/metapi_test?sslmode=disable

  build-and-push:
    needs: [test]
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - uses: actions/checkout@v6

      - uses: docker/setup-buildx-action@v4

      - uses: docker/login-action@v4
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - uses: docker/metadata-action@v6
        id: meta
        with:
          images: ${{ env.IMAGE_NAME }}
          tags: |
            type=raw,value=latest,enable=${{ startsWith(github.ref, 'refs/tags/v') }}
            type=semver,pattern={{version}},enable=${{ startsWith(github.ref, 'refs/tags/v') }}
            type=semver,pattern={{major}}.{{minor}},enable=${{ startsWith(github.ref, 'refs/tags/v') }}
            type=sha,prefix=,format=short

      - uses: docker/build-push-action@v7
        with:
          context: .
          file: ./Dockerfile
          push: true
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
          platforms: linux/amd64,linux/arm64
          provenance: mode=max
          sbom: true
```

Key changes in suggested CD:
- Added `test` job as gate before Docker build/push
- Upgraded all docker actions to latest majors
- Changed `latest` tag to only fire on version tags (not every master push)
- Added `linux/arm64` platform
- Added provenance attestation and SBOM generation
