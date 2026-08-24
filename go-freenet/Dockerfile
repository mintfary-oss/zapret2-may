# ---- build stage ----
FROM golang:1.22-alpine AS builder

# ca-certificates needed for hostlist HTTPS download at runtime.
RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /freenet ./cmd/freenet

# ---- runtime stage ----
FROM alpine:3.20

# iptables + ip6tables for transparent proxy redirect rules.
# ca-certificates for HTTPS hostlist downloads.
RUN apk add --no-cache iptables ip6tables ca-certificates && \
    addgroup -S freenet && adduser -S -G freenet freenet

WORKDIR /app

COPY --from=builder /freenet /app/freenet
COPY docker-entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Persist config and domain lists.
VOLUME ["/app/data"]

# 8080 = web UI, 1080 = SOCKS5, 1090 = transparent proxy (optional)
EXPOSE 8080 1080 1090

ENV FREENET_WEB=":8080" \
    FREENET_CONFIG="/app/data/config.yaml"

ENTRYPOINT ["/app/entrypoint.sh"]
