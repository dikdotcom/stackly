# syntax=docker/dockerfile:1.7

# ─────────────────────────────────────────────────────────────
# Stage 1: Build the Go binary
# ─────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache deps first
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY data/ ./data/
COPY web/ ./web/

# Build server
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=docker" \
    -o /out/stackly-server ./cmd/server

# Build CLI
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /out/stackly-cli ./cmd/techstack


# ─────────────────────────────────────────────────────────────
# Stage 2: Runtime — use chromedp/headless-shell for Chrome
# ─────────────────────────────────────────────────────────────
FROM chromedp/headless-shell:latest AS runtime

# Add labels
LABEL org.opencontainers.image.title="Stackly"
LABEL org.opencontainers.image.description="Multi-tenant tech stack detection platform — headless browser scanning with fingerprint DB, top-level category grouping, and contact email extraction"
LABEL org.opencontainers.image.source="https://github.com/dikdotcom/stackly"

# Install minimal runtime deps (tini for proper signal handling, curl for healthcheck)
USER root
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates tini curl \
    && rm -rf /var/lib/apt/lists/*

# Switch to non-root user (chromedp/headless-shell already has 'chrome' user)
USER chrome

WORKDIR /app

# Copy binary + assets from builder
COPY --from=builder --chown=chrome:chrome /out/stackly-server /app/stackly-server
COPY --from=builder --chown=chrome:chrome /out/stackly-cli /app/stackly-cli
COPY --from=builder --chown=chrome:chrome /app/data /app/data
COPY --from=builder --chown=chrome:chrome /app/web /app/web

# Persistent data directory for cache
RUN mkdir -p /app/data && chown -R chrome:chrome /app/data

# Environment
ENV PORT=8890 \
    CHROME_BIN=/headless-shell/bin/headless-shell \
    CACHE_TTL=24h \
    CACHE_PATH=/app/data/cache.json \
    UA_ROTATE=true \
    WEB_DIR=/app/web \
    DATA_DIR=/app/data \
    GIN_MODE=release

EXPOSE 8890

HEALTHCHECK --interval=30s --timeout=10s --start-period=20s --retries=3 \
    CMD curl -fsS http://localhost:${PORT}/api/health || exit 1

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/app/stackly-server", "-port=8890"]