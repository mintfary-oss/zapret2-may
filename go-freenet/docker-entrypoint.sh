#!/bin/sh
# FreeNet Docker entrypoint.
# Sets up transparent proxy iptables rules (if TRANSPARENT_PROXY=1) and
# launches the freenet daemon.
set -e

FREENET_WEB="${FREENET_WEB:-:8080}"
FREENET_CONFIG="${FREENET_CONFIG:-/app/data/config.yaml}"
TRANSPARENT_PROXY="${TRANSPARENT_PROXY:-0}"
TRANSPARENT_PORT="${TRANSPARENT_PORT:-1090}"

mkdir -p "$(dirname "$FREENET_CONFIG")"

# ---- optional transparent proxy iptables rules ----
if [ "$TRANSPARENT_PROXY" = "1" ]; then
  echo "[entrypoint] setting up transparent proxy rules on port $TRANSPARENT_PORT"

  # Redirect outbound TCP 80 and 443 to the transparent proxy port,
  # excluding the proxy process itself (mark 0x40) to prevent loops.
  iptables -t nat -N FREENET_REDIRECT 2>/dev/null || true
  iptables -t nat -F FREENET_REDIRECT

  # Skip already-marked packets (from freenet itself).
  iptables -t nat -A FREENET_REDIRECT -m mark --mark 0x40 -j RETURN

  # Redirect HTTP and HTTPS traffic.
  iptables -t nat -A FREENET_REDIRECT -p tcp --dport 80  -j REDIRECT --to-ports "$TRANSPARENT_PORT"
  iptables -t nat -A FREENET_REDIRECT -p tcp --dport 443 -j REDIRECT --to-ports "$TRANSPARENT_PORT"

  # Apply to OUTPUT (local traffic from this container).
  iptables -t nat -A OUTPUT -p tcp -j FREENET_REDIRECT

  echo "[entrypoint] transparent proxy rules installed"
fi

echo "[entrypoint] starting freenet"
exec /app/freenet \
  -config "$FREENET_CONFIG" \
  -web "$FREENET_WEB"
