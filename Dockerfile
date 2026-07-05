# Stage 1: Go build
FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o metapi ./cmd/server
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w" -o metapi-migrate ./cmd/migrate

# Stage 2: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata curl
RUN addgroup -g 1001 -S appgroup && adduser -u 1001 -S appuser -G appgroup
RUN mkdir -p /app/data && chown -R appuser:appgroup /app/data
COPY --from=build /app/metapi /usr/local/bin/metapi
COPY --from=build /app/metapi-migrate /usr/local/bin/metapi-migrate
USER appuser
EXPOSE 4000
ENV DATA_DIR=/app/data
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/metapi", "healthcheck"]
CMD ["/usr/local/bin/metapi"]