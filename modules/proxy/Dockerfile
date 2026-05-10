FROM golang:1.25.10-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/aegion-proxy ./modules/proxy/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/aegion-proxy /app/aegion-proxy

EXPOSE 9009

ENTRYPOINT ["/app/aegion-proxy"]
