# ── Build stage ──────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build toolchain (gcc required by mattn/go-sqlite3 CGO)
RUN apk add --no-cache build-base

# Cache module downloads before copying source
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/

# Build a stripped binary (CGO_ENABLED=1 is mandatory for go-sqlite3)
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w" \
    -o predictive-analysis-engine \
    ./cmd/analysis-engine

# ── Production stage ────────────────────────────────────────
FROM alpine:3.21

WORKDIR /app

# Create non-root user
RUN addgroup -g 1001 appgroup && \
    adduser -u 1001 -G appgroup -s /bin/sh -D appuser

# Runtime dependencies: sqlite-libs (dynamic link), ca-certificates (TLS), wget (healthcheck)
RUN apk add --no-cache ca-certificates wget sqlite-libs

# Copy binary from builder
COPY --from=builder /app/predictive-analysis-engine .

# Swagger docs served at runtime via http.ServeFile
COPY docs/swagger.json docs/swagger.yaml ./docs/

# Demo seed data used by /demo/snapshots endpoint
COPY data/demo/ ./data/demo/

# Create writable data directory for SQLite (will be volume-mounted in production)
RUN mkdir -p /app/data && \
    chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Default port (overridable via PORT env var)
EXPOSE 5000

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:${PORT:-5000}/health || exit 1

CMD ["./predictive-analysis-engine"]