FROM golang:1.25.10-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/aegion-sso ./modules/sso/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/aegion-sso /app/aegion-sso

EXPOSE 9007

ENTRYPOINT ["/app/aegion-sso"]
