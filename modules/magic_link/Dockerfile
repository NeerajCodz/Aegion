# =============================================================================
# Aegion Magic Link Module Dockerfile
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

# Build the magic link module server
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/aegion-magic-link ./modules/magic_link/cmd/server

# Final runtime image
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary
COPY --from=builder /out/aegion-magic-link /app/aegion-magic-link

# Environment defaults
ENV MODULE_PORT=9002
ENV MODULE_LOG_LEVEL=info
ENV MODULE_LOG_FORMAT=json

# Expose module port
EXPOSE 9002

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/aegion-magic-link", "health"]

# Entry point
ENTRYPOINT ["/app/aegion-magic-link"]
CMD ["serve"]
