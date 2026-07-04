.PHONY: build test lint run docker-build clean

# Build the server binary
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

# Build Docker image
docker-build:
	docker build -t metapi-go:latest .

# Clean build artifacts
clean:
	rm -f metapi metapi.exe
