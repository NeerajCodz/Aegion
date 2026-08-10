# Non-listening operator CLI image. It is invoked on demand, never registered.
FROM golang:1.25.10-alpine AS builder

WORKDIR /src
RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/aegion-cli ./modules/cli/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="Aegion Operator CLI" \
      org.opencontainers.image.description="On-demand authenticated Aegion operator client" \
      org.opencontainers.image.version="${VERSION}"

WORKDIR /app
COPY --from=builder /out/aegion-cli /app/aegion-cli

USER 65532:65532
ENTRYPOINT ["/app/aegion-cli"]
