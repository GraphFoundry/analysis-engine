# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/

# Build the application
# CGO_ENABLED=1 is needed for go-sqlite3, which requires gcc. 
# So we need to install build-base in alpine.
RUN apk add --no-cache build-base
RUN CGO_ENABLED=1 GOOS=linux go build -o predictive-analysis-engine ./cmd/server

# Production stage
FROM alpine:3.19

WORKDIR /app

# Create non-root user (matching Node Dockerfile)
RUN addgroup -g 1001 appgroup && \
    adduser -u 1001 -G appgroup -s /bin/sh -D appuser

# Install runtime dependencies (sqlite libs if dynamic, but also wget for healthcheck)
# ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates wget sqlite-libs

# Copy binary from builder
COPY --from=builder /app/predictive-analysis-engine .

# Create data directory for SQLite
RUN mkdir -p /app/data && \
    chown -R appuser:appgroup /app/data

# Set ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port (default 5000)
EXPOSE 5000

# Health check (Parity with Node: wget -qO- http://localhost:${PORT:-5000}/health || exit 1)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:${PORT:-5000}/health || exit 1

# Start server
CMD ["./predictive-analysis-engine"]
