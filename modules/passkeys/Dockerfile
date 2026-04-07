FROM golang:1.25.8-alpine AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/aegion-passkeys ./modules/passkeys/cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/aegion-passkeys /app/aegion-passkeys

EXPOSE 9004

ENTRYPOINT ["/app/aegion-passkeys"]
