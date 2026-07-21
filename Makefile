.PHONY: p0585-probe p0585-e2e build test race race-integration vet lint vuln mod-verify docs-hygiene bench-routing coverage verify verify-race docker-verify run docker-build clean web-build migrate-build ui-visual ui-e2e

# Build the server binary (requires web/dist/ to exist for go:embed)
build:
	go build -trimpath -ldflags="-s -w" -o metapi ./cmd/server

# Run tests
test:
	go test ./... -count=1 -timeout=60s

# Run tests with the Go race detector
race:
	go test ./... -count=1 -race -timeout=120s

# Run integration tests with the Go race detector (requires PG_TEST_DSN)
race-integration:
	go test ./... -count=1 -race -tags=integration -timeout=180s

# Run go vet
vet:
	go vet ./...

# Run linter (requires golangci-lint)
lint:
	golangci-lint run --timeout=3m ./...

# Run dependency vulnerability scan
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Verify downloaded modules match go.sum checksums
mod-verify:
	go mod verify

# Check public Markdown for local paths, credential examples, AI citation artifacts, and unsupported runtime claims
docs-hygiene:
	go test ./docs -run TestPublicMarkdownHygiene -count=1

# Run routing benchmark smoke set with allocation reporting
bench-routing:
	go test ./routing -run '^$$' -bench '^BenchmarkCalculateWeightedSelection' -benchmem -count=5

# Generate aggregate coverage profile
coverage:
	go test ./... -count=1 -coverprofile=coverage.out

# Local release gate
verify: docs-hygiene mod-verify test vet lint vuln build migrate-build

# Local release gate plus race detector (requires a working CGO/C toolchain)
verify-race: verify race

# Local container release gate (requires Docker)
docker-verify: docker-build

# Run the server locally
run:
	go run ./cmd/server

# Build Docker image (multi-stage: frontend + Go)
docker-build:
	docker build -t metapi-go:latest .

# Build the React frontend (requires Node.js)
web-build:
	cd web && npm ci --ignore-scripts && npm run build:web

# Playwright UX e2e smoke (theme / FOUC / login surface) — requires Node + Chromium
ui-e2e:
	cd web && npm run test:e2e

# Playwright visual baselines for /__design__ (skips if gallery route missing)
ui-visual:
	cd web && npm run test:visual

# Build the standalone migration tool
migrate-build:
	go build -trimpath -ldflags="-s -w" -o metapi-migrate ./cmd/migrate

# Clean build artifacts
clean:
	rm -f metapi metapi.exe metapi-migrate metapi-migrate.exe
	rm -rf web/dist

# P0-585 cascade probe (dry-run by default; set METAPI_P0585_LIVE=1 for authorized live)
p0585-probe:
	python scripts/p0585_cascade_probe.py

p0585-e2e:
	go test ./e2e -count=1 -run 'P0585HTTP' -timeout 60s
