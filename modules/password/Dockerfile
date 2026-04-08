# =============================================================================
# Aegion Password Module Dockerfile
# =============================================================================

FROM golang:1.25.8-alpine AS builder

WORKDIR /src

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go modules first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the password module server
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/aegion-password ./modules/password/cmd/server

# Final runtime image
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary
COPY --from=builder /out/aegion-password /app/aegion-password

# Environment defaults
ENV MODULE_PORT=9001
ENV MODULE_LOG_LEVEL=info
ENV MODULE_LOG_FORMAT=json

# Expose module port
EXPOSE 9001

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/aegion-password", "health"]

# Entry point
ENTRYPOINT ["/app/aegion-password"]
CMD ["serve"]
