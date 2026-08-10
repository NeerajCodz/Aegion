# Admin is an independently deployed module. Build its embedded SPA before Go
# compilation so the image contains the same assets served in production.
FROM node:22-alpine AS spa-builder
WORKDIR /workspace
COPY modules/admin/spa/package.json modules/admin/spa/package-lock.json ./
RUN npm ci
COPY modules/admin/spa/ ./
RUN npm run build

FROM golang:1.25.10-bookworm AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=spa-builder /workspace/dist ./modules/admin/spa/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/aegion-admin ./modules/admin/cmd/admin

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="Aegion Admin Module" \
      org.opencontainers.image.description="Private Aegion admin control-plane module" \
      org.opencontainers.image.version="${VERSION}"
WORKDIR /app
COPY --from=go-builder /out/aegion-admin /app/aegion-admin
USER 65532:65532
EXPOSE 9003
ENTRYPOINT ["/app/aegion-admin"]
