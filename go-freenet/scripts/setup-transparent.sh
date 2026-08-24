#!/usr/bin/env bash
# Sets up iptables transparent proxy rules so ALL TCP traffic on ports
# 80 and 443 is automatically routed through FreeNet.
#
# After running this script you don't need to configure a SOCKS5 proxy
# in your browser — all connections are intercepted automatically.
#
# To undo, run:  sudo bash scripts/teardown-transparent.sh

set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root"; exit 1; }

TPORT="${1:-1090}"   # transparent proxy port (must match config)

echo "[+] installing iptables rules for transparent proxy (port $TPORT)"

# Create a new chain.
iptables -t nat -N FREENET 2>/dev/null || iptables -t nat -F FREENET

# Ignore local/LAN traffic.
iptables -t nat -A FREENET -d 127.0.0.0/8  -j RETURN
iptables -t nat -A FREENET -d 10.0.0.0/8   -j RETURN
iptables -t nat -A FREENET -d 172.16.0.0/12 -j RETURN
iptables -t nat -A FREENET -d 192.168.0.0/16 -j RETURN

# Skip packets already processed by freenet (loopback mark).
iptables -t nat -A FREENET -m mark --mark 0x40 -j RETURN

# Redirect HTTP + HTTPS.
iptables -t nat -A FREENET -p tcp --dport 80  -j REDIRECT --to-ports "$TPORT"
iptables -t nat -A FREENET -p tcp --dport 443 -j REDIRECT --to-ports "$TPORT"

# Hook into OUTPUT (local traffic) and PREROUTING (forwarded traffic).
iptables -t nat -A OUTPUT     -p tcp -j FREENET
iptables -t nat -A PREROUTING -p tcp -j FREENET

echo "[+] done — all TCP 80/443 traffic now routed through freenet"
echo "    transparent proxy must be enabled in /etc/freenet/config.yaml:"
echo "      transparent_addr: \"127.0.0.1:$TPORT\""
echo "    then: sudo systemctl restart freenet"
