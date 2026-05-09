# =============================================================================
# Aegion OAuth2 Module Dockerfile
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

# Build the OAuth2 module server
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/aegion-oauth2 ./modules/oauth2/cmd/server

# Final runtime image
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy binary
COPY --from=builder /out/aegion-oauth2 /app/aegion-oauth2
COPY modules/oauth2/oauth2.yaml /app/oauth2.yaml

# Environment defaults
ENV MODULE_PORT=9005
ENV MODULE_LOG_LEVEL=info
ENV MODULE_LOG_FORMAT=json

# Expose module port
EXPOSE 9005

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/app/aegion-oauth2", "health"]

# Entry point
ENTRYPOINT ["/app/aegion-oauth2"]
CMD ["serve"]
