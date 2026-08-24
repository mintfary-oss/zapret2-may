#!/usr/bin/env bash
# Removes the iptables transparent proxy rules added by setup-transparent.sh.
set -euo pipefail
[ "$(id -u)" -eq 0 ] || { echo "run as root"; exit 1; }

echo "[+] removing freenet iptables rules"
iptables -t nat -D OUTPUT     -p tcp -j FREENET 2>/dev/null || true
iptables -t nat -D PREROUTING -p tcp -j FREENET 2>/dev/null || true
iptables -t nat -F FREENET 2>/dev/null || true
iptables -t nat -X FREENET 2>/dev/null || true
echo "[+] done"
