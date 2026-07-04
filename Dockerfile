# Stage 1: Go build
FROM golang:1.25-alpine AS go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o metapi ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o metapi-migrate ./cmd/migrate

# Stage 2: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl
RUN mkdir -p /app/data
COPY --from=go /app/metapi /usr/local/bin/metapi
COPY --from=go /app/metapi-migrate /usr/local/bin/metapi-migrate
EXPOSE 4000
ENV DATA_DIR=/app/data
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:4000/health || exit 1
CMD ["metapi"]
