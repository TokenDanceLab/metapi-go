# Stage 1: Frontend build
FROM node:25-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# npx vite build instead of npm run build:web: skips desktop:icons
# (Electron icon generation) which is irrelevant in a container.
RUN npx vite build

# Stage 2: Go build
FROM golang:1.24-alpine AS go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi ./cmd/server
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o metapi-migrate ./cmd/migrate

# Stage 3: Runtime
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
