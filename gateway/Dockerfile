FROM golang:1.22-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /src/gateway

COPY gateway/go.mod gateway/go.sum ./
RUN go mod download

COPY gateway/. ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/gateway ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /out/gateway /usr/local/bin/gateway

EXPOSE 8080

CMD ["gateway"]
