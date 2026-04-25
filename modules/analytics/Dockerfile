# =============================================================================
# Aegion Analytics Dockerfile
# Builds analytics service with DuckDB, Iceberg, and S3 support
# =============================================================================

ARG BASE_IMAGE=golang:1.25.9-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim

# Stage 1: Build analytics binary
FROM ${BASE_IMAGE} AS builder

RUN apt-get update && apt-get install -y \
    pkg-config \
    libssl-dev \
    clang \
    lld \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Copy go modules first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the analytics binary
ARG VERSION=dev
RUN go build -ldflags="-s -w -X main.version=${VERSION}" \
    -o /build/analytics ./modules/analytics/cmd/analytics

# Stage 2: Build migrations
FROM ${BASE_IMAGE} AS migration-builder

WORKDIR /build

# Copy go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build migrations tool if exists
RUN if [ -f ./modules/analytics/cmd/migrate/main.go ]; then \
    go build -ldflags="-s -w" -o /build/analytics-migrate ./modules/analytics/cmd/migrate; \
    fi

# Stage 3: Runtime image
FROM ${RUNTIME_IMAGE}

# Install DuckDB, Iceberg CLI tools, and runtime dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    curl \
    wget \
    postgresql-client \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -r -u 1000 -m -s /usr/sbin/nologin analytics

ENV TZ=UTC

# Create necessary directories
RUN mkdir -p /app \
    /app/config \
    /app/data \
    /app/migrations \
    && chown -R analytics:analytics /app

WORKDIR /app

# Copy binary
COPY --from=builder /build/analytics /app/analytics

# Copy migrations SQL files
COPY --chown=analytics:analytics modules/analytics/migrations /app/migrations

# Copy default configuration
COPY --chown=analytics:analytics modules/analytics/config.yaml /app/config/analytics.yaml || true
COPY --chown=analytics:analytics configs/aegion.yaml /app/config/aegion.yaml || true

# Switch to non-root user
USER analytics

# Expose ports:
# 8080 - REST API
# 8081 - GraphQL API  
# 50051 - gRPC
# 9090 - Prometheus metrics
EXPOSE 8080 8081 50051 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Entry point
ENTRYPOINT ["/app/analytics"]
CMD ["serve", "--config", "/app/config/analytics.yaml"]
