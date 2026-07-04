.PHONY: build test lint run docker-build clean web-build migrate-build

# Build the server binary (requires web/dist/ to exist for go:embed)
build:
	go build -ldflags="-s -w" -o metapi ./cmd/server

# Run tests
test:
	go test ./... -count=1 -timeout=60s

# Run linter (requires golangci-lint)
lint:
	golangci-lint run ./...

# Run the server locally
run:
	go run ./cmd/server

# Build Docker image (multi-stage: frontend + Go)
docker-build:
	docker build -t metapi-go:latest .

# Build the React frontend (requires Node.js)
web-build:
	cd web && npm ci && npx vite build

# Build the standalone migration tool
migrate-build:
	go build -ldflags="-s -w" -o metapi-migrate ./cmd/migrate

# Clean build artifacts
clean:
	rm -f metapi metapi.exe metapi-migrate metapi-migrate.exe
	rm -rf web/dist
