# Stage 1: build Go binary
FROM golang:1.24-alpine AS go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi ./cmd/server

# Stage 2: runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
ENV DATA_DIR=/app/data
WORKDIR /app
RUN mkdir -p /app/data
COPY --from=go /app/metapi /app/metapi
EXPOSE 4000
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:4000/health || exit 1
CMD ["/app/metapi"]
