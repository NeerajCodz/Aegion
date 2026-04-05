FROM golang:1.25.1-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/aegion-oauth2 ./modules/oauth2/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/aegion-oauth2 /app/aegion-oauth2

ENV AEGION_OAUTH2_CONFIG=/app/oauth2.yaml
EXPOSE 8083

ENTRYPOINT ["/app/aegion-oauth2"]
